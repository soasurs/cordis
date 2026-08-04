package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

const (
	defaultTextCategoryName  = "Text Channels"
	defaultTextChannelName   = "general"
	defaultVoiceCategoryName = "Voice Channels"
	defaultVoiceChannelName  = "General"
)

func (s *guildServer) CreateGuild(ctx context.Context, req *guildv1.CreateGuildRequest) (*guildv1.CreateGuildResponse, error) {
	if req.GetOwnerId() <= 0 {
		return nil, invalidRequest("owner id is required")
	}
	name, err := normalizeGuildName(req.GetName())
	if err != nil {
		return nil, err
	}
	if err := validateIdempotencyKey(req.HasIdempotencyKey(), req.GetIdempotencyKey(), s.svcCtx.Cfg.Idempotency.KeyLength()); err != nil {
		return nil, err
	}
	guildID := s.svcCtx.Snowflake.Generate().Int64()
	createdAt := time.Now().UnixMilli()
	var requestHash []byte
	if req.HasIdempotencyKey() {
		requestHash, err = createGuildRequestHash(name)
		if err != nil {
			return nil, err
		}
	}
	var created *model.Guild
	createdNewGuild := !req.HasIdempotencyKey()
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		if req.HasIdempotencyKey() {
			claim, err := txStore.ClaimGuildIdempotency(ctx, store.ClaimGuildIdempotencyParams{
				ActorUserID:    req.GetOwnerId(),
				Operation:      createGuildOperation,
				IdempotencyKey: req.GetIdempotencyKey(),
				RequestHash:    requestHash,
				ResourceID:     guildID,
				CreatedAt:      createdAt,
				ExpiresAt:      createdAt + s.svcCtx.Cfg.Idempotency.CreateGuildTTL().Milliseconds(),
			})
			if err != nil {
				return err
			}
			if !bytes.Equal(claim.RequestHash, requestHash) {
				return idempotencyKeyReused()
			}
			if !claim.Claimed {
				guild, err := txStore.GetGuild(ctx, claim.ResourceID)
				if err != nil {
					return err
				}
				created = guild
				return nil
			}
			createdNewGuild = true
		}
		if err := txStore.CheckResourceQuota(ctx, store.ResourceQuota{
			Kind: store.QuotaOwnedGuilds, ScopeID: req.GetOwnerId(), Limit: s.svcCtx.Cfg.Limits.OwnedGuilds(),
		}); err != nil {
			return err
		}
		if err := txStore.CheckResourceQuota(ctx, store.ResourceQuota{
			Kind: store.QuotaJoinedGuilds, ScopeID: req.GetOwnerId(), Limit: s.svcCtx.Cfg.Limits.JoinedGuilds(), TargetID: guildID,
		}); err != nil {
			return err
		}
		guild, err := txStore.CreateGuild(ctx, guildID, req.GetOwnerId(), name, createdAt)
		if err != nil {
			return err
		}
		created = guild
		member, err := txStore.CreateGuildMember(ctx, guildID, req.GetOwnerId(), createdAt)
		if err != nil {
			return err
		}
		if err := txStore.UpsertGuildMemberProfile(ctx, guildMemberProfilePlaceholder(guildID, member.UserID, member.Nickname)); err != nil {
			return err
		}
		if err := txStore.CreateDefaultRole(ctx, guildID, createdAt); err != nil {
			return err
		}
		if err := s.createDefaultChannels(ctx, txStore, guildID, createdAt); err != nil {
			return err
		}
		event, err := newGuildCreatedEvent(created, s.svcCtx.Snowflake.Generate().Int64())
		if err != nil {
			return err
		}
		return s.enqueueEvents(ctx, txStore, []guildEvent{event})
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	if createdNewGuild {
		profiles, profileErr := s.getEventUserProfiles(ctx, req.GetOwnerId())
		if profileErr != nil {
			logx.WithContext(ctx).Errorw("hydrate guild member profile projection after guild creation",
				logx.Field("guild_id", created.ID), logx.Field("user_id", req.GetOwnerId()), logx.Field("error", profileErr))
		} else if profileErr := s.svcCtx.Store.UpdateGuildMemberProfilesByUser(
			ctx, guildMemberProfileFromProto(created.ID, "", profiles[req.GetOwnerId()]),
		); profileErr != nil {
			logx.WithContext(ctx).Errorw("update guild member profile projection after guild creation",
				logx.Field("guild_id", created.ID), logx.Field("user_id", req.GetOwnerId()), logx.Field("error", profileErr))
		}
	}

	resp := new(guildv1.CreateGuildResponse)
	resp.SetGuild(guildToProto(created))
	return resp, nil
}

func (s *guildServer) createDefaultChannels(ctx context.Context, txStore store.Store, guildID, createdAt int64) error {
	textCategoryID := s.svcCtx.Snowflake.Generate().Int64()
	voiceCategoryID := s.svcCtx.Snowflake.Generate().Int64()
	channels := []struct {
		id       int64
		name     string
		typeID   guildv1.GuildChannelType
		parentID int64
	}{
		{id: textCategoryID, name: defaultTextCategoryName, typeID: guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY},
		{id: s.svcCtx.Snowflake.Generate().Int64(), name: defaultTextChannelName, typeID: guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT, parentID: textCategoryID},
		{id: voiceCategoryID, name: defaultVoiceCategoryName, typeID: guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY},
		{id: s.svcCtx.Snowflake.Generate().Int64(), name: defaultVoiceChannelName, typeID: guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_VOICE, parentID: voiceCategoryID},
	}
	for position, channel := range channels {
		if err := txStore.CheckResourceQuota(ctx, store.ResourceQuota{
			Kind: store.QuotaGuildChannels, ScopeID: guildID, Limit: s.svcCtx.Cfg.Limits.Channels(),
		}); err != nil {
			return err
		}
		if _, err := txStore.CreateGuildChannel(
			ctx, channel.id, guildID, channel.name, int32(channel.typeID), int32(position), "", channel.parentID, createdAt,
		); err != nil {
			return err
		}
		if _, err := upsertDefaultEveryoneOverwrite(
			ctx, txStore, channel.id, guildID, createdAt, s.svcCtx.Cfg.Limits.Overwrites(),
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *guildServer) GetGuild(ctx context.Context, req *guildv1.GetGuildRequest) (*guildv1.GetGuildResponse, error) {
	if req.GetGuildId() <= 0 {
		return nil, invalidRequest("guild id is required")
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	guild, err := s.svcCtx.Store.GetGuildForMember(ctx, req.GetGuildId(), req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.GetGuildResponse)
	resp.SetGuild(guildToProto(guild))
	return resp, nil
}

func (s *guildServer) ListUserGuilds(ctx context.Context, req *guildv1.ListUserGuildsRequest) (*guildv1.ListUserGuildsResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	token, err := readCursor(req.HasCursor(), req.GetCursor())
	if err != nil {
		return nil, err
	}
	beforeID, _, err := decodeUserGuildsCursor(s.svcCtx.Cursors, token, req.GetUserId())
	if err != nil {
		return nil, err
	}
	limit, err := normalizeLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}
	guilds, err := s.svcCtx.Store.ListUserGuilds(ctx, store.ListUserGuildsParams{
		UserID: req.GetUserId(),
		Before: beforeID,
		Limit:  limit + 1,
	})
	if err != nil {
		return nil, err
	}
	page, hasMore := cursor.Trim(guilds, limit)

	resp := new(guildv1.ListUserGuildsResponse)
	resp.SetGuilds(guildsToProto(page))
	if err := setNextUserGuildsCursor(s.svcCtx.Cursors, resp.SetNextCursor, hasMore, page, req.GetUserId()); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *guildServer) UpdateGuild(ctx context.Context, req *guildv1.UpdateGuildRequest) (*guildv1.UpdateGuildResponse, error) {
	if req.GetGuildId() <= 0 {
		return nil, invalidRequest("guild id is required")
	}
	if req.GetActorUserId() <= 0 {
		return nil, invalidRequest("actor user id is required")
	}
	if !req.HasName() && !req.HasDescription() {
		return nil, invalidRequest("at least one field must be updated")
	}

	params := store.UpdateGuildParams{GuildID: req.GetGuildId()}
	if req.HasName() {
		name, err := normalizeGuildName(req.GetName())
		if err != nil {
			return nil, err
		}
		params.Name = &name
	}
	if req.HasDescription() {
		description, err := normalizeGuildDescription(req.GetDescription())
		if err != nil {
			return nil, err
		}
		params.Description = &description
	}
	var updated *model.Guild
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		authority, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageGuild) {
			return permissionDenied()
		}
		updated, err = txStore.UpdateGuild(ctx, params)
		if err != nil {
			return err
		}
		event, err := newGuildUpdatedEvent(updated, s.svcCtx.Snowflake.Generate().Int64())
		if err != nil {
			return err
		}
		return s.enqueueEvents(ctx, txStore, []guildEvent{event})
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(guildv1.UpdateGuildResponse)
	resp.SetGuild(guildToProto(updated))
	return resp, nil
}

func (s *guildServer) DeleteGuild(ctx context.Context, req *guildv1.DeleteGuildRequest) (*guildv1.DeleteGuildResponse, error) {
	if req.GetGuildId() <= 0 {
		return nil, invalidRequest("guild id is required")
	}
	if req.GetActorUserId() <= 0 {
		return nil, invalidRequest("actor user id is required")
	}

	deletedAt := time.Now().UnixMilli()
	var deleted *model.Guild
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		guild, err := txStore.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if guild.OwnerID != req.GetActorUserId() {
			return permissionDenied()
		}
		deleted, err = txStore.DeleteGuild(ctx, req.GetGuildId(), deletedAt)
		if err != nil {
			return err
		}
		if err := txStore.DeleteGuildMembers(ctx, req.GetGuildId(), deletedAt); err != nil {
			return err
		}
		if err := txStore.DeleteGuildMemberProfiles(ctx, req.GetGuildId()); err != nil {
			return err
		}
		if err := txStore.DeleteAllGuildRoleAssignments(ctx, req.GetGuildId()); err != nil {
			return err
		}
		if err := txStore.DeleteAllGuildChannelPermissionOverwrites(ctx, req.GetGuildId()); err != nil {
			return err
		}
		if err := txStore.DeleteGuildChannels(ctx, req.GetGuildId(), deletedAt); err != nil {
			return err
		}
		if err := txStore.DeleteGuildBans(ctx, req.GetGuildId()); err != nil {
			return err
		}
		if err := txStore.DeleteGuildInvites(ctx, req.GetGuildId()); err != nil {
			return err
		}
		if err := txStore.DeleteGuildRoles(ctx, req.GetGuildId(), deletedAt); err != nil {
			return err
		}
		event, err := newGuildDeletedEvent(deleted, s.svcCtx.Snowflake.Generate().Int64())
		if err != nil {
			return err
		}
		return s.enqueueEvents(ctx, txStore, []guildEvent{event})
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(guildv1.DeleteGuildResponse)
	resp.SetOk(true)
	return resp, nil
}

func addEventAccessRevision(payload []byte, accessRevision int64) ([]byte, error) {
	var envelope struct {
		Type           string                     `json:"t"`
		Data           map[string]json.RawMessage `json:"d"`
		IdempotencyKey string                     `json:"idempotency_key"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal guild event: %w", err)
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("guild event data is missing")
	}
	revision, err := json.Marshal(accessRevision)
	if err != nil {
		return nil, fmt.Errorf("marshal guild access revision: %w", err)
	}
	envelope.Data["access_revision"] = revision
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal guild event: %w", err)
	}
	return encoded, nil
}

func guildToProto(guild *model.Guild) *guildv1.Guild {
	if guild == nil {
		return nil
	}
	value := new(guildv1.Guild)
	value.SetId(guild.ID)
	value.SetOwnerId(guild.OwnerID)
	value.SetName(guild.Name)
	value.SetDescription(guild.Description)
	value.SetIconAssetId(guild.IconAssetID)
	value.SetRevision(guild.Revision)
	value.SetCreatedAt(guild.CreatedAt)
	value.SetUpdatedAt(guild.UpdatedAt)
	return value
}

func guildsToProto(guilds []*model.Guild) []*guildv1.Guild {
	values := make([]*guildv1.Guild, 0, len(guilds))
	for _, guild := range guilds {
		values = append(values, guildToProto(guild))
	}
	return values
}
