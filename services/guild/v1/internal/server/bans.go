package server

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

const maxBanReasonRunes = 512

func (s *guildServer) BanGuildMember(ctx context.Context, req *guildv1.BanGuildMemberRequest) (*guildv1.BanGuildMemberResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	if req.GetUserId() == req.GetActorUserId() {
		return nil, invalidRequest("actor cannot ban themselves")
	}
	reason := strings.TrimSpace(req.GetReason())
	if utf8.RuneCountInString(reason) > maxBanReasonRunes {
		return nil, invalidRequest("ban reason is too long")
	}
	authority, err := loadMemberAuthority(ctx, s.svcCtx.Store, req.GetGuildId(), req.GetActorUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	if !authority.has(PermissionBanMembers) {
		return nil, permissionDenied()
	}

	// A ban may target a user who has not joined yet, so verify the user
	// independently from membership before entering the transaction.
	userReq := new(userv1.GetUserRequest)
	userReq.SetUserId(req.GetUserId())
	userResp, err := s.svcCtx.UserClient.GetUser(ctx, userReq)
	if err != nil {
		return nil, err
	}
	if userResp.GetUser().GetUserId() != req.GetUserId() {
		return nil, notFound()
	}

	var ban *model.GuildBan
	createdAt := time.Now().UnixMilli()
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		actor, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !actor.has(PermissionBanMembers) {
			return permissionDenied()
		}
		if actor.Guild.OwnerID == req.GetUserId() {
			return invalidRequest("guild owner cannot be banned")
		}

		target, targetErr := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetUserId())
		switch {
		case targetErr == nil:
			if !canManageMember(actor, target) {
				return permissionDenied()
			}
			if err := txStore.DeleteGuildMemberRoleAssignments(ctx, req.GetGuildId(), req.GetUserId()); err != nil {
				return err
			}
			if err := txStore.DeleteGuildChannelPermissionOverwritesForTarget(
				ctx,
				req.GetGuildId(),
				int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER),
				req.GetUserId(),
			); err != nil {
				return err
			}
			if _, err := txStore.RemoveGuildMember(ctx, req.GetGuildId(), req.GetUserId(), createdAt); err != nil {
				return err
			}
		case !errors.Is(targetErr, sql.ErrNoRows):
			return targetErr
		}

		ban, err = txStore.UpsertGuildBan(ctx, &model.GuildBan{
			GuildID: req.GetGuildId(), UserID: req.GetUserId(),
			ActorUserID: req.GetActorUserId(), Reason: reason, CreatedAt: createdAt,
		})
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newGuildMemberBannedEvent(ban, s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.BanGuildMemberResponse)
	resp.SetBan(guildBanToProto(ban))
	return resp, nil
}

func (s *guildServer) UnbanGuildMember(ctx context.Context, req *guildv1.UnbanGuildMemberRequest) (*guildv1.UnbanGuildMemberResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		actor, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !actor.has(PermissionBanMembers) {
			return permissionDenied()
		}
		return txStore.DeleteGuildBan(ctx, req.GetGuildId(), req.GetUserId())
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newGuildMemberUnbannedEvent(req.GetGuildId(), req.GetUserId(), time.Now().UnixMilli(), s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.UnbanGuildMemberResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *guildServer) ListGuildBans(ctx context.Context, req *guildv1.ListGuildBansRequest) (*guildv1.ListGuildBansResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetBeforeUserId() < 0 {
		return nil, invalidRequest("before user id must not be negative")
	}
	limit, err := normalizeLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}
	authority, err := loadMemberAuthority(ctx, s.svcCtx.Store, req.GetGuildId(), req.GetActorUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	if !authority.has(PermissionBanMembers) {
		return nil, permissionDenied()
	}
	bans, err := s.svcCtx.Store.ListGuildBans(ctx, store.ListGuildBansParams{
		GuildID: req.GetGuildId(), BeforeUserID: req.GetBeforeUserId(), Limit: limit,
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.ListGuildBansResponse)
	resp.SetBans(guildBansToProto(bans))
	if len(bans) > 0 {
		resp.SetBeforeUserId(bans[len(bans)-1].UserID)
	}
	return resp, nil
}

func guildBanToProto(ban *model.GuildBan) *guildv1.GuildBan {
	if ban == nil {
		return nil
	}
	value := new(guildv1.GuildBan)
	value.SetGuildId(ban.GuildID)
	value.SetUserId(ban.UserID)
	value.SetActorUserId(ban.ActorUserID)
	value.SetReason(ban.Reason)
	value.SetCreatedAt(ban.CreatedAt)
	return value
}

func guildBansToProto(bans []*model.GuildBan) []*guildv1.GuildBan {
	values := make([]*guildv1.GuildBan, 0, len(bans))
	for _, ban := range bans {
		values = append(values, guildBanToProto(ban))
	}
	return values
}
