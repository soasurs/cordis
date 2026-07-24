package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
	"github.com/soasurs/cordis/services/guild/v1/internal/svc"
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
	guildID := s.svcCtx.Snowflake.Generate().Int64()
	createdAt := time.Now().UnixMilli()
	var created *model.Guild
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
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
		if _, err := txStore.CreateGuildMember(ctx, guildID, req.GetOwnerId(), createdAt); err != nil {
			return err
		}
		if err := txStore.CreateDefaultRole(ctx, guildID, createdAt); err != nil {
			return err
		}
		return s.createDefaultChannels(ctx, txStore, guildID, createdAt)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newGuildCreatedEvent(created, s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)

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
	if req.GetBefore() < 0 {
		return nil, invalidRequest("before cursor must not be negative")
	}
	limit, err := normalizeLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}
	guilds, err := s.svcCtx.Store.ListUserGuilds(ctx, store.ListUserGuildsParams{
		UserID: req.GetUserId(),
		Before: req.GetBefore(),
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}

	resp := new(guildv1.ListUserGuildsResponse)
	resp.SetGuilds(guildsToProto(guilds))
	if len(guilds) > 0 {
		resp.SetBeforeCursor(guilds[len(guilds)-1].ID)
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
	if !req.HasName() {
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
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newGuildUpdatedEvent(updated, s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)

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
		return txStore.DeleteGuildRoles(ctx, req.GetGuildId(), deletedAt)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newGuildDeletedEvent(deleted, s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)

	resp := new(guildv1.DeleteGuildResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *guildServer) publishEvent(ctx context.Context, event guildEvent, buildErr error) {
	if buildErr != nil {
		logx.WithContext(ctx).Errorw("build guild event", logx.Field("error", buildErr))
		return
	}
	s.publishEvents(ctx, []guildEvent{event})
}

func (s *guildServer) publishEvents(ctx context.Context, events []guildEvent) {
	if s.svcCtx.Publisher == nil || len(events) == 0 {
		return
	}

	type accessRevisionResult struct {
		value int64
		ok    bool
	}
	accessRevisions := make(map[int64]accessRevisionResult)
	prepared := make([]kafka.Record, 0, len(events))
	for _, event := range events {
		if event.Type != EventTypeGuildDeleted {
			result, loaded := accessRevisions[event.GuildID]
			if !loaded {
				guild, err := s.svcCtx.Store.GetGuild(ctx, event.GuildID)
				if err != nil {
					logx.WithContext(ctx).Errorw("load guild access revision for event",
						logx.Field("guild_id", event.GuildID),
						logx.Field("event_type", event.Type),
						logx.Field("error", err),
					)
				} else {
					result = accessRevisionResult{value: guild.AccessRevision, ok: true}
				}
				accessRevisions[event.GuildID] = result
			}
			if result.ok {
				payload, err := addEventAccessRevision(event.Payload, result.value)
				if err != nil {
					logx.WithContext(ctx).Errorw("add guild access revision to event",
						logx.Field("guild_id", event.GuildID),
						logx.Field("event_type", event.Type),
						logx.Field("error", err),
					)
				} else {
					event.Payload = payload
				}
			}
		}
		prepared = append(prepared, kafka.Record{Key: event.Key, Payload: event.Payload})
	}

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.svcCtx.Cfg.Kafka.PublishTimeout())
	defer cancel()
	if publisher, ok := s.svcCtx.Publisher.(svc.BatchEventPublisher); ok {
		if err := publisher.PublishBatch(publishCtx, prepared); err != nil {
			logx.WithContext(ctx).Errorw(
				"publish guild events",
				logx.Field("count", len(prepared)),
				logx.Field("error", err),
			)
		}
		return
	}
	for _, record := range prepared {
		if err := s.svcCtx.Publisher.Publish(publishCtx, record.Key, record.Payload); err == nil {
			continue
		} else {
			logx.WithContext(ctx).Errorw(
				"publish guild event",
				logx.Field("key", string(record.Key)),
				logx.Field("error", err),
			)
		}
	}
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
