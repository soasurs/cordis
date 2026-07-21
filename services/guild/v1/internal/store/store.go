package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

var (
	ErrMemberAlreadyExists = errors.New("member already exists")
	ErrUserBanned          = errors.New("user is banned")
	// ErrResourceLimitExceeded indicates that a persistent resource quota is full.
	ErrResourceLimitExceeded = errors.New("resource limit exceeded")
)

// QuotaKind identifies one independently serialized resource quota.
type QuotaKind string

const (
	// QuotaOwnedGuilds limits active guild ownership by user.
	QuotaOwnedGuilds QuotaKind = "owned_guilds"
	// QuotaJoinedGuilds limits active guild memberships by user.
	QuotaJoinedGuilds QuotaKind = "joined_guilds"
	// QuotaGuildRoles limits active roles by guild, including the default role.
	QuotaGuildRoles QuotaKind = "guild_roles"
	// QuotaGuildChannels limits active channels by guild.
	QuotaGuildChannels QuotaKind = "guild_channels"
	// QuotaActiveInvites limits usable invites by guild.
	QuotaActiveInvites QuotaKind = "active_invites"
	// QuotaChannelOverwrites limits permission overwrites by channel.
	QuotaChannelOverwrites QuotaKind = "channel_overwrites"
)

// ResourceQuota describes a quota check performed inside a store transaction.
type ResourceQuota struct {
	Kind       QuotaKind
	ScopeID    int64
	Limit      int
	Now        int64
	TargetType int32
	TargetID   int64
}

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

type ListGuildBansParams struct {
	GuildID      int64
	BeforeUserID int64
	Limit        int
}

type ListGuildInvitesParams struct {
	GuildID  int64
	BeforeID int64
	Limit    int
}

type UpdateGuildRoleParams struct {
	GuildID     int64
	RoleID      int64
	Name        *string
	Permissions *uint64
	UpdatedAt   int64
}

type UpdateGuildChannelParams struct {
	ChannelID int64
	Name      *string
	Topic     *string
	ParentID  *int64
	UpdatedAt int64
}

type Store interface {
	Transact(ctx context.Context, fn func(txStore Store) error) error
	CheckResourceQuota(ctx context.Context, quota ResourceQuota) error
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
	UpsertGuildBan(ctx context.Context, ban *model.GuildBan) (*model.GuildBan, error)
	DeleteGuildBan(ctx context.Context, guildID, userID int64) error
	GetGuildBan(ctx context.Context, guildID, userID int64) (*model.GuildBan, error)
	ListGuildBans(ctx context.Context, params ListGuildBansParams) ([]*model.GuildBan, error)
	DeleteGuildBans(ctx context.Context, guildID int64) error
	GetGuild(ctx context.Context, guildID int64) (*model.Guild, error)
	CountGuildMembers(ctx context.Context, guildID int64) (int64, error)
	CreateGuildInvite(ctx context.Context, invite *model.GuildInvite) (*model.GuildInvite, error)
	GetGuildInvite(ctx context.Context, code string) (*model.GuildInvite, error)
	ListGuildInvites(ctx context.Context, params ListGuildInvitesParams) ([]*model.GuildInvite, error)
	ConsumeGuildInvite(ctx context.Context, code string, now int64) (*model.GuildInvite, error)
	DeleteGuildInvite(ctx context.Context, code string) error
	DeleteGuildInvites(ctx context.Context, guildID int64) error
	TransferGuildOwnership(ctx context.Context, guildID, currentOwnerID, newOwnerID int64) (*model.Guild, error)
	CreateGuildRole(ctx context.Context, roleID, guildID int64, name string, permissions uint64, position int32, createdAt int64) (*model.Role, error)
	GetGuildRole(ctx context.Context, guildID, roleID int64) (*model.Role, error)
	ListGuildRoles(ctx context.Context, guildID int64) ([]*model.Role, error)
	ListGuildRolesByGuilds(ctx context.Context, guildIDs []int64) ([]*model.Role, error)
	UpdateGuildRole(ctx context.Context, params UpdateGuildRoleParams) (*model.Role, error)
	UpdateGuildRolePosition(ctx context.Context, guildID, roleID int64, position int32, updatedAt int64) (*model.Role, error)
	UpdateGuildRolePositions(ctx context.Context, guildID int64, roleIDs []int64, positions []int32, updatedAt int64) ([]*model.Role, error)
	DeleteGuildRole(ctx context.Context, guildID, roleID, deletedAt int64) (*model.Role, error)
	AddGuildMemberRole(ctx context.Context, guildID, userID, roleID, createdAt int64) error
	RemoveGuildMemberRole(ctx context.Context, guildID, userID, roleID int64) error
	DeleteGuildMemberRoleAssignments(ctx context.Context, guildID, userID int64) error
	DeleteGuildRoleAssignments(ctx context.Context, guildID, roleID int64) error
	DeleteAllGuildRoleAssignments(ctx context.Context, guildID int64) error
	ListGuildMemberRoles(ctx context.Context, guildID, userID int64) ([]*model.Role, error)
	ListGuildMemberRolesByGuilds(ctx context.Context, guildIDs []int64, userID int64) ([]*model.Role, error)
	CreateGuildChannel(ctx context.Context, channelID, guildID int64, name string, channelType, position int32, topic string, parentID, createdAt int64) (*model.Channel, error)
	GetGuildChannel(ctx context.Context, channelID int64) (*model.Channel, error)
	ListGuildChannels(ctx context.Context, guildID int64) ([]*model.Channel, error)
	ListGuildChannelsByGuilds(ctx context.Context, guildIDs []int64) ([]*model.Channel, error)
	UpdateGuildChannel(ctx context.Context, params UpdateGuildChannelParams) (*model.Channel, error)
	UpdateGuildChannelPosition(ctx context.Context, guildID, channelID int64, position int32, updatedAt int64) (*model.Channel, error)
	UpdateGuildChannelPositions(ctx context.Context, guildID int64, channelIDs []int64, positions []int32, updatedAt int64) ([]*model.Channel, error)
	DeleteGuildChannel(ctx context.Context, channelID, deletedAt int64) (*model.Channel, error)
	DeleteGuildChannels(ctx context.Context, guildID, deletedAt int64) error
	ClearGuildChannelParent(ctx context.Context, guildID, parentID, updatedAt int64) error
	UpsertGuildChannelPermissionOverwrite(ctx context.Context, overwrite *model.ChannelPermissionOverwrite) (*model.ChannelPermissionOverwrite, error)
	DeleteGuildChannelPermissionOverwrite(ctx context.Context, channelID int64, targetType int32, targetID int64) error
	DeleteGuildChannelPermissionOverwrites(ctx context.Context, channelID int64) error
	DeleteAllGuildChannelPermissionOverwrites(ctx context.Context, guildID int64) error
	DeleteGuildChannelPermissionOverwritesForTarget(ctx context.Context, guildID int64, targetType int32, targetID int64) error
	ListGuildChannelPermissionOverwrites(ctx context.Context, channelID int64) ([]*model.ChannelPermissionOverwrite, error)
	ListGuildChannelPermissionOverwritesByChannels(ctx context.Context, channelIDs []int64) ([]*model.ChannelPermissionOverwrite, error)
	ListGuildChannelPermissionOverwritesByGuild(ctx context.Context, guildID int64) ([]*model.ChannelPermissionOverwrite, error)
	ListGuildChannelPermissionOverwritesByGuilds(ctx context.Context, guildIDs []int64, userID int64) ([]*model.ChannelPermissionOverwrite, error)
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
