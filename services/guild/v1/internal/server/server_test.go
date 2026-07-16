package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
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

func TestGetGuildHidesNonMember(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = map[int64]struct{}{1001: {}}
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
	fakeStore.members[10] = map[int64]struct{}{1001: {}, 1002: {}}
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
	fakeStore.members[10] = map[int64]struct{}{1001: {}, 1002: {}}
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
		fakeStore.members[id] = map[int64]struct{}{1001: {}}
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
	})
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
	members      map[int64]map[int64]struct{}
	defaultRoles map[int64]bool
	transactErr  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		guilds: make(map[int64]*model.Guild), members: make(map[int64]map[int64]struct{}),
		defaultRoles: make(map[int64]bool),
	}
}

func (s *fakeStore) Transact(_ context.Context, fn func(txStore store.Store) error) error {
	if err := fn(s); err != nil {
		return err
	}
	return s.transactErr
}

func (s *fakeStore) CreateGuild(_ context.Context, guildID, ownerID int64, name, iconURI string, createdAt int64) (*model.Guild, error) {
	guild := &model.Guild{ID: guildID, OwnerID: ownerID, Name: name, IconURI: iconURI, Revision: 1, CreatedAt: createdAt}
	s.guilds[guildID] = guild
	return cloneGuild(guild), nil
}

func (s *fakeStore) CreateGuildMember(_ context.Context, guildID, userID, _ int64) error {
	if s.members[guildID] == nil {
		s.members[guildID] = make(map[int64]struct{})
	}
	s.members[guildID][userID] = struct{}{}
	return nil
}

func (s *fakeStore) CreateDefaultRole(_ context.Context, guildID, _ int64) error {
	s.defaultRoles[guildID] = true
	return nil
}

func (s *fakeStore) GetGuildForMember(_ context.Context, guildID, userID int64) (*model.Guild, error) {
	guild, ok := s.guilds[guildID]
	if !ok || guild.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	if _, ok := s.members[guildID][userID]; !ok {
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
		if _, ok := s.members[id][params.UserID]; !ok {
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
	return nil
}

func testGuild(id, ownerID int64) *model.Guild {
	return &model.Guild{ID: id, OwnerID: ownerID, Name: "Guild", IconURI: "icon://old", Revision: 1, CreatedAt: 1}
}

func cloneGuild(guild *model.Guild) *model.Guild {
	clone := *guild
	return &clone
}

func guildIDString(id int64) string {
	return strconv.FormatInt(id, 10)
}
