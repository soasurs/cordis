package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

var ErrMemberAlreadyExists = errors.New("member already exists")

type UpdateGuildParams struct {
	GuildID int64
	Name    *string
	IconURI *string
}

type ListUserGuildsParams struct {
	UserID int64
	Before int64
	Limit  int
}

type ListGuildMembersParams struct {
	GuildID      int64
	BeforeUserID int64
	Limit        int
}

type UpdateGuildRoleParams struct {
	GuildID     int64
	RoleID      int64
	Name        *string
	Permissions *uint64
	UpdatedAt   int64
}

type Store interface {
	Transact(ctx context.Context, fn func(txStore Store) error) error
	CreateGuild(ctx context.Context, guildID, ownerID int64, name, iconURI string, createdAt int64) (*model.Guild, error)
	CreateGuildMember(ctx context.Context, guildID, userID, joinedAt int64) (*model.GuildMember, error)
	CreateDefaultRole(ctx context.Context, guildID, createdAt int64) error
	GetGuildForMember(ctx context.Context, guildID, userID int64) (*model.Guild, error)
	ListUserGuilds(ctx context.Context, params ListUserGuildsParams) ([]*model.Guild, error)
	UpdateGuild(ctx context.Context, params UpdateGuildParams) (*model.Guild, error)
	DeleteGuild(ctx context.Context, guildID, deletedAt int64) (*model.Guild, error)
	DeleteGuildMembers(ctx context.Context, guildID, deletedAt int64) error
	DeleteGuildRoles(ctx context.Context, guildID, deletedAt int64) error
	GetGuildMember(ctx context.Context, guildID, userID int64) (*model.GuildMember, error)
	ListGuildMembers(ctx context.Context, params ListGuildMembersParams) ([]*model.GuildMember, error)
	UpdateGuildMemberNickname(ctx context.Context, guildID, userID int64, nickname string) (*model.GuildMember, error)
	RemoveGuildMember(ctx context.Context, guildID, userID, removedAt int64) (*model.GuildMember, error)
	TransferGuildOwnership(ctx context.Context, guildID, currentOwnerID, newOwnerID int64) (*model.Guild, error)
	CreateGuildRole(ctx context.Context, roleID, guildID int64, name string, permissions uint64, position int32, createdAt int64) (*model.Role, error)
	GetGuildRole(ctx context.Context, guildID, roleID int64) (*model.Role, error)
	ListGuildRoles(ctx context.Context, guildID int64) ([]*model.Role, error)
	UpdateGuildRole(ctx context.Context, params UpdateGuildRoleParams) (*model.Role, error)
	UpdateGuildRolePosition(ctx context.Context, guildID, roleID int64, position int32, updatedAt int64) (*model.Role, error)
	DeleteGuildRole(ctx context.Context, guildID, roleID, deletedAt int64) (*model.Role, error)
	AddGuildMemberRole(ctx context.Context, guildID, userID, roleID, createdAt int64) error
	RemoveGuildMemberRole(ctx context.Context, guildID, userID, roleID int64) error
	DeleteGuildMemberRoleAssignments(ctx context.Context, guildID, userID int64) error
	DeleteGuildRoleAssignments(ctx context.Context, guildID, roleID int64) error
	DeleteAllGuildRoleAssignments(ctx context.Context, guildID int64) error
	ListGuildMemberRoles(ctx context.Context, guildID, userID int64) ([]*model.Role, error)
}

type SQLStore struct {
	db *sqlx.DB
	q  sqlx.ExtContext
}

func New(db *sqlx.DB) Store {
	return &SQLStore{db: db, q: db}
}

func (s *SQLStore) Transact(ctx context.Context, fn func(txStore Store) error) (err error) {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	err = fn(&SQLStore{db: s.db, q: tx})
	if err != nil {
		return err
	}
	return tx.Commit()
}
