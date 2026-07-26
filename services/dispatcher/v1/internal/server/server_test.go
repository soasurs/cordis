package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/dispatcher/v1/config"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

func TestNewRejectsDuplicateKafkaIsolationKeys(t *testing.T) {
	t.Run("topic", func(t *testing.T) {
		cfg := config.Config{Kafka: config.KafkaConfig{
			Seeds:        []string{"127.0.0.1:9092"},
			GuildTopic:   "duplicate",
			MessageTopic: "duplicate",
		}}
		require.PanicsWithValue(t, "dispatcher kafka topics must be unique", func() {
			New(cfg, nil, nil, nil, nil)
		})
	})

	t.Run("consumer group", func(t *testing.T) {
		cfg := config.Config{Kafka: config.KafkaConfig{
			Seeds:                []string{"127.0.0.1:9092"},
			GuildConsumerGroup:   "duplicate",
			MessageConsumerGroup: "duplicate",
		}}
		require.PanicsWithValue(t, "dispatcher kafka consumer groups must be unique", func() {
			New(cfg, nil, nil, nil, nil)
		})
	})
}

func TestDispatchRecordRoutesGuildMessageByGuild(t *testing.T) {
	resolver := &fakeResolver{}
	server := &Server{resolver: resolver}
	value := []byte(`{"t":"` + realtime.EventMessageCreated + `","d":{"id":"1","guild_id":"8001","channel_id":"7001"},"idempotency_key":"1"}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.NoError(t, err)
	require.False(t, permanent)
	require.Equal(t, discovery.RouteGuild, resolver.kind)
	require.Equal(t, int64(8001), resolver.id)
}

func TestDispatchRecordRoutesDmMessageByRecipient(t *testing.T) {
	resolver := &fakeResolver{}
	server := &Server{resolver: resolver}
	value := []byte(`{"t":"` + realtime.EventMessageCreated + `","d":{"id":"1","channel_id":"7001","user_id":"1001"},"idempotency_key":"2"}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.NoError(t, err)
	require.False(t, permanent)
	require.Equal(t, discovery.RouteUser, resolver.kind)
	require.Equal(t, int64(1001), resolver.id)
}

func TestDispatchRecordRoutesReadUpdateByUser(t *testing.T) {
	resolver := &fakeResolver{}
	server := &Server{resolver: resolver}
	value := []byte(`{"t":"` + realtime.EventMessageReadUpdated + `","d":{"user_id":"1001","channel_id":"7001","last_read_message_id":"8001"},"idempotency_key":"3"}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.NoError(t, err)
	require.False(t, permanent)
	require.Equal(t, discovery.RouteUser, resolver.kind)
	require.Equal(t, int64(1001), resolver.id)
}

func TestDispatchRecordRejectsMessageWithoutAggregateRoute(t *testing.T) {
	server := &Server{resolver: &fakeResolver{}}
	value := []byte(`{"t":"` + realtime.EventMessageCreated + `","d":{"id":"1","channel_id":"7001"},"idempotency_key":"4"}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.Error(t, err)
	require.True(t, permanent)
}

func TestDispatchRecordAcceptsStringGuildIDs(t *testing.T) {
	resolver := &fakeResolver{}
	server := &Server{resolver: resolver}
	value := []byte(`{"t":"` + realtime.EventGuildUpdated + `","d":{"id":"8001"},"idempotency_key":"5"}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.NoError(t, err)
	require.False(t, permanent)
	require.Equal(t, discovery.RouteGuild, resolver.kind)
	require.Equal(t, int64(8001), resolver.id)
}

func TestDispatchRecordRejectsUnderscoreEventName(t *testing.T) {
	server := &Server{resolver: &fakeResolver{}}
	value := []byte(`{"t":"message_created","d":{"id":"1","channel_id":"7001"},"idempotency_key":"6"}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.Error(t, err)
	require.True(t, permanent)
}

func TestDispatchRecordRejectsInvalidIdempotencyKey(t *testing.T) {
	server := &Server{resolver: &fakeResolver{}}
	values := []string{"", "abc", "0", "-1", "+1", "01", "999999999999999999999999"}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			recordValue := []byte(`{"t":"` + realtime.EventMessageCreated + `","d":{"id":"1","guild_id":"8001","channel_id":"7001"},"idempotency_key":"` + value + `"}`)
			permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: recordValue})

			require.EqualError(t, err, "invalid idempotency key")
			require.True(t, permanent)
		})
	}
}

func TestDispatchRecordRoutesProfileAudience(t *testing.T) {
	resolver := newCollectingResolver()
	server := &Server{
		resolver: resolver,
		userClient: staticRelationshipClient{relationships: []relationshipValue{
			{targetID: 2001, relationshipType: userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND},
			{targetID: 2002, relationshipType: userv1.RelationshipType_RELATIONSHIP_TYPE_INCOMING},
			{targetID: 2003, relationshipType: userv1.RelationshipType_RELATIONSHIP_TYPE_BLOCKED},
		}},
		guildClient:   staticGuildClient{guildIDs: []int64{8001, 8001, 8002}},
		messageClient: staticMessageClient{recipientIDs: []int64{2001, 3001}},
	}
	value := []byte(`{"t":"` + realtime.EventUserProfileUpdated + `","d":{"user_id":"1001","username":"user"},"idempotency_key":"7"}`)

	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})

	require.NoError(t, err)
	require.False(t, permanent)
	require.ElementsMatch(t, []string{
		"guilds:8001",
		"guilds:8002",
		"users:1001",
		"users:2001",
		"users:2002",
		"users:3001",
	}, resolver.routes())
}

func TestEventConstantsUseDotSeparator(t *testing.T) {
	require.Equal(t, "message.created", realtime.EventMessageCreated)
	require.Equal(t, "message.read.updated", realtime.EventMessageReadUpdated)
	require.Equal(t, "user.profile.updated", realtime.EventUserProfileUpdated)
}

func TestDispatchPresenceSchedulesUserAlongsideGuilds(t *testing.T) {
	resolver := &blockingGuildResolver{userStarted: make(chan struct{})}
	server := &Server{resolver: resolver, userClient: emptyRelationshipClient{}}
	routing := eventRouting{GuildIDs: make([]eventID, 0, presenceDispatchConcurrency+1)}
	for guildID := 1; guildID <= presenceDispatchConcurrency+1; guildID++ {
		routing.GuildIDs = append(routing.GuildIDs, eventID(guildID))
	}
	done := make(chan error, 1)
	go func() {
		done <- server.dispatchPresence(t.Context(), 1001, eventEnvelope{Type: realtime.EventPresenceUpdated}, routing, 0)
	}()

	select {
	case <-resolver.userStarted:
	case <-time.After(time.Second):
		t.Fatal("user dispatch was delayed behind the Guild concurrency limit")
	}
	require.Error(t, <-done)
}

type fakeResolver struct {
	kind discovery.RouteKind
	id   int64
}

type collectingResolver struct {
	mu     sync.Mutex
	values []string
}

func newCollectingResolver() *collectingResolver {
	return new(collectingResolver)
}

func (r *collectingResolver) Resolve(_ context.Context, kind discovery.RouteKind, id int64) ([]discovery.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = append(r.values, fmt.Sprintf("%s:%d", kind, id))
	return nil, nil
}

func (r *collectingResolver) routes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.values...)
}

func (f *fakeResolver) Resolve(_ context.Context, kind discovery.RouteKind, id int64) ([]discovery.Node, error) {
	f.kind = kind
	f.id = id
	return nil, nil
}

type blockingGuildResolver struct {
	userStarted chan struct{}
}

func (r *blockingGuildResolver) Resolve(ctx context.Context, kind discovery.RouteKind, _ int64) ([]discovery.Node, error) {
	if kind == discovery.RouteUser {
		close(r.userStarted)
		return nil, errors.New("stop dispatch")
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

type emptyRelationshipClient struct {
	userv1.UserServiceClient
}

type relationshipValue struct {
	targetID         int64
	relationshipType userv1.RelationshipType
}

type staticRelationshipClient struct {
	userv1.UserServiceClient
	relationships []relationshipValue
}

func (c staticRelationshipClient) ListRelationships(
	_ context.Context,
	req *userv1.ListRelationshipsRequest,
	_ ...grpc.CallOption,
) (*userv1.ListRelationshipsResponse, error) {
	resp := new(userv1.ListRelationshipsResponse)
	for _, value := range c.relationships {
		relationship := new(userv1.Relationship)
		relationship.SetUserId(req.GetUserId())
		relationship.SetTargetId(value.targetID)
		relationship.SetType(value.relationshipType)
		resp.SetRelationships(append(resp.GetRelationships(), relationship))
	}
	return resp, nil
}

type staticGuildClient struct {
	guildv1.GuildServiceClient
	guildIDs []int64
}

func (c staticGuildClient) ListUserGuilds(
	context.Context,
	*guildv1.ListUserGuildsRequest,
	...grpc.CallOption,
) (*guildv1.ListUserGuildsResponse, error) {
	resp := new(guildv1.ListUserGuildsResponse)
	for _, guildID := range c.guildIDs {
		guild := new(guildv1.Guild)
		guild.SetId(guildID)
		resp.SetGuilds(append(resp.GetGuilds(), guild))
	}
	return resp, nil
}

type staticMessageClient struct {
	messagev1.MessageServiceClient
	recipientIDs []int64
}

func (c staticMessageClient) ListDmChannels(
	_ context.Context,
	req *messagev1.ListDmChannelsRequest,
	_ ...grpc.CallOption,
) (*messagev1.ListDmChannelsResponse, error) {
	resp := new(messagev1.ListDmChannelsResponse)
	for index, recipientID := range c.recipientIDs {
		channel := new(messagev1.DmChannel)
		channel.SetId(int64(index + 1))
		channel.SetUserLo(min(req.GetUserId(), recipientID))
		channel.SetUserHi(max(req.GetUserId(), recipientID))
		resp.SetChannels(append(resp.GetChannels(), channel))
	}
	return resp, nil
}

func (emptyRelationshipClient) ListRelationships(
	context.Context,
	*userv1.ListRelationshipsRequest,
	...grpc.CallOption,
) (*userv1.ListRelationshipsResponse, error) {
	return new(userv1.ListRelationshipsResponse), nil
}
