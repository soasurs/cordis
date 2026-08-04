package server

import (
	"bytes"
	"context"
	"time"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

// CreateGuildChannel creates a channel with optional idempotent replay after
// checking the actor's MANAGE_CHANNELS permission.
func (s *guildServer) CreateGuildChannel(ctx context.Context, req *guildv1.CreateGuildChannelRequest) (*guildv1.CreateGuildChannelResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	name, err := normalizeChannelName(req.GetName())
	if err != nil {
		return nil, err
	}
	channelType, err := normalizeChannelType(req.GetType())
	if err != nil {
		return nil, err
	}
	if err := validateChannelTopic(req.GetTopic()); err != nil {
		return nil, err
	}
	if err := validateExpectedChannelLayoutRevision(
		req.HasExpectedChannelLayoutRevision(),
		req.GetExpectedChannelLayoutRevision(),
	); err != nil {
		return nil, err
	}
	if err := validateIdempotencyKey(req.HasIdempotencyKey(), req.GetIdempotencyKey(), s.svcCtx.Cfg.Idempotency.KeyLength()); err != nil {
		return nil, err
	}

	var requestHash []byte
	if req.HasIdempotencyKey() {
		requestHash, err = createGuildChannelRequestHash(req.GetGuildId(), name, int32(channelType), req.GetTopic(), req.GetParentId())
		if err != nil {
			return nil, err
		}
	}
	var channel *model.Channel
	var everyoneOverwrite *model.ChannelPermissionOverwrite
	var shifted []*model.Channel
	var layoutRevision int64
	var createdAt int64
	createdNewChannel := !req.HasIdempotencyKey()
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		authority, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageChannels) {
			return permissionDenied()
		}
		createdAt = time.Now().UnixMilli()
		channelID := s.svcCtx.Snowflake.Generate().Int64()
		if req.HasIdempotencyKey() {
			claim, err := txStore.ClaimGuildIdempotency(ctx, store.ClaimGuildIdempotencyParams{
				ActorUserID:    req.GetActorUserId(),
				Operation:      createGuildChannelOperation,
				IdempotencyKey: req.GetIdempotencyKey(),
				RequestHash:    requestHash,
				ResourceID:     channelID,
				CreatedAt:      createdAt,
				ExpiresAt:      createdAt + s.svcCtx.Cfg.Idempotency.CreateGuildChannelTTL().Milliseconds(),
			})
			if err != nil {
				return err
			}
			if !bytes.Equal(claim.RequestHash, requestHash) {
				return idempotencyKeyReused()
			}
			if !claim.Claimed {
				existing, err := txStore.GetGuildChannel(ctx, claim.ResourceID)
				if err != nil {
					return err
				}
				channel = existing
				layoutRevision, err = txStore.GetGuildChannelLayoutRevision(ctx, req.GetGuildId())
				if err != nil {
					return err
				}
				return nil
			}
			createdNewChannel = true
		}
		if err := txStore.LockGuildChannelMutations(ctx, req.GetGuildId()); err != nil {
			return err
		}
		if err := requireGuildChannelLayoutRevision(
			ctx,
			txStore,
			req.GetGuildId(),
			req.GetExpectedChannelLayoutRevision(),
		); err != nil {
			return err
		}
		if err := txStore.CheckResourceQuota(ctx, store.ResourceQuota{
			Kind: store.QuotaGuildChannels, ScopeID: req.GetGuildId(), Limit: s.svcCtx.Cfg.Limits.Channels(),
		}); err != nil {
			return err
		}
		channels, err := txStore.ListGuildChannels(ctx, req.GetGuildId())
		if err != nil {
			return err
		}
		if err := validateChannelParent(ctx, txStore, req.GetGuildId(), channelType, req.GetParentId()); err != nil {
			return err
		}
		position := nextGuildChannelPosition(channels)
		if channelType != guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY && req.GetParentId() == 0 {
			position = uncategorizedChannelInsertPosition(channels)
			updates := make([]store.GuildChannelPositionUpdate, 0, len(channels))
			for _, existing := range channels {
				if existing.Position < position {
					continue
				}
				updates = append(updates, store.GuildChannelPositionUpdate{
					ChannelID: existing.ID, Position: existing.Position + 1, ParentID: existing.ParentID,
				})
			}
			shifted, err = txStore.UpdateGuildChannelPositions(ctx, req.GetGuildId(), updates, createdAt)
			if err != nil {
				return err
			}
			if len(shifted) != len(updates) {
				return notFound()
			}
		}
		channel, err = txStore.CreateGuildChannel(
			ctx, channelID, req.GetGuildId(), name,
			int32(channelType), position, req.GetTopic(), req.GetParentId(), createdAt,
		)
		if err != nil {
			return err
		}
		everyoneOverwrite, err = upsertDefaultEveryoneOverwrite(
			ctx, txStore, channel.ID, channel.GuildID, createdAt, s.svcCtx.Cfg.Limits.Overwrites(),
		)
		if err != nil {
			return err
		}
		layoutRevision, err = txStore.AdvanceGuildChannelLayoutRevision(
			ctx,
			req.GetGuildId(),
			req.GetExpectedChannelLayoutRevision(),
		)
		if err != nil {
			return err
		}
		if createdNewChannel {
			events := make([]guildEvent, 0, len(shifted)+2)
			for _, existing := range shifted {
				event, err := newGuildChannelUpdatedEvent(
					existing,
					layoutRevision,
					s.svcCtx.Snowflake.Generate().Int64(),
				)
				if err != nil {
					return err
				}
				events = append(events, event)
			}
			event, err := newGuildChannelCreatedEvent(
				channel,
				layoutRevision,
				s.svcCtx.Snowflake.Generate().Int64(),
			)
			if err != nil {
				return err
			}
			events = append(events, event)
			if everyoneOverwrite != nil {
				event, err = newGuildChannelOverwriteUpdatedEvent(everyoneOverwrite, s.svcCtx.Snowflake.Generate().Int64())
				if err != nil {
					return err
				}
				events = append(events, event)
			}
			return s.enqueueEvents(ctx, txStore, events)
		}
		return nil
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.CreateGuildChannelResponse)
	resp.SetChannel(guildChannelToProto(channel))
	resp.SetChannelLayoutRevision(layoutRevision)
	return resp, nil
}

// GetGuildChannel returns one channel when the actor can view it.
func (s *guildServer) GetGuildChannel(ctx context.Context, req *guildv1.GetGuildChannelRequest) (*guildv1.GetGuildChannelResponse, error) {
	if err := validateChannelActorRequest(req.GetChannelId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	channel, permissions, err := s.loadAuthorizedChannel(ctx, req.GetChannelId(), req.GetActorUserId())
	if err != nil {
		return nil, err
	}
	if permissions&PermissionViewChannel == 0 {
		return nil, notFound()
	}
	resp := new(guildv1.GetGuildChannelResponse)
	resp.SetChannel(guildChannelToProto(channel))
	return resp, nil
}

// ListGuildChannels returns the channels the actor can view and the current
// layout revision.
func (s *guildServer) ListGuildChannels(ctx context.Context, req *guildv1.ListGuildChannelsRequest) (*guildv1.ListGuildChannelsResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	visible, layoutRevision, err := loadVisibleGuildChannels(
		ctx,
		s.svcCtx.Store,
		req.GetGuildId(),
		req.GetActorUserId(),
	)
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.ListGuildChannelsResponse)
	resp.SetChannels(guildChannelsToProto(visible))
	resp.SetChannelLayoutRevision(layoutRevision)
	return resp, nil
}

func loadVisibleGuildChannels(
	ctx context.Context,
	guildStore store.Store,
	guildID, userID int64,
) ([]*model.Channel, int64, error) {
	authority, roles, err := loadMemberAuthorityAndRoles(ctx, guildStore, guildID, userID)
	if err != nil {
		return nil, 0, err
	}
	channels, layoutRevision, err := guildStore.ListGuildChannelsWithRevision(ctx, guildID)
	if err != nil {
		return nil, 0, err
	}
	if authority.IsOwner || authority.Permissions&PermissionAdministrator != 0 {
		return channels, layoutRevision, nil
	}
	overwrites, err := guildStore.ListGuildChannelPermissionOverwritesByGuild(ctx, guildID)
	if err != nil {
		return nil, 0, err
	}
	return visibleGuildChannels(authority, roles, channels, overwrites, userID), layoutRevision, nil
}

func visibleGuildChannels(
	authority memberAuthority,
	roles []*model.Role,
	channels []*model.Channel,
	overwrites []*model.ChannelPermissionOverwrite,
	userID int64,
) []*model.Channel {
	if authority.IsOwner || authority.Permissions&PermissionAdministrator != 0 {
		return channels
	}
	overwritesByChannel := make(map[int64][]*model.ChannelPermissionOverwrite)
	for _, overwrite := range overwrites {
		overwritesByChannel[overwrite.ChannelID] = append(overwritesByChannel[overwrite.ChannelID], overwrite)
	}
	visible := make([]*model.Channel, 0, len(channels))
	for _, channel := range channels {
		if channelPermissions(authority, roles, overwritesByChannel[channel.ID], userID)&PermissionViewChannel != 0 {
			visible = append(visible, channel)
		}
	}
	return visible
}

// UpdateGuildChannel applies present name, topic, and parent changes under the
// channel mutation lock when the layout changes.
func (s *guildServer) UpdateGuildChannel(ctx context.Context, req *guildv1.UpdateGuildChannelRequest) (*guildv1.UpdateGuildChannelResponse, error) {
	if err := validateChannelActorRequest(req.GetChannelId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	params := store.UpdateGuildChannelParams{ChannelID: req.GetChannelId(), UpdatedAt: time.Now().UnixMilli()}
	if req.HasName() {
		name, err := normalizeChannelName(req.GetName())
		if err != nil {
			return nil, err
		}
		params.Name = &name
	}
	if req.HasTopic() {
		if err := validateChannelTopic(req.GetTopic()); err != nil {
			return nil, err
		}
		topic := req.GetTopic()
		params.Topic = &topic
	}
	if req.HasParentId() {
		parentID := req.GetParentId()
		if parentID < 0 {
			return nil, invalidRequest("parent id must not be negative")
		}
		params.ParentID = &parentID
		if err := validateExpectedChannelLayoutRevision(
			req.HasExpectedChannelLayoutRevision(),
			req.GetExpectedChannelLayoutRevision(),
		); err != nil {
			return nil, err
		}
	}
	if params.Name == nil && params.Topic == nil && params.ParentID == nil {
		return nil, invalidRequest("at least one channel field is required")
	}

	var updated *model.Channel
	var updatedChannels []*model.Channel
	var layoutRevision int64
	var layoutChanged bool
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		channel, err := txStore.GetGuildChannel(ctx, req.GetChannelId())
		if err != nil {
			return err
		}
		if params.ParentID != nil {
			if err := txStore.LockGuildChannelMutations(ctx, channel.GuildID); err != nil {
				return err
			}
			if err := requireGuildChannelLayoutRevision(
				ctx,
				txStore,
				channel.GuildID,
				req.GetExpectedChannelLayoutRevision(),
			); err != nil {
				return err
			}
			// Re-read after locking so parent validation and the update use a
			// consistent channel state with category deletion and reordering.
			channel, err = txStore.GetGuildChannel(ctx, req.GetChannelId())
			if err != nil {
				return err
			}
		}
		authority, err := loadMemberAuthority(ctx, txStore, channel.GuildID, req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageChannels) {
			return permissionDenied()
		}
		if params.ParentID != nil {
			if err := validateChannelParent(ctx, txStore, channel.GuildID, guildv1.GuildChannelType(channel.Type), *params.ParentID); err != nil {
				return err
			}
		}
		if params.ParentID != nil && *params.ParentID != channel.ParentID {
			channels, err := txStore.ListGuildChannels(ctx, channel.GuildID)
			if err != nil {
				return err
			}
			updates, err := channelParentMoveUpdates(channels, channel.ID, *params.ParentID)
			if err != nil {
				return err
			}
			updatedChannels, err = txStore.UpdateGuildChannelPositions(ctx, channel.GuildID, updates, params.UpdatedAt)
			if err != nil {
				return err
			}
			if len(updatedChannels) != len(updates) {
				return notFound()
			}
			for _, candidate := range updatedChannels {
				if candidate.ID == channel.ID {
					updated = candidate
					break
				}
			}
			if updated == nil {
				return notFound()
			}
			params.ParentID = nil
			layoutChanged = true
		}
		if params.Name != nil || params.Topic != nil || params.ParentID != nil {
			updated, err = txStore.UpdateGuildChannel(ctx, params)
			if err != nil {
				return err
			}
			if len(updatedChannels) == 0 {
				updatedChannels = []*model.Channel{updated}
			} else {
				for i, candidate := range updatedChannels {
					if candidate.ID == updated.ID {
						updatedChannels[i] = updated
						break
					}
				}
			}
		}
		if layoutChanged {
			layoutRevision, err = txStore.AdvanceGuildChannelLayoutRevision(
				ctx,
				channel.GuildID,
				req.GetExpectedChannelLayoutRevision(),
			)
			if err != nil {
				return err
			}
		}
		events := make([]guildEvent, 0, len(updatedChannels))
		for _, channel := range updatedChannels {
			event, err := newGuildChannelUpdatedEvent(
				channel,
				layoutRevision,
				s.svcCtx.Snowflake.Generate().Int64(),
			)
			if err != nil {
				return err
			}
			events = append(events, event)
		}
		return s.enqueueEvents(ctx, txStore, events)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.UpdateGuildChannelResponse)
	resp.SetChannel(guildChannelToProto(updated))
	if layoutChanged {
		resp.SetChannelLayoutRevision(layoutRevision)
	}
	return resp, nil
}

// DeleteGuildChannel soft-deletes one channel, detaches its children, and
// advances the layout revision.
func (s *guildServer) DeleteGuildChannel(ctx context.Context, req *guildv1.DeleteGuildChannelRequest) (*guildv1.DeleteGuildChannelResponse, error) {
	if err := validateChannelActorRequest(req.GetChannelId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if err := validateExpectedChannelLayoutRevision(
		req.HasExpectedChannelLayoutRevision(),
		req.GetExpectedChannelLayoutRevision(),
	); err != nil {
		return nil, err
	}
	var deleted *model.Channel
	var movedChildren []*model.Channel
	var layoutRevision int64
	var deletedAt int64
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		channel, err := txStore.GetGuildChannel(ctx, req.GetChannelId())
		if err != nil {
			return err
		}
		authority, err := loadMemberAuthority(ctx, txStore, channel.GuildID, req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageChannels) {
			return permissionDenied()
		}
		if err := txStore.LockGuildChannelMutations(ctx, channel.GuildID); err != nil {
			return err
		}
		channel, err = txStore.GetGuildChannel(ctx, req.GetChannelId())
		if err != nil {
			return err
		}
		if err := requireGuildChannelLayoutRevision(
			ctx,
			txStore,
			channel.GuildID,
			req.GetExpectedChannelLayoutRevision(),
		); err != nil {
			return err
		}
		deletedAt = time.Now().UnixMilli()
		if channel.Type == int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY) {
			// Deleting a category keeps its children and moves them to the
			// guild root instead of cascading content deletion.
			channels, err := txStore.ListGuildChannels(ctx, channel.GuildID)
			if err != nil {
				return err
			}
			childIDs := make(map[int64]struct{})
			for _, child := range channels {
				if child.ParentID == channel.ID {
					childIDs[child.ID] = struct{}{}
				}
			}
			if err := txStore.ClearGuildChannelParent(ctx, channel.GuildID, channel.ID, deletedAt); err != nil {
				return err
			}
			channels, err = txStore.ListGuildChannels(ctx, channel.GuildID)
			if err != nil {
				return err
			}
			for _, child := range channels {
				if _, moved := childIDs[child.ID]; moved {
					movedChildren = append(movedChildren, child)
				}
			}
		}
		if err := txStore.DeleteGuildChannelPermissionOverwrites(ctx, channel.ID); err != nil {
			return err
		}
		deleted, err = txStore.DeleteGuildChannel(ctx, channel.ID, deletedAt)
		if err != nil {
			return err
		}
		layoutRevision, err = txStore.AdvanceGuildChannelLayoutRevision(
			ctx,
			channel.GuildID,
			req.GetExpectedChannelLayoutRevision(),
		)
		if err != nil {
			return err
		}
		events := make([]guildEvent, 0, 1+len(movedChildren))
		event, err := newGuildChannelDeletedEvent(
			deleted,
			layoutRevision,
			s.svcCtx.Snowflake.Generate().Int64(),
		)
		if err != nil {
			return err
		}
		events = append(events, event)
		for _, child := range movedChildren {
			event, err := newGuildChannelUpdatedEvent(
				child,
				layoutRevision,
				s.svcCtx.Snowflake.Generate().Int64(),
			)
			if err != nil {
				return err
			}
			events = append(events, event)
		}
		return s.enqueueEvents(ctx, txStore, events)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.DeleteGuildChannelResponse)
	resp.SetOk(true)
	resp.SetChannelLayoutRevision(layoutRevision)
	return resp, nil
}
