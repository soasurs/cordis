package server

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

// ReorderGuildChannels atomically applies requested positions and parents
// under the channel mutation lock and publishes the changed channels.
func (s *guildServer) ReorderGuildChannels(ctx context.Context, req *guildv1.ReorderGuildChannelsRequest) (*guildv1.ReorderGuildChannelsResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if len(req.GetPositions()) == 0 {
		return nil, invalidRequest("channel positions are required")
	}
	if err := validateExpectedChannelLayoutRevision(
		req.HasExpectedChannelLayoutRevision(),
		req.GetExpectedChannelLayoutRevision(),
	); err != nil {
		return nil, err
	}
	positions := make(map[int64]int32, len(req.GetPositions()))
	for _, item := range req.GetPositions() {
		if item.GetChannelId() <= 0 || item.GetPosition() < 0 || item.HasParentId() && item.GetParentId() < 0 {
			return nil, invalidRequest("channel id and position are invalid")
		}
		if _, exists := positions[item.GetChannelId()]; exists {
			return nil, invalidRequest("channel id must be unique")
		}
		positions[item.GetChannelId()] = item.GetPosition()
	}

	var channels []*model.Channel
	var updated []*model.Channel
	var layoutRevision int64
	var updatedAt int64
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		authority, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageChannels) {
			return permissionDenied()
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
		updatedAt = time.Now().UnixMilli()
		current, err := txStore.ListGuildChannels(ctx, req.GetGuildId())
		if err != nil {
			return err
		}
		currentByID := make(map[int64]*model.Channel, len(current))
		for _, channel := range current {
			currentByID[channel.ID] = channel
		}
		parentIDs := make(map[int64]int64, len(positions))
		for _, item := range req.GetPositions() {
			channel := currentByID[item.GetChannelId()]
			if channel == nil {
				return notFound()
			}
			parentID := channel.ParentID
			if item.HasParentId() {
				parentID = item.GetParentId()
				if err := validateChannelParent(ctx, txStore, req.GetGuildId(), guildv1.GuildChannelType(channel.Type), parentID); err != nil {
					return err
				}
			}
			parentIDs[item.GetChannelId()] = parentID
		}
		updates, err := normalizeGuildChannelPlacements(current, positions, parentIDs)
		if err != nil {
			return err
		}
		if len(updates) == 0 {
			channels = current
			layoutRevision = req.GetExpectedChannelLayoutRevision()
			return nil
		}
		updated, err = txStore.UpdateGuildChannelPositions(ctx, req.GetGuildId(), updates, updatedAt)
		if err != nil {
			return err
		}
		if len(updated) != len(updates) {
			return notFound()
		}
		layoutRevision, err = txStore.AdvanceGuildChannelLayoutRevision(
			ctx,
			req.GetGuildId(),
			req.GetExpectedChannelLayoutRevision(),
		)
		if err != nil {
			return err
		}
		channels, err = txStore.ListGuildChannels(ctx, req.GetGuildId())
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	events := make([]guildEvent, 0, len(updated))
	for _, channel := range updated {
		event, eventErr := newGuildChannelUpdatedEvent(
			channel,
			layoutRevision,
			s.svcCtx.Snowflake.Generate().Int64(),
		)
		if eventErr != nil {
			logx.WithContext(ctx).Errorw("build guild event", logx.Field("error", eventErr))
			continue
		}
		events = append(events, event)
	}
	s.publishEvents(ctx, events)
	resp := new(guildv1.ReorderGuildChannelsResponse)
	resp.SetChannels(guildChannelsToProto(channels))
	resp.SetChannelLayoutRevision(layoutRevision)
	return resp, nil
}

func normalizeGuildChannelPlacements(
	channels []*model.Channel,
	positions map[int64]int32,
	parentIDs map[int64]int64,
) ([]store.GuildChannelPositionUpdate, error) {
	byID := make(map[int64]*model.Channel, len(channels))
	for _, channel := range channels {
		byID[channel.ID] = channel
	}

	ordered := make([]*model.Channel, len(channels))
	requested := make(map[int64]struct{}, len(positions))
	for channelID, position := range positions {
		if position >= int32(len(ordered)) {
			return nil, invalidRequest("channel position is out of range")
		}
		channel := byID[channelID]
		if channel == nil {
			return nil, notFound()
		}
		if ordered[position] != nil {
			return nil, invalidRequest("channel positions conflict")
		}
		ordered[position] = channel
		requested[channelID] = struct{}{}
	}

	next := 0
	for _, channel := range channels {
		if _, ok := requested[channel.ID]; ok {
			continue
		}
		for ordered[next] != nil {
			next++
		}
		ordered[next] = channel
	}

	uncategorized := make([]*model.Channel, 0, len(ordered))
	other := make([]*model.Channel, 0, len(ordered))
	for _, channel := range ordered {
		if isUncategorizedChannel(channel.Type, resolvedChannelParentID(channel, parentIDs)) {
			uncategorized = append(uncategorized, channel)
		} else {
			other = append(other, channel)
		}
	}
	ordered = append(uncategorized, other...)

	updates := make([]store.GuildChannelPositionUpdate, 0, len(ordered))
	for i, channel := range ordered {
		parentID := resolvedChannelParentID(channel, parentIDs)
		position := int32(i)
		if channel.Position == position && channel.ParentID == parentID {
			continue
		}
		updates = append(updates, store.GuildChannelPositionUpdate{
			ChannelID: channel.ID,
			Position:  position,
			ParentID:  parentID,
		})
	}
	return updates, nil
}

func channelParentMoveUpdates(channels []*model.Channel, channelID, parentID int64) ([]store.GuildChannelPositionUpdate, error) {
	remaining := make([]*model.Channel, 0, len(channels)-1)
	var moving *model.Channel
	for _, channel := range channels {
		if channel.ID == channelID {
			moving = channel
			continue
		}
		remaining = append(remaining, channel)
	}
	if moving == nil {
		return nil, notFound()
	}

	destination := len(remaining)
	if parentID == 0 {
		for i, channel := range remaining {
			if !isUncategorizedChannel(channel.Type, channel.ParentID) {
				destination = i
				break
			}
		}
	} else {
		foundParent := false
		for i, channel := range remaining {
			if channel.ID == parentID {
				foundParent = true
				destination = i + 1
			}
			if channel.ParentID == parentID {
				foundParent = true
				destination = i + 1
			}
		}
		if !foundParent {
			return nil, notFound()
		}
	}

	ordered := make([]*model.Channel, 0, len(channels))
	ordered = append(ordered, remaining[:destination]...)
	ordered = append(ordered, moving)
	ordered = append(ordered, remaining[destination:]...)

	updates := make([]store.GuildChannelPositionUpdate, 0, len(ordered))
	for position, channel := range ordered {
		resolvedParentID := channel.ParentID
		if channel.ID == moving.ID {
			resolvedParentID = parentID
		}
		if channel.Position == int32(position) && channel.ParentID == resolvedParentID {
			continue
		}
		updates = append(updates, store.GuildChannelPositionUpdate{
			ChannelID: channel.ID,
			Position:  int32(position),
			ParentID:  resolvedParentID,
		})
	}
	return updates, nil
}

func resolvedChannelParentID(channel *model.Channel, parentIDs map[int64]int64) int64 {
	if parentID, ok := parentIDs[channel.ID]; ok {
		return parentID
	}
	return channel.ParentID
}

func uncategorizedChannelInsertPosition(channels []*model.Channel) int32 {
	position := nextGuildChannelPosition(channels)
	for _, channel := range channels {
		if isUncategorizedChannel(channel.Type, channel.ParentID) {
			continue
		}
		if channel.Position < position {
			position = channel.Position
		}
	}
	return position
}

func nextGuildChannelPosition(channels []*model.Channel) int32 {
	var position int32
	for _, channel := range channels {
		if channel.Position >= position {
			position = channel.Position + 1
		}
	}
	return position
}

func isUncategorizedChannel(channelType int32, parentID int64) bool {
	return parentID == 0 && channelType != int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY)
}
