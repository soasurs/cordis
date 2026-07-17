package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/lib/pq"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

const (
	maxInviteMaxUses      = 10000
	inviteCodeLength      = 10
	inviteCodeAlphabet    = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	maxInviteCodeAttempts = 3
)

func (s *guildServer) CreateGuildInvite(ctx context.Context, req *guildv1.CreateGuildInviteRequest) (*guildv1.CreateGuildInviteResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetMaxUses() < 0 || req.GetMaxUses() > maxInviteMaxUses {
		return nil, invalidRequest("max uses is out of range")
	}
	if req.GetExpiresInMs() < 0 {
		return nil, invalidRequest("expires in must not be negative")
	}
	authority, err := loadMemberAuthority(ctx, s.svcCtx.Store, req.GetGuildId(), req.GetActorUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	if !authority.has(PermissionCreateInvite) {
		return nil, permissionDenied()
	}

	createdAt := time.Now().UnixMilli()
	var expiresAt int64
	if req.GetExpiresInMs() > 0 {
		expiresAt = createdAt + req.GetExpiresInMs()
	}

	var created *model.GuildInvite
	for attempt := 1; ; attempt++ {
		code, err := newInviteCode()
		if err != nil {
			return nil, err
		}
		created, err = s.svcCtx.Store.CreateGuildInvite(ctx, &model.GuildInvite{
			ID:            s.svcCtx.Snowflake.Generate().Int64(),
			Code:          code,
			GuildID:       req.GetGuildId(),
			CreatorUserID: req.GetActorUserId(),
			MaxUses:       req.GetMaxUses(),
			ExpiresAt:     expiresAt,
			CreatedAt:     createdAt,
		})
		if err == nil {
			break
		}
		// Retry only on the astronomically unlikely random code collision.
		if !isUniqueViolation(err) || attempt >= maxInviteCodeAttempts {
			return nil, mapStoreError(err)
		}
	}

	resp := new(guildv1.CreateGuildInviteResponse)
	resp.SetInvite(guildInviteToProto(created))
	return resp, nil
}

func (s *guildServer) GetGuildInvite(ctx context.Context, req *guildv1.GetGuildInviteRequest) (*guildv1.GetGuildInviteResponse, error) {
	code, err := normalizeInviteCode(req.GetCode())
	if err != nil {
		return nil, err
	}
	invite, err := s.svcCtx.Store.GetGuildInvite(ctx, code)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, inviteNotFound()
	}
	if err != nil {
		return nil, err
	}
	if !inviteUsable(invite, time.Now().UnixMilli()) {
		return nil, inviteNotFound()
	}
	guild, err := s.svcCtx.Store.GetGuild(ctx, invite.GuildID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, inviteNotFound()
	}
	if err != nil {
		return nil, err
	}
	memberCount, err := s.svcCtx.Store.CountGuildMembers(ctx, invite.GuildID)
	if err != nil {
		return nil, err
	}

	preview := new(guildv1.GuildInvitePreview)
	preview.SetCode(invite.Code)
	preview.SetGuildId(guild.ID)
	preview.SetGuildName(guild.Name)
	preview.SetGuildIconUri(guild.IconURI)
	preview.SetMemberCount(memberCount)
	preview.SetExpiresAt(invite.ExpiresAt)
	resp := new(guildv1.GetGuildInviteResponse)
	resp.SetPreview(preview)
	return resp, nil
}

func (s *guildServer) ListGuildInvites(ctx context.Context, req *guildv1.ListGuildInvitesRequest) (*guildv1.ListGuildInvitesResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetBeforeId() < 0 {
		return nil, invalidRequest("before id must not be negative")
	}
	limit, err := normalizeLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}
	authority, err := loadMemberAuthority(ctx, s.svcCtx.Store, req.GetGuildId(), req.GetActorUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	if !authority.has(PermissionManageGuild) {
		return nil, permissionDenied()
	}
	invites, err := s.svcCtx.Store.ListGuildInvites(ctx, store.ListGuildInvitesParams{
		GuildID: req.GetGuildId(), BeforeID: req.GetBeforeId(), Limit: limit,
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.ListGuildInvitesResponse)
	resp.SetInvites(guildInvitesToProto(invites))
	if len(invites) > 0 {
		resp.SetBeforeId(invites[len(invites)-1].ID)
	}
	return resp, nil
}

func (s *guildServer) DeleteGuildInvite(ctx context.Context, req *guildv1.DeleteGuildInviteRequest) (*guildv1.DeleteGuildInviteResponse, error) {
	code, err := normalizeInviteCode(req.GetCode())
	if err != nil {
		return nil, err
	}
	if req.GetActorUserId() <= 0 {
		return nil, invalidRequest("actor user id is required")
	}
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		invite, err := txStore.GetGuildInvite(ctx, code)
		if errors.Is(err, sql.ErrNoRows) {
			return inviteNotFound()
		}
		if err != nil {
			return err
		}
		authority, err := loadMemberAuthority(ctx, txStore, invite.GuildID, req.GetActorUserId())
		if err != nil {
			return err
		}
		if invite.CreatorUserID != req.GetActorUserId() && !authority.has(PermissionManageGuild) {
			return permissionDenied()
		}
		return txStore.DeleteGuildInvite(ctx, code)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.DeleteGuildInviteResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *guildServer) JoinGuildByInvite(ctx context.Context, req *guildv1.JoinGuildByInviteRequest) (*guildv1.JoinGuildByInviteResponse, error) {
	code, err := normalizeInviteCode(req.GetCode())
	if err != nil {
		return nil, err
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}

	joinedAt := time.Now().UnixMilli()
	var guild *model.Guild
	var member *model.GuildMember
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		// Consuming first keeps the use-count check atomic; a failed join
		// below rolls the increment back together with the membership.
		invite, err := txStore.ConsumeGuildInvite(ctx, code, joinedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return inviteNotFound()
		}
		if err != nil {
			return err
		}
		guild, err = txStore.GetGuild(ctx, invite.GuildID)
		if errors.Is(err, sql.ErrNoRows) {
			return inviteNotFound()
		}
		if err != nil {
			return err
		}
		if _, err := txStore.GetGuildBan(ctx, invite.GuildID, req.GetUserId()); err == nil {
			return store.ErrUserBanned
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		member, err = txStore.CreateGuildMember(ctx, invite.GuildID, req.GetUserId(), joinedAt)
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newGuildMemberJoinedEvent(member)
	s.publishEvent(ctx, event, eventErr)

	resp := new(guildv1.JoinGuildByInviteResponse)
	resp.SetGuild(guildToProto(guild))
	resp.SetMember(guildMemberToProto(member))
	return resp, nil
}

func normalizeInviteCode(code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", invalidRequest("invite code is required")
	}
	return code, nil
}

func inviteUsable(invite *model.GuildInvite, now int64) bool {
	if invite.ExpiresAt != 0 && invite.ExpiresAt <= now {
		return false
	}
	if invite.MaxUses != 0 && invite.Uses >= invite.MaxUses {
		return false
	}
	return true
}

func newInviteCode() (string, error) {
	code := make([]byte, inviteCodeLength)
	alphabetSize := big.NewInt(int64(len(inviteCodeAlphabet)))
	for i := range code {
		index, err := rand.Int(rand.Reader, alphabetSize)
		if err != nil {
			return "", err
		}
		code[i] = inviteCodeAlphabet[index.Int64()]
	}
	return string(code), nil
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func guildInviteToProto(invite *model.GuildInvite) *guildv1.GuildInvite {
	if invite == nil {
		return nil
	}
	value := new(guildv1.GuildInvite)
	value.SetId(invite.ID)
	value.SetCode(invite.Code)
	value.SetGuildId(invite.GuildID)
	value.SetCreatorUserId(invite.CreatorUserID)
	value.SetMaxUses(invite.MaxUses)
	value.SetUses(invite.Uses)
	value.SetExpiresAt(invite.ExpiresAt)
	value.SetCreatedAt(invite.CreatedAt)
	return value
}

func guildInvitesToProto(invites []*model.GuildInvite) []*guildv1.GuildInvite {
	values := make([]*guildv1.GuildInvite, 0, len(invites))
	for _, invite := range invites {
		values = append(values, guildInviteToProto(invite))
	}
	return values
}
