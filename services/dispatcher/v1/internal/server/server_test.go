package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

func TestDispatchRecordRoutesGuildMessageByGuild(t *testing.T) {
	resolver := &fakeResolver{}
	server := &Server{resolver: resolver}
	value := []byte(`{"t":"` + realtime.EventMessageCreated + `","d":{"id":"1","guild_id":"8001","channel_id":"7001"}}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.NoError(t, err)
	require.False(t, permanent)
	require.Equal(t, discovery.RouteGuild, resolver.kind)
	require.Equal(t, int64(8001), resolver.id)
}

func TestDispatchRecordRoutesDmMessageByRecipient(t *testing.T) {
	resolver := &fakeResolver{}
	server := &Server{resolver: resolver}
	value := []byte(`{"t":"` + realtime.EventMessageCreated + `","d":{"id":"1","channel_id":"7001","user_id":"1001"}}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.NoError(t, err)
	require.False(t, permanent)
	require.Equal(t, discovery.RouteUser, resolver.kind)
	require.Equal(t, int64(1001), resolver.id)
}

func TestDispatchRecordRoutesReadUpdateByUser(t *testing.T) {
	resolver := &fakeResolver{}
	server := &Server{resolver: resolver}
	value := []byte(`{"t":"` + realtime.EventMessageReadUpdated + `","d":{"user_id":"1001","channel_id":"7001","last_read_message_id":"8001"}}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.NoError(t, err)
	require.False(t, permanent)
	require.Equal(t, discovery.RouteUser, resolver.kind)
	require.Equal(t, int64(1001), resolver.id)
}

func TestDispatchRecordRejectsMessageWithoutAggregateRoute(t *testing.T) {
	server := &Server{resolver: &fakeResolver{}}
	value := []byte(`{"t":"` + realtime.EventMessageCreated + `","d":{"id":"1","channel_id":"7001"}}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.Error(t, err)
	require.True(t, permanent)
}

func TestDispatchRecordAcceptsStringGuildIDs(t *testing.T) {
	resolver := &fakeResolver{}
	server := &Server{resolver: resolver}
	value := []byte(`{"t":"` + realtime.EventGuildUpdated + `","d":{"id":"8001"}}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.NoError(t, err)
	require.False(t, permanent)
	require.Equal(t, discovery.RouteGuild, resolver.kind)
	require.Equal(t, int64(8001), resolver.id)
}

func TestDispatchRecordRejectsUnderscoreEventName(t *testing.T) {
	server := &Server{resolver: &fakeResolver{}}
	value := []byte(`{"t":"message_created","d":{"id":"1","channel_id":"7001"}}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.Error(t, err)
	require.True(t, permanent)
}

func TestEventConstantsUseDotSeparator(t *testing.T) {
	require.Equal(t, "message.created", realtime.EventMessageCreated)
	require.Equal(t, "message.read.updated", realtime.EventMessageReadUpdated)
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
		done <- server.dispatchPresence(t.Context(), 1001, eventEnvelope{Type: realtime.EventPresenceUpdated}, routing)
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

func (emptyRelationshipClient) ListRelationships(
	context.Context,
	*userv1.ListRelationshipsRequest,
	...grpc.CallOption,
) (*userv1.ListRelationshipsResponse, error) {
	return new(userv1.ListRelationshipsResponse), nil
}
