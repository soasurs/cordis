package server

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func (s *fakeStore) CreateGuildChannel(
	_ context.Context,
	channelID, guildID int64,
	name string,
	channelType, position int32,
	topic string,
	parentID int64,
	createdAt int64,
) (*model.Channel, error) {
	channel := &model.Channel{
		ID: channelID, GuildID: guildID, Name: name, Type: channelType,
		Position: position, Topic: topic, Revision: 1, CreatedAt: createdAt, ParentID: parentID,
	}
	s.channels[channelID] = channel
	return cloneChannel(channel), nil
}

func (s *fakeStore) GetGuildChannel(_ context.Context, channelID int64) (*model.Channel, error) {
	channel := s.channels[channelID]
	if channel == nil || channel.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	return cloneChannel(channel), nil
}

func (s *fakeStore) ListGuildChannels(_ context.Context, guildID int64) ([]*model.Channel, error) {
	var channels []*model.Channel
	for _, channel := range s.channels {
		if channel.GuildID == guildID && channel.DeletedAt == 0 {
			channels = append(channels, cloneChannel(channel))
		}
	}
	sort.Slice(channels, func(i, j int) bool {
		if channels[i].Position == channels[j].Position {
			return channels[i].ID < channels[j].ID
		}
		return channels[i].Position < channels[j].Position
	})
	return channels, nil
}

func (s *fakeStore) ListGuildChannelsWithRevision(
	ctx context.Context,
	guildID int64,
) ([]*model.Channel, int64, error) {
	channels, err := s.ListGuildChannels(ctx, guildID)
	if err != nil {
		return nil, 0, err
	}
	revision, err := s.GetGuildChannelLayoutRevision(ctx, guildID)
	if err != nil {
		return nil, 0, err
	}
	return channels, revision, nil
}

func (s *fakeStore) ListGuildChannelsWithRevisionsByGuilds(
	ctx context.Context,
	guildIDs []int64,
) ([]*model.Channel, map[int64]int64, error) {
	var channels []*model.Channel
	revisions := make(map[int64]int64, len(guildIDs))
	for _, guildID := range guildIDs {
		values, revision, err := s.ListGuildChannelsWithRevision(ctx, guildID)
		if err != nil {
			return nil, nil, err
		}
		channels = append(channels, values...)
		revisions[guildID] = revision
	}
	return channels, revisions, nil
}

func (s *fakeStore) ListGuildChannelsByGuilds(ctx context.Context, guildIDs []int64) ([]*model.Channel, error) {
	var channels []*model.Channel
	for _, guildID := range guildIDs {
		values, err := s.ListGuildChannels(ctx, guildID)
		if err != nil {
			return nil, err
		}
		channels = append(channels, values...)
	}
	return channels, nil
}

func (s *fakeStore) UpdateGuildChannel(_ context.Context, params store.UpdateGuildChannelParams) (*model.Channel, error) {
	channel := s.channels[params.ChannelID]
	if channel == nil || channel.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	if params.Name != nil {
		channel.Name = *params.Name
	}
	if params.Topic != nil {
		channel.Topic = *params.Topic
	}
	if params.ParentID != nil {
		channel.ParentID = *params.ParentID
	}
	channel.Revision++
	channel.UpdatedAt = params.UpdatedAt
	return cloneChannel(channel), nil
}

func (s *fakeStore) UpdateGuildChannelPosition(_ context.Context, guildID, channelID int64, position int32, updatedAt int64) (*model.Channel, error) {
	channel := s.channels[channelID]
	if channel == nil || channel.GuildID != guildID || channel.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	channel.Position = position
	channel.Revision++
	channel.UpdatedAt = updatedAt
	return cloneChannel(channel), nil
}

func (s *fakeStore) UpdateGuildChannelPositions(_ context.Context, guildID int64, updates []store.GuildChannelPositionUpdate, updatedAt int64) ([]*model.Channel, error) {
	channels := make([]*model.Channel, 0, len(updates))
	for _, update := range updates {
		channel := s.channels[update.ChannelID]
		if channel == nil || channel.GuildID != guildID || channel.DeletedAt != 0 {
			return nil, sql.ErrNoRows
		}
		channel.Position = update.Position
		channel.ParentID = update.ParentID
		channel.Revision++
		channel.UpdatedAt = updatedAt
		channels = append(channels, cloneChannel(channel))
	}
	return channels, nil
}

func (s *fakeStore) DeleteGuildChannel(_ context.Context, channelID, deletedAt int64) (*model.Channel, error) {
	channel := s.channels[channelID]
	if channel == nil || channel.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	channel.Revision++
	channel.UpdatedAt = deletedAt
	channel.DeletedAt = deletedAt
	return cloneChannel(channel), nil
}

func (s *fakeStore) DeleteGuildChannels(_ context.Context, guildID, deletedAt int64) error {
	for _, channel := range s.channels {
		if channel.GuildID == guildID && channel.DeletedAt == 0 {
			channel.DeletedAt = deletedAt
		}
	}
	return nil
}

func (s *fakeStore) ClearGuildChannelParent(_ context.Context, guildID, parentID, updatedAt int64) error {
	for _, channel := range s.channels {
		if channel.GuildID == guildID && channel.ParentID == parentID && channel.DeletedAt == 0 {
			channel.ParentID = 0
			channel.Revision++
			channel.UpdatedAt = updatedAt
		}
	}
	return nil
}

func (s *fakeStore) UpsertGuildChannelPermissionOverwrite(_ context.Context, overwrite *model.ChannelPermissionOverwrite) (*model.ChannelPermissionOverwrite, error) {
	if s.overwrites[overwrite.ChannelID] == nil {
		s.overwrites[overwrite.ChannelID] = make(map[string]*model.ChannelPermissionOverwrite)
	}
	key := overwriteKey(overwrite.AppliesTo, overwrite.AppliesToID)
	if existing := s.overwrites[overwrite.ChannelID][key]; existing != nil {
		overwrite.Revision = existing.Revision + 1
		overwrite.CreatedAt = existing.CreatedAt
		overwrite.UpdatedAt = time.Now().UnixMilli()
	} else {
		overwrite.Revision = 1
	}
	clone := *overwrite
	s.overwrites[overwrite.ChannelID][key] = &clone
	return cloneOverwrite(&clone), nil
}

func (s *fakeStore) DeleteGuildChannelPermissionOverwrite(_ context.Context, channelID int64, appliesTo int32, appliesToID int64) error {
	delete(s.overwrites[channelID], overwriteKey(appliesTo, appliesToID))
	return nil
}

func (s *fakeStore) DeleteGuildChannelPermissionOverwrites(_ context.Context, channelID int64) error {
	delete(s.overwrites, channelID)
	return nil
}

func (s *fakeStore) DeleteAllGuildChannelPermissionOverwrites(_ context.Context, guildID int64) error {
	for channelID, channel := range s.channels {
		if channel.GuildID == guildID {
			delete(s.overwrites, channelID)
		}
	}
	return nil
}

func (s *fakeStore) DeleteGuildChannelPermissionOverwritesForAppliesTo(_ context.Context, guildID int64, appliesTo int32, appliesToID int64) error {
	key := overwriteKey(appliesTo, appliesToID)
	for channelID, channel := range s.channels {
		if channel.GuildID == guildID {
			delete(s.overwrites[channelID], key)
		}
	}
	return nil
}

func (s *fakeStore) ListGuildChannelPermissionOverwrites(_ context.Context, channelID int64) ([]*model.ChannelPermissionOverwrite, error) {
	s.listOverwritesByChannelCalls++
	var values []*model.ChannelPermissionOverwrite
	for _, overwrite := range s.overwrites[channelID] {
		values = append(values, cloneOverwrite(overwrite))
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].AppliesTo == values[j].AppliesTo {
			return values[i].AppliesToID < values[j].AppliesToID
		}
		return values[i].AppliesTo < values[j].AppliesTo
	})
	return values, nil
}

func (s *fakeStore) ListGuildChannelPermissionOverwritesByChannels(ctx context.Context, channelIDs []int64) ([]*model.ChannelPermissionOverwrite, error) {
	var overwrites []*model.ChannelPermissionOverwrite
	for _, channelID := range channelIDs {
		values, err := s.ListGuildChannelPermissionOverwrites(ctx, channelID)
		if err != nil {
			return nil, err
		}
		overwrites = append(overwrites, values...)
	}
	return overwrites, nil
}

func (s *fakeStore) ListGuildChannelPermissionOverwritesByGuild(_ context.Context, guildID int64) ([]*model.ChannelPermissionOverwrite, error) {
	s.listOverwritesByGuildCalls++
	var values []*model.ChannelPermissionOverwrite
	for channelID, channel := range s.channels {
		if channel.GuildID != guildID || channel.DeletedAt != 0 {
			continue
		}
		for _, overwrite := range s.overwrites[channelID] {
			values = append(values, cloneOverwrite(overwrite))
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].ChannelID != values[j].ChannelID {
			return values[i].ChannelID < values[j].ChannelID
		}
		if values[i].AppliesTo != values[j].AppliesTo {
			return values[i].AppliesTo < values[j].AppliesTo
		}
		return values[i].AppliesToID < values[j].AppliesToID
	})
	return values, nil
}

func (s *fakeStore) ListGuildChannelPermissionOverwritesByGuilds(ctx context.Context, guildIDs []int64, _ int64) ([]*model.ChannelPermissionOverwrite, error) {
	var overwrites []*model.ChannelPermissionOverwrite
	for _, guildID := range guildIDs {
		values, err := s.ListGuildChannelPermissionOverwritesByGuild(ctx, guildID)
		if err != nil {
			return nil, err
		}
		overwrites = append(overwrites, values...)
	}
	return overwrites, nil
}
