package server

import (
	"context"
	"database/sql"
	"sort"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func (s *fakeStore) UpsertGuildBan(_ context.Context, ban *model.GuildBan) (*model.GuildBan, error) {
	if s.bans[ban.GuildID] == nil {
		s.bans[ban.GuildID] = make(map[int64]*model.GuildBan)
	}
	value := *ban
	s.bans[ban.GuildID][ban.UserID] = &value
	return &value, nil
}

func (s *fakeStore) DeleteGuildBan(_ context.Context, guildID, userID int64) error {
	if s.bans[guildID][userID] == nil {
		return sql.ErrNoRows
	}
	delete(s.bans[guildID], userID)
	return nil
}

func (s *fakeStore) GetGuildBan(_ context.Context, guildID, userID int64) (*model.GuildBan, error) {
	ban := s.bans[guildID][userID]
	if ban == nil {
		return nil, sql.ErrNoRows
	}
	value := *ban
	return &value, nil
}

func (s *fakeStore) ListGuildBans(_ context.Context, params store.ListGuildBansParams) ([]*model.GuildBan, error) {
	var bans []*model.GuildBan
	for _, ban := range s.bans[params.GuildID] {
		if params.BeforeCreatedAt != 0 {
			if ban.CreatedAt > params.BeforeCreatedAt ||
				(ban.CreatedAt == params.BeforeCreatedAt && ban.UserID >= params.BeforeUserID) {
				continue
			}
		}
		value := *ban
		bans = append(bans, &value)
	}
	sort.Slice(bans, func(i, j int) bool {
		if bans[i].CreatedAt != bans[j].CreatedAt {
			return bans[i].CreatedAt > bans[j].CreatedAt
		}
		return bans[i].UserID > bans[j].UserID
	})
	if len(bans) > params.Limit {
		bans = bans[:params.Limit]
	}
	return bans, nil
}

func (s *fakeStore) DeleteGuildBans(_ context.Context, guildID int64) error {
	delete(s.bans, guildID)
	return nil
}

func (s *fakeStore) CreateGuildInvite(_ context.Context, invite *model.GuildInvite) (*model.GuildInvite, error) {
	if s.invites[invite.Code] != nil {
		return nil, &pgconn.PgError{Code: "23505"}
	}
	value := *invite
	s.invites[invite.Code] = &value
	clone := value
	return &clone, nil
}

func (s *fakeStore) GetGuildInvite(_ context.Context, code string) (*model.GuildInvite, error) {
	invite := s.invites[code]
	if invite == nil {
		return nil, sql.ErrNoRows
	}
	value := *invite
	return &value, nil
}

func (s *fakeStore) GetGuildInviteByID(_ context.Context, inviteID int64) (*model.GuildInvite, error) {
	for _, invite := range s.invites {
		if invite.ID == inviteID {
			value := *invite
			return &value, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (s *fakeStore) ListGuildInvites(_ context.Context, params store.ListGuildInvitesParams) ([]*model.GuildInvite, error) {
	var invites []*model.GuildInvite
	for _, invite := range s.invites {
		if invite.GuildID != params.GuildID {
			continue
		}
		if params.BeforeID == 0 || invite.ID < params.BeforeID {
			value := *invite
			invites = append(invites, &value)
		}
	}
	sort.Slice(invites, func(i, j int) bool { return invites[i].ID > invites[j].ID })
	if len(invites) > params.Limit {
		invites = invites[:params.Limit]
	}
	return invites, nil
}

func (s *fakeStore) ConsumeGuildInvite(_ context.Context, code string, now int64) (*model.GuildInvite, error) {
	invite := s.invites[code]
	if invite == nil {
		return nil, sql.ErrNoRows
	}
	if invite.MaxUses != 0 && invite.Uses >= invite.MaxUses {
		return nil, sql.ErrNoRows
	}
	if invite.ExpiresAt != 0 && invite.ExpiresAt <= now {
		return nil, sql.ErrNoRows
	}
	invite.Uses++
	value := *invite
	return &value, nil
}

func (s *fakeStore) DeleteGuildInvite(_ context.Context, code string) error {
	if s.invites[code] == nil {
		return sql.ErrNoRows
	}
	delete(s.invites, code)
	return nil
}

func (s *fakeStore) DeleteGuildInvites(_ context.Context, guildID int64) error {
	for code, invite := range s.invites {
		if invite.GuildID == guildID {
			delete(s.invites, code)
		}
	}
	return nil
}

func (s *fakeStore) TransferGuildOwnership(_ context.Context, guildID, currentOwnerID, newOwnerID int64) (*model.Guild, error) {
	guild := s.guilds[guildID]
	if guild == nil || guild.DeletedAt != 0 || guild.OwnerID != currentOwnerID {
		return nil, sql.ErrNoRows
	}
	guild.OwnerID = newOwnerID
	guild.Revision++
	guild.UpdatedAt = 2
	return cloneGuild(guild), nil
}
