package server

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func (s *guildServer) CreateGuild(ctx context.Context, req *guildv1.CreateGuildRequest) (*guildv1.CreateGuildResponse, error) {
	if req.GetOwnerId() <= 0 {
		return nil, invalidRequest("owner id is required")
	}
	name, err := normalizeGuildName(req.GetName())
	if err != nil {
		return nil, err
	}
	if err := validateIconURI(req.GetIconUri()); err != nil {
		return nil, err
	}

	guildID := s.svcCtx.Snowflake.Generate().Int64()
	createdAt := time.Now().UnixMilli()
	var created *model.Guild
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		guild, err := txStore.CreateGuild(ctx, guildID, req.GetOwnerId(), name, req.GetIconUri(), createdAt)
		if err != nil {
			return err
		}
		created = guild
		if _, err := txStore.CreateGuildMember(ctx, guildID, req.GetOwnerId(), createdAt); err != nil {
			return err
		}
		return txStore.CreateDefaultRole(ctx, guildID, createdAt)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newGuildCreatedEvent(created)
	s.publishEvent(ctx, event, eventErr)

	resp := new(guildv1.CreateGuildResponse)
	resp.SetGuild(guildToProto(created))
	return resp, nil
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
	if !req.HasName() && !req.HasIconUri() {
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
	if req.HasIconUri() {
		if err := validateIconURI(req.GetIconUri()); err != nil {
			return nil, err
		}
		iconURI := req.GetIconUri()
		params.IconURI = &iconURI
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

	event, eventErr := newGuildUpdatedEvent(updated)
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
		return txStore.DeleteGuildRoles(ctx, req.GetGuildId(), deletedAt)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newGuildDeletedEvent(deleted)
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
	if s.svcCtx.Publisher == nil {
		return
	}
	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.svcCtx.Cfg.Kafka.PublishTimeout())
	defer cancel()
	if err := s.svcCtx.Publisher.Publish(publishCtx, event.Key, event.Payload); err != nil {
		logx.WithContext(ctx).Errorw(
			"publish guild event",
			logx.Field("key", string(event.Key)),
			logx.Field("error", err),
		)
	}
}

func guildToProto(guild *model.Guild) *guildv1.Guild {
	if guild == nil {
		return nil
	}
	value := new(guildv1.Guild)
	value.SetId(guild.ID)
	value.SetOwnerId(guild.OwnerID)
	value.SetName(guild.Name)
	value.SetIconUri(guild.IconURI)
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
