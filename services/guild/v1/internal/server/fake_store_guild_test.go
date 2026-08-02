package server

import (
	"context"
	"database/sql"
	"sort"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func (s *fakeStore) CreateGuild(_ context.Context, guildID, ownerID int64, name string, createdAt int64) (*model.Guild, error) {
	guild := &model.Guild{
		ID: guildID, OwnerID: ownerID, Name: name, Revision: 1, AccessRevision: 1,
		ChannelLayoutRevision: 1, CreatedAt: createdAt,
	}
	s.guilds[guildID] = guild
	return cloneGuild(guild), nil
}

func (s *fakeStore) CreateDefaultRole(_ context.Context, guildID, _ int64) error {
	s.defaultRoles[guildID] = true
	if s.roles[guildID] == nil {
		s.roles[guildID] = make(map[int64]*model.Role)
	}
	s.roles[guildID][guildID] = &model.Role{
		ID: guildID, GuildID: guildID, Name: "@everyone",
		Permissions: PermissionViewChannel | PermissionSendMessages | PermissionCreateInvite,
		IsDefault:   true, Revision: 1, CreatedAt: 1,
	}
	return nil
}

func (s *fakeStore) GetGuildForMember(_ context.Context, guildID, userID int64) (*model.Guild, error) {
	guild, ok := s.guilds[guildID]
	if !ok || guild.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	member := s.members[guildID][userID]
	if member == nil || member.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	return cloneGuild(guild), nil
}

func (s *fakeStore) ListUserGuilds(_ context.Context, params store.ListUserGuildsParams) ([]*model.Guild, error) {
	var guilds []*model.Guild
	for id, guild := range s.guilds {
		if guild.DeletedAt != 0 || (params.Before != 0 && id >= params.Before) {
			continue
		}
		member := s.members[id][params.UserID]
		if member == nil || member.DeletedAt != 0 {
			continue
		}
		guilds = append(guilds, cloneGuild(guild))
	}
	sort.Slice(guilds, func(i, j int) bool { return guilds[i].ID > guilds[j].ID })
	if len(guilds) > params.Limit {
		guilds = guilds[:params.Limit]
	}
	return guilds, nil
}

func (s *fakeStore) UpdateGuild(_ context.Context, params store.UpdateGuildParams) (*model.Guild, error) {
	guild, ok := s.guilds[params.GuildID]
	if !ok || guild.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	if params.Name != nil {
		guild.Name = *params.Name
	}
	if params.Description != nil {
		guild.Description = *params.Description
	}
	guild.Revision++
	guild.UpdatedAt = 2
	return cloneGuild(guild), nil
}

func (s *fakeStore) UpdateGuildIcon(_ context.Context, guildID, assetID int64) (*model.Guild, error) {
	guild, ok := s.guilds[guildID]
	if !ok || guild.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	guild.IconAssetID = assetID
	guild.Revision++
	guild.UpdatedAt = 2
	return cloneGuild(guild), nil
}

func (s *fakeStore) DeleteGuild(_ context.Context, guildID, deletedAt int64) (*model.Guild, error) {
	guild, ok := s.guilds[guildID]
	if !ok || guild.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	guild.Revision++
	guild.UpdatedAt = deletedAt
	guild.DeletedAt = deletedAt
	return cloneGuild(guild), nil
}

func (s *fakeStore) DeleteGuildMembers(_ context.Context, guildID, _ int64) error {
	s.members[guildID] = nil
	return nil
}

func (s *fakeStore) DeleteGuildRoles(_ context.Context, guildID, _ int64) error {
	s.defaultRoles[guildID] = false
	s.roles[guildID] = nil
	return nil
}

func (s *fakeStore) GetGuild(_ context.Context, guildID int64) (*model.Guild, error) {
	guild, ok := s.guilds[guildID]
	if !ok || guild.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	return cloneGuild(guild), nil
}

func (s *fakeStore) GetGuildChannelLayoutRevision(_ context.Context, guildID int64) (int64, error) {
	guild, ok := s.guilds[guildID]
	if !ok || guild.DeletedAt != 0 {
		return 0, sql.ErrNoRows
	}
	return guild.ChannelLayoutRevision, nil
}

func (s *fakeStore) AdvanceGuildChannelLayoutRevision(
	_ context.Context,
	guildID, expectedRevision int64,
) (int64, error) {
	guild, ok := s.guilds[guildID]
	if !ok || guild.DeletedAt != 0 {
		return 0, sql.ErrNoRows
	}
	if guild.ChannelLayoutRevision != expectedRevision {
		return 0, store.ErrGuildChannelLayoutRevisionConflict
	}
	guild.ChannelLayoutRevision++
	return guild.ChannelLayoutRevision, nil
}

func (s *fakeStore) CountGuildMembers(_ context.Context, guildID int64) (int64, error) {
	var count int64
	for _, member := range s.members[guildID] {
		if member.DeletedAt == 0 {
			count++
		}
	}
	return count, nil
}
