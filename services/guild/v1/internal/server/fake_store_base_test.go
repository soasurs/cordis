package server

import (
	"context"
	"strconv"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

type fakeStore struct {
	guilds       map[int64]*model.Guild
	members      map[int64]map[int64]*model.GuildMember
	roles        map[int64]map[int64]*model.Role
	memberRoles  map[int64]map[int64]map[int64]bool
	profiles     map[int64]map[int64]*model.GuildMemberProfile
	channels     map[int64]*model.Channel
	overwrites   map[int64]map[string]*model.ChannelPermissionOverwrite
	defaultRoles map[int64]bool
	bans         map[int64]map[int64]*model.GuildBan
	invites      map[string]*model.GuildInvite
	transactErr  error
	quotaErr     error
	quotas       []store.ResourceQuota
	channelLocks []int64

	idempotency map[string]fakeIdempotencyClaim

	listOverwritesByChannelCalls int
	listOverwritesByGuildCalls   int
}

type fakeIdempotencyClaim struct {
	resourceID  int64
	requestHash []byte
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		guilds: make(map[int64]*model.Guild), members: make(map[int64]map[int64]*model.GuildMember),
		roles: make(map[int64]map[int64]*model.Role), memberRoles: make(map[int64]map[int64]map[int64]bool),
		profiles: make(map[int64]map[int64]*model.GuildMemberProfile),
		channels: make(map[int64]*model.Channel), overwrites: make(map[int64]map[string]*model.ChannelPermissionOverwrite),
		defaultRoles: make(map[int64]bool),
		bans:         make(map[int64]map[int64]*model.GuildBan),
		invites:      make(map[string]*model.GuildInvite),
		idempotency:  make(map[string]fakeIdempotencyClaim),
	}
}

func (s *fakeStore) Transact(_ context.Context, fn func(txStore store.Store) error) error {
	if err := fn(s); err != nil {
		return err
	}
	return s.transactErr
}

func (s *fakeStore) ClaimGuildIdempotency(_ context.Context, params store.ClaimGuildIdempotencyParams) (*store.GuildIdempotencyClaim, error) {
	key := strconv.FormatInt(params.ActorUserID, 10) + "/" + params.Operation + "/" + params.IdempotencyKey
	if existing, ok := s.idempotency[key]; ok {
		return &store.GuildIdempotencyClaim{
			ResourceID:  existing.resourceID,
			RequestHash: append([]byte(nil), existing.requestHash...),
		}, nil
	}
	s.idempotency[key] = fakeIdempotencyClaim{
		resourceID:  params.ResourceID,
		requestHash: append([]byte(nil), params.RequestHash...),
	}
	return &store.GuildIdempotencyClaim{
		ResourceID:  params.ResourceID,
		RequestHash: append([]byte(nil), params.RequestHash...),
		Claimed:     true,
	}, nil
}

func (s *fakeStore) CheckResourceQuota(_ context.Context, quota store.ResourceQuota) error {
	s.quotas = append(s.quotas, quota)
	return s.quotaErr
}

func (s *fakeStore) LockGuildChannelMutations(_ context.Context, guildID int64) error {
	s.channelLocks = append(s.channelLocks, guildID)
	return nil
}

func testGuild(id, ownerID int64) *model.Guild {
	return &model.Guild{
		ID: id, OwnerID: ownerID, Name: "Guild", IconAssetID: 77,
		Revision: 1, AccessRevision: 1, ChannelLayoutRevision: 1, CreatedAt: 1,
	}
}

func cloneGuild(guild *model.Guild) *model.Guild {
	clone := *guild
	return &clone
}

func cloneMember(member *model.GuildMember) *model.GuildMember {
	clone := *member
	return &clone
}

func cloneRole(role *model.Role) *model.Role {
	clone := *role
	return &clone
}

func cloneChannel(channel *model.Channel) *model.Channel {
	clone := *channel
	return &clone
}

func cloneOverwrite(overwrite *model.ChannelPermissionOverwrite) *model.ChannelPermissionOverwrite {
	clone := *overwrite
	return &clone
}

func overwriteKey(appliesTo int32, appliesToID int64) string {
	return strconv.FormatInt(int64(appliesTo), 10) + ":" + strconv.FormatInt(appliesToID, 10)
}

func testMembers(guildID int64, userIDs ...int64) map[int64]*model.GuildMember {
	members := make(map[int64]*model.GuildMember, len(userIDs))
	for _, userID := range userIDs {
		members[userID] = &model.GuildMember{
			GuildID: guildID, UserID: userID, Revision: 1, JoinedAt: 1,
		}
	}
	return members
}

func guildIDString(id int64) string {
	return strconv.FormatInt(id, 10)
}
