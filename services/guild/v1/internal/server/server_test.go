package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/guild/v1/config"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
	"github.com/soasurs/cordis/services/guild/v1/internal/svc"
)

func TestCreateGuildCreatesOwnerDefaultRoleAndEvent(t *testing.T) {
	fakeStore := newFakeStore()
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName(" Cordis ")
	req.SetIconUri("icon://guild")
	resp, err := server.CreateGuild(t.Context(), req)
	require.NoError(t, err)

	guild := resp.GetGuild()
	require.Equal(t, int64(1001), guild.GetOwnerId())
	require.Equal(t, "Cordis", guild.GetName())
	require.Equal(t, int64(1), guild.GetRevision())
	require.Contains(t, fakeStore.members[guild.GetId()], int64(1001))
	require.True(t, fakeStore.defaultRoles[guild.GetId()])

	record := publisher.onlyRecord(t)
	require.Equal(t, string(record.key), guildIDString(guild.GetId()))
	var envelope eventEnvelope[guildPayload]
	require.NoError(t, json.Unmarshal(record.payload, &envelope))
	require.Equal(t, EventTypeGuildCreated, envelope.Type)
	require.Equal(t, guildIDString(guild.GetId()), envelope.Data.ID)
	require.Equal(t, "1001", envelope.Data.OwnerID)
}

func TestCreateGuildCommitFailureDoesNotPublish(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.transactErr = errors.New("commit failed")
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")
	_, err := server.CreateGuild(t.Context(), req)
	require.Error(t, err)
	require.Empty(t, publisher.records)
}

func TestPublishEventAddsCommittedAccessRevision(t *testing.T) {
	fakeStore := newFakeStore()
	guild := testGuild(10, 1001)
	guild.AccessRevision = 37
	fakeStore.guilds[10] = guild
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher).(*guildServer)
	event, err := newGuildRoleUpdatedEvent(&model.Role{ID: 20, GuildID: 10, Revision: 2})
	require.NoError(t, err)

	server.publishEvent(t.Context(), event, nil)

	var envelope struct {
		Data struct {
			AccessRevision int64 `json:"access_revision"`
		} `json:"d"`
	}
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, int64(37), envelope.Data.AccessRevision)
}

func TestCreateGuildPublishFailureIsBestEffort(t *testing.T) {
	publisher := &fakePublisher{err: errors.New("kafka unavailable")}
	server := newTestGuildServer(t, newFakeStore(), publisher)
	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")

	resp, err := server.CreateGuild(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.GetGuild())
	require.Len(t, publisher.records, 1)
}

func TestCreateGuildMapsResourceLimit(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.quotaErr = store.ErrResourceLimitExceeded
	server := newTestGuildServer(t, fakeStore, nil)
	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")

	_, err := server.CreateGuild(t.Context(), req)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Len(t, fakeStore.quotas, 1)
	require.Equal(t, store.QuotaOwnedGuilds, fakeStore.quotas[0].Kind)
}

func TestGetGuildHidesNonMember(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001)
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.GetGuildRequest)
	req.SetGuildId(10)
	req.SetUserId(1002)
	_, err := server.GetGuild(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestUpdateGuildRequiresOwnerAndPreservesPresence(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002)
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	deniedReq := new(guildv1.UpdateGuildRequest)
	deniedReq.SetGuildId(10)
	deniedReq.SetActorUserId(1002)
	deniedReq.SetName("Renamed")
	_, err := server.UpdateGuild(t.Context(), deniedReq)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	updateReq := new(guildv1.UpdateGuildRequest)
	updateReq.SetGuildId(10)
	updateReq.SetActorUserId(1001)
	updateReq.SetIconUri("")
	resp, err := server.UpdateGuild(t.Context(), updateReq)
	require.NoError(t, err)
	require.Equal(t, "Guild", resp.GetGuild().GetName())
	require.Empty(t, resp.GetGuild().GetIconUri())
	require.Equal(t, int64(2), resp.GetGuild().GetRevision())

	var envelope eventEnvelope[guildPayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeGuildUpdated, envelope.Type)
	require.Equal(t, int64(2), envelope.Data.Revision)
}

func TestDeleteGuildSoftDeletesChildrenAndPublishesEvent(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002)
	fakeStore.defaultRoles[10] = true
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.DeleteGuildRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	resp, err := server.DeleteGuild(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.NotZero(t, fakeStore.guilds[10].DeletedAt)
	require.Empty(t, fakeStore.members[10])
	require.False(t, fakeStore.defaultRoles[10])

	var envelope eventEnvelope[guildDeletedPayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeGuildDeleted, envelope.Type)
	require.Equal(t, "10", envelope.Data.ID)
	require.Equal(t, int64(2), envelope.Data.Revision)
	require.NotZero(t, envelope.Data.DeletedAt)
}

func TestListUserGuildsUsesDescendingCursor(t *testing.T) {
	fakeStore := newFakeStore()
	for _, id := range []int64{10, 20, 30} {
		fakeStore.guilds[id] = testGuild(id, 1001)
		fakeStore.members[id] = testMembers(id, 1001)
	}
	server := newTestGuildServer(t, fakeStore, nil)
	req := new(guildv1.ListUserGuildsRequest)
	req.SetUserId(1001)
	req.SetBefore(30)
	req.SetLimit(1)
	resp, err := server.ListUserGuilds(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetGuilds(), 1)
	require.Equal(t, int64(20), resp.GetGuilds()[0].GetId())
	require.Equal(t, int64(20), resp.GetBeforeCursor())
}

func newTestGuildServer(t *testing.T, fakeStore store.Store, publisher svc.EventPublisher) guildv1.GuildServiceServer {
	t.Helper()
	node, err := snowflake.New()
	require.NoError(t, err)
	return New(&svc.ServiceContext{
		Cfg:   config.Config{Kafka: config.KafkaConfig{PublishTimeoutMs: 100}},
		Store: fakeStore, Snowflake: node, Publisher: publisher,
		UserClient: &fakeUserClient{},
	})
}

type fakeUserClient struct {
	userv1.UserServiceClient
	requestedUserID int64
	err             error
}

func (f *fakeUserClient) GetUser(_ context.Context, req *userv1.GetUserRequest, _ ...grpc.CallOption) (*userv1.GetUserResponse, error) {
	f.requestedUserID = req.GetUserId()
	if f.err != nil {
		return nil, f.err
	}
	user := new(userv1.User)
	user.SetUserId(req.GetUserId())
	resp := new(userv1.GetUserResponse)
	resp.SetUser(user)
	return resp, nil
}

type publishedRecord struct {
	key     []byte
	payload []byte
}

type fakePublisher struct {
	records []publishedRecord
	err     error
}

func (p *fakePublisher) Publish(_ context.Context, key, payload []byte) error {
	p.records = append(p.records, publishedRecord{
		key: append([]byte(nil), key...), payload: append([]byte(nil), payload...),
	})
	return p.err
}

func (p *fakePublisher) onlyRecord(t *testing.T) publishedRecord {
	t.Helper()
	require.Len(t, p.records, 1)
	return p.records[0]
}

type fakeStore struct {
	guilds       map[int64]*model.Guild
	members      map[int64]map[int64]*model.GuildMember
	roles        map[int64]map[int64]*model.Role
	memberRoles  map[int64]map[int64]map[int64]bool
	channels     map[int64]*model.Channel
	overwrites   map[int64]map[string]*model.ChannelPermissionOverwrite
	defaultRoles map[int64]bool
	bans         map[int64]map[int64]*model.GuildBan
	invites      map[string]*model.GuildInvite
	transactErr  error
	quotaErr     error
	quotas       []store.ResourceQuota

	listOverwritesByChannelCalls int
	listOverwritesByGuildCalls   int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		guilds: make(map[int64]*model.Guild), members: make(map[int64]map[int64]*model.GuildMember),
		roles: make(map[int64]map[int64]*model.Role), memberRoles: make(map[int64]map[int64]map[int64]bool),
		channels: make(map[int64]*model.Channel), overwrites: make(map[int64]map[string]*model.ChannelPermissionOverwrite),
		defaultRoles: make(map[int64]bool),
		bans:         make(map[int64]map[int64]*model.GuildBan),
		invites:      make(map[string]*model.GuildInvite),
	}
}

func (s *fakeStore) Transact(_ context.Context, fn func(txStore store.Store) error) error {
	if err := fn(s); err != nil {
		return err
	}
	return s.transactErr
}

func (s *fakeStore) CheckResourceQuota(_ context.Context, quota store.ResourceQuota) error {
	s.quotas = append(s.quotas, quota)
	return s.quotaErr
}

func (s *fakeStore) CreateGuild(_ context.Context, guildID, ownerID int64, name, iconURI string, createdAt int64) (*model.Guild, error) {
	guild := &model.Guild{ID: guildID, OwnerID: ownerID, Name: name, IconURI: iconURI, Revision: 1, AccessRevision: 1, CreatedAt: createdAt}
	s.guilds[guildID] = guild
	return cloneGuild(guild), nil
}

func (s *fakeStore) CreateGuildMember(_ context.Context, guildID, userID, joinedAt int64) (*model.GuildMember, error) {
	if s.members[guildID] == nil {
		s.members[guildID] = make(map[int64]*model.GuildMember)
	}
	if existing := s.members[guildID][userID]; existing != nil && existing.DeletedAt == 0 {
		return nil, store.ErrMemberAlreadyExists
	}
	revision := int64(1)
	if existing := s.members[guildID][userID]; existing != nil {
		revision = existing.Revision + 1
	}
	member := &model.GuildMember{
		GuildID: guildID, UserID: userID, Revision: revision, JoinedAt: joinedAt,
	}
	s.members[guildID][userID] = member
	return cloneMember(member), nil
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
	if params.IconURI != nil {
		guild.IconURI = *params.IconURI
	}
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

func (s *fakeStore) GetGuildMember(_ context.Context, guildID, userID int64) (*model.GuildMember, error) {
	member := s.members[guildID][userID]
	if member == nil || member.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	return cloneMember(member), nil
}

func (s *fakeStore) ListGuildMembers(_ context.Context, params store.ListGuildMembersParams) ([]*model.GuildMember, error) {
	var members []*model.GuildMember
	for userID, member := range s.members[params.GuildID] {
		if member.DeletedAt != 0 || (params.BeforeUserID != 0 && userID >= params.BeforeUserID) {
			continue
		}
		members = append(members, cloneMember(member))
	}
	sort.Slice(members, func(i, j int) bool { return members[i].UserID > members[j].UserID })
	if len(members) > params.Limit {
		members = members[:params.Limit]
	}
	return members, nil
}

func (s *fakeStore) UpdateGuildMemberNickname(_ context.Context, guildID, userID int64, nickname string) (*model.GuildMember, error) {
	member := s.members[guildID][userID]
	if member == nil || member.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	member.Nickname = nickname
	member.Revision++
	member.UpdatedAt = 2
	return cloneMember(member), nil
}

func (s *fakeStore) RemoveGuildMember(_ context.Context, guildID, userID, removedAt int64) (*model.GuildMember, error) {
	member := s.members[guildID][userID]
	if member == nil || member.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	member.Revision++
	member.UpdatedAt = removedAt
	member.DeletedAt = removedAt
	return cloneMember(member), nil
}

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
		if params.BeforeUserID == 0 || ban.UserID < params.BeforeUserID {
			value := *ban
			bans = append(bans, &value)
		}
	}
	sort.Slice(bans, func(i, j int) bool { return bans[i].UserID > bans[j].UserID })
	if len(bans) > params.Limit {
		bans = bans[:params.Limit]
	}
	return bans, nil
}

func (s *fakeStore) DeleteGuildBans(_ context.Context, guildID int64) error {
	delete(s.bans, guildID)
	return nil
}

func (s *fakeStore) GetGuild(_ context.Context, guildID int64) (*model.Guild, error) {
	guild, ok := s.guilds[guildID]
	if !ok || guild.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	return cloneGuild(guild), nil
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

func (s *fakeStore) CreateGuildInvite(_ context.Context, invite *model.GuildInvite) (*model.GuildInvite, error) {
	if s.invites[invite.Code] != nil {
		return nil, &pq.Error{Code: "23505"}
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

func (s *fakeStore) CreateGuildRole(
	_ context.Context,
	roleID, guildID int64,
	name string,
	permissions uint64,
	position int32,
	createdAt int64,
) (*model.Role, error) {
	if s.roles[guildID] == nil {
		s.roles[guildID] = make(map[int64]*model.Role)
	}
	role := &model.Role{
		ID: roleID, GuildID: guildID, Name: name, Permissions: permissions,
		Position: position, Revision: 1, CreatedAt: createdAt,
	}
	s.roles[guildID][roleID] = role
	return cloneRole(role), nil
}

func (s *fakeStore) GetGuildRole(_ context.Context, guildID, roleID int64) (*model.Role, error) {
	role := s.roles[guildID][roleID]
	if role == nil || role.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	return cloneRole(role), nil
}

func (s *fakeStore) ListGuildRoles(_ context.Context, guildID int64) ([]*model.Role, error) {
	var roles []*model.Role
	for _, role := range s.roles[guildID] {
		if role.DeletedAt == 0 {
			roles = append(roles, cloneRole(role))
		}
	}
	sort.Slice(roles, func(i, j int) bool {
		if roles[i].Position == roles[j].Position {
			return roles[i].ID < roles[j].ID
		}
		return roles[i].Position > roles[j].Position
	})
	return roles, nil
}

func (s *fakeStore) UpdateGuildRole(_ context.Context, params store.UpdateGuildRoleParams) (*model.Role, error) {
	role := s.roles[params.GuildID][params.RoleID]
	if role == nil || role.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	if params.Name != nil {
		role.Name = *params.Name
	}
	if params.Permissions != nil {
		role.Permissions = *params.Permissions
	}
	role.Revision++
	role.UpdatedAt = params.UpdatedAt
	return cloneRole(role), nil
}

func (s *fakeStore) UpdateGuildRolePosition(_ context.Context, guildID, roleID int64, position int32, updatedAt int64) (*model.Role, error) {
	role := s.roles[guildID][roleID]
	if role == nil || role.DeletedAt != 0 || role.IsDefault {
		return nil, sql.ErrNoRows
	}
	role.Position = position
	role.Revision++
	role.UpdatedAt = updatedAt
	return cloneRole(role), nil
}

func (s *fakeStore) DeleteGuildRole(_ context.Context, guildID, roleID, deletedAt int64) (*model.Role, error) {
	role := s.roles[guildID][roleID]
	if role == nil || role.DeletedAt != 0 || role.IsDefault {
		return nil, sql.ErrNoRows
	}
	role.Revision++
	role.UpdatedAt = deletedAt
	role.DeletedAt = deletedAt
	return cloneRole(role), nil
}

func (s *fakeStore) AddGuildMemberRole(_ context.Context, guildID, userID, roleID, _ int64) error {
	if s.memberRoles[guildID] == nil {
		s.memberRoles[guildID] = make(map[int64]map[int64]bool)
	}
	if s.memberRoles[guildID][userID] == nil {
		s.memberRoles[guildID][userID] = make(map[int64]bool)
	}
	s.memberRoles[guildID][userID][roleID] = true
	return nil
}

func (s *fakeStore) RemoveGuildMemberRole(_ context.Context, guildID, userID, roleID int64) error {
	delete(s.memberRoles[guildID][userID], roleID)
	return nil
}

func (s *fakeStore) DeleteGuildMemberRoleAssignments(_ context.Context, guildID, userID int64) error {
	delete(s.memberRoles[guildID], userID)
	return nil
}

func (s *fakeStore) DeleteGuildRoleAssignments(_ context.Context, guildID, roleID int64) error {
	for _, roles := range s.memberRoles[guildID] {
		delete(roles, roleID)
	}
	return nil
}

func (s *fakeStore) DeleteAllGuildRoleAssignments(_ context.Context, guildID int64) error {
	delete(s.memberRoles, guildID)
	return nil
}

func (s *fakeStore) ListGuildMemberRoles(_ context.Context, guildID, userID int64) ([]*model.Role, error) {
	var roles []*model.Role
	for _, role := range s.roles[guildID] {
		if role.DeletedAt != 0 {
			continue
		}
		if role.IsDefault || s.memberRoles[guildID][userID][role.ID] {
			roles = append(roles, cloneRole(role))
		}
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Position > roles[j].Position })
	return roles, nil
}

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

func (s *fakeStore) UpdateGuildChannelPosition(_ context.Context, channelID int64, position int32, updatedAt int64) (*model.Channel, error) {
	channel := s.channels[channelID]
	if channel == nil || channel.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	channel.Position = position
	channel.Revision++
	channel.UpdatedAt = updatedAt
	return cloneChannel(channel), nil
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
	key := overwriteKey(overwrite.TargetType, overwrite.TargetID)
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

func (s *fakeStore) DeleteGuildChannelPermissionOverwrite(_ context.Context, channelID int64, targetType int32, targetID int64) error {
	delete(s.overwrites[channelID], overwriteKey(targetType, targetID))
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

func (s *fakeStore) DeleteGuildChannelPermissionOverwritesForTarget(_ context.Context, guildID int64, targetType int32, targetID int64) error {
	key := overwriteKey(targetType, targetID)
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
		if values[i].TargetType == values[j].TargetType {
			return values[i].TargetID < values[j].TargetID
		}
		return values[i].TargetType < values[j].TargetType
	})
	return values, nil
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
		if values[i].TargetType != values[j].TargetType {
			return values[i].TargetType < values[j].TargetType
		}
		return values[i].TargetID < values[j].TargetID
	})
	return values, nil
}

func testGuild(id, ownerID int64) *model.Guild {
	return &model.Guild{ID: id, OwnerID: ownerID, Name: "Guild", IconURI: "icon://old", Revision: 1, AccessRevision: 1, CreatedAt: 1}
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

func overwriteKey(targetType int32, targetID int64) string {
	return strconv.FormatInt(int64(targetType), 10) + ":" + strconv.FormatInt(targetID, 10)
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
