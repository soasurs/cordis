package server

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

const maxCommonGuildFilterBatch = 100

func (s *guildServer) FilterUsersWithCommonGuild(
	ctx context.Context,
	req *guildv1.FilterUsersWithCommonGuildRequest,
) (*guildv1.FilterUsersWithCommonGuildResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	targetUserIDs := append([]int64(nil), req.GetTargetUserIds()...)
	slices.Sort(targetUserIDs)
	targetUserIDs = slices.Compact(targetUserIDs)
	if len(targetUserIDs) == 0 {
		return new(guildv1.FilterUsersWithCommonGuildResponse), nil
	}
	if len(targetUserIDs) > maxCommonGuildFilterBatch {
		return nil, invalidRequest("too many target users")
	}
	for _, targetUserID := range targetUserIDs {
		if targetUserID <= 0 {
			return nil, invalidRequest("target user id is required")
		}
	}
	userIDs, err := s.svcCtx.Store.ListUsersWithCommonGuild(ctx, req.GetUserId(), targetUserIDs)
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.FilterUsersWithCommonGuildResponse)
	resp.SetUserIds(userIDs)
	return resp, nil
}

func (s *guildServer) AddGuildMember(ctx context.Context, req *guildv1.AddGuildMemberRequest) (*guildv1.AddGuildMemberResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}

	authority, err := loadMemberAuthority(ctx, s.svcCtx.Store, req.GetGuildId(), req.GetActorUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	if !authority.has(PermissionManageMembers) {
		return nil, permissionDenied()
	}

	userReq := new(userv1.GetUserRequest)
	userReq.SetUserId(req.GetUserId())
	userResp, err := s.svcCtx.UserClient.GetUser(ctx, userReq)
	if err != nil {
		return nil, err
	}
	if userResp.GetUser().GetUserId() != req.GetUserId() {
		return nil, notFound()
	}
	profiles, err := s.getEventUserProfiles(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}

	var member *model.GuildMember
	joinedAt := time.Now().UnixMilli()
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		current, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !current.has(PermissionManageMembers) {
			return permissionDenied()
		}
		if _, err := txStore.GetGuildBan(ctx, req.GetGuildId(), req.GetUserId()); err == nil {
			return store.ErrUserBanned
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := txStore.GetGuildMember(ctx, req.GetGuildId(), req.GetUserId()); err == nil {
			return store.ErrMemberAlreadyExists
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := txStore.CheckResourceQuota(ctx, store.ResourceQuota{
			Kind: store.QuotaJoinedGuilds, ScopeID: req.GetUserId(), Limit: s.svcCtx.Cfg.Limits.JoinedGuilds(), TargetID: req.GetGuildId(),
		}); err != nil {
			return err
		}
		member, err = txStore.CreateGuildMember(ctx, req.GetGuildId(), req.GetUserId(), joinedAt)
		if err != nil {
			return err
		}
		return txStore.UpsertGuildMemberProfile(ctx, guildMemberProfileFromProto(req.GetGuildId(), member.Nickname, profiles[req.GetUserId()]))
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newGuildMemberJoinedEvent(member, profiles[member.UserID], s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.AddGuildMemberResponse)
	resp.SetMember(guildMemberToProto(member))
	return resp, nil
}

func (s *guildServer) GetGuildMember(ctx context.Context, req *guildv1.GetGuildMemberRequest) (*guildv1.GetGuildMemberResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	if _, err := s.svcCtx.Store.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, mapStoreError(err)
	}
	member, err := s.svcCtx.Store.GetGuildMember(ctx, req.GetGuildId(), req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.GetGuildMemberResponse)
	resp.SetMember(guildMemberToProto(member))
	return resp, nil
}

func (s *guildServer) ListGuildMembers(ctx context.Context, req *guildv1.ListGuildMembersRequest) (*guildv1.ListGuildMembersResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	token, err := readCursor(req.HasCursor(), req.GetCursor())
	if err != nil {
		return nil, err
	}
	beforeJoinedAt, beforeUserID, _, err := decodeGuildTimeIDCursor(s.svcCtx.Cursors, cursor.KindGuildMembers, token, req.GetGuildId())
	if err != nil {
		return nil, err
	}
	limit, err := normalizeLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}
	if _, err := s.svcCtx.Store.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, mapStoreError(err)
	}
	members, err := s.svcCtx.Store.ListGuildMembers(ctx, store.ListGuildMembersParams{
		GuildID:        req.GetGuildId(),
		BeforeJoinedAt: beforeJoinedAt,
		BeforeUserID:   beforeUserID,
		Limit:          limit + 1,
	})
	if err != nil {
		return nil, err
	}
	page, hasMore := cursor.Trim(members, limit)
	resp := new(guildv1.ListGuildMembersResponse)
	resp.SetMembers(guildMembersToProto(page))
	if err := setNextMemberCursor(s.svcCtx.Cursors, cursor.KindGuildMembers, resp.SetNextCursor, hasMore, page, req.GetGuildId()); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *guildServer) UpdateGuildMember(ctx context.Context, req *guildv1.UpdateGuildMemberRequest) (*guildv1.UpdateGuildMemberResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	nickname, err := normalizeNickname(req.GetNickname())
	if err != nil {
		return nil, err
	}
	profiles, err := s.getEventUserProfiles(ctx, req.GetActorUserId())
	if err != nil {
		return nil, err
	}
	var member *model.GuildMember
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		if _, err := txStore.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
			return err
		}
		member, err = txStore.UpdateGuildMemberNickname(ctx, req.GetGuildId(), req.GetActorUserId(), nickname)
		if err != nil {
			return err
		}
		return txStore.UpdateGuildMemberProfileNickname(ctx, req.GetGuildId(), req.GetActorUserId(), member.Nickname)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildMemberUpdatedEvent(member, profiles[member.UserID], s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.UpdateGuildMemberResponse)
	resp.SetMember(guildMemberToProto(member))
	return resp, nil
}

func (s *guildServer) KickGuildMember(ctx context.Context, req *guildv1.KickGuildMemberRequest) (*guildv1.KickGuildMemberResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	var removed *model.GuildMember
	removedAt := time.Now().UnixMilli()
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		actor, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !actor.has(PermissionKickMembers) {
			return permissionDenied()
		}
		target, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetUserId())
		if err != nil {
			return err
		}
		if target.IsOwner {
			return invalidRequest("guild owner cannot be kicked")
		}
		if !canManageMember(actor, target) {
			return permissionDenied()
		}
		if err := txStore.DeleteGuildMemberRoleAssignments(ctx, req.GetGuildId(), req.GetUserId()); err != nil {
			return err
		}
		if err := txStore.DeleteGuildChannelPermissionOverwritesForAppliesTo(
			ctx,
			req.GetGuildId(),
			int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER),
			req.GetUserId(),
		); err != nil {
			return err
		}
		removed, err = txStore.RemoveGuildMember(ctx, req.GetGuildId(), req.GetUserId(), removedAt)
		if err != nil {
			return err
		}
		return txStore.DeleteGuildMemberProfile(ctx, req.GetGuildId(), req.GetUserId())
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildMemberRemovedEvent(removed, s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.KickGuildMemberResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *guildServer) LeaveGuild(ctx context.Context, req *guildv1.LeaveGuildRequest) (*guildv1.LeaveGuildResponse, error) {
	if req.GetGuildId() <= 0 {
		return nil, invalidRequest("guild id is required")
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	var removed *model.GuildMember
	removedAt := time.Now().UnixMilli()
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		guild, err := txStore.GetGuildForMember(ctx, req.GetGuildId(), req.GetUserId())
		if err != nil {
			return err
		}
		if guild.OwnerID == req.GetUserId() {
			return invalidRequest("guild owner must transfer ownership before leaving")
		}
		if err := txStore.DeleteGuildMemberRoleAssignments(ctx, req.GetGuildId(), req.GetUserId()); err != nil {
			return err
		}
		if err := txStore.DeleteGuildChannelPermissionOverwritesForAppliesTo(
			ctx,
			req.GetGuildId(),
			int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER),
			req.GetUserId(),
		); err != nil {
			return err
		}
		removed, err = txStore.RemoveGuildMember(ctx, req.GetGuildId(), req.GetUserId(), removedAt)
		if err != nil {
			return err
		}
		return txStore.DeleteGuildMemberProfile(ctx, req.GetGuildId(), req.GetUserId())
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildMemberRemovedEvent(removed, s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.LeaveGuildResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *guildServer) TransferGuildOwnership(ctx context.Context, req *guildv1.TransferGuildOwnershipRequest) (*guildv1.TransferGuildOwnershipResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetNewOwnerId() <= 0 {
		return nil, invalidRequest("new owner id is required")
	}
	if req.GetNewOwnerId() == req.GetActorUserId() {
		return nil, invalidRequest("new owner must differ from current owner")
	}
	var updated *model.Guild
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		guild, err := txStore.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if guild.OwnerID != req.GetActorUserId() {
			return permissionDenied()
		}
		if _, err := txStore.GetGuildMember(ctx, req.GetGuildId(), req.GetNewOwnerId()); err != nil {
			return err
		}
		if err := txStore.CheckResourceQuota(ctx, store.ResourceQuota{
			Kind: store.QuotaOwnedGuilds, ScopeID: req.GetNewOwnerId(), Limit: s.svcCtx.Cfg.Limits.OwnedGuilds(),
		}); err != nil {
			return err
		}
		updated, err = txStore.TransferGuildOwnership(ctx, req.GetGuildId(), req.GetActorUserId(), req.GetNewOwnerId())
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildUpdatedEvent(updated, s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.TransferGuildOwnershipResponse)
	resp.SetGuild(guildToProto(updated))
	return resp, nil
}

func validateMemberActorRequest(guildID, actorUserID int64) error {
	if guildID <= 0 {
		return invalidRequest("guild id is required")
	}
	if actorUserID <= 0 {
		return invalidRequest("actor user id is required")
	}
	return nil
}

func guildMemberToProto(member *model.GuildMember) *guildv1.GuildMember {
	if member == nil {
		return nil
	}
	value := new(guildv1.GuildMember)
	value.SetGuildId(member.GuildID)
	value.SetUserId(member.UserID)
	value.SetNickname(member.Nickname)
	value.SetRevision(member.Revision)
	value.SetJoinedAt(member.JoinedAt)
	value.SetUpdatedAt(member.UpdatedAt)
	return value
}

func guildMembersToProto(members []*model.GuildMember) []*guildv1.GuildMember {
	values := make([]*guildv1.GuildMember, 0, len(members))
	for _, member := range members {
		values = append(values, guildMemberToProto(member))
	}
	return values
}
