package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

func TestDispatchRecordUsesDotSeparatedMessageEvent(t *testing.T) {
	resolver := &fakeResolver{}
	server := &Server{resolver: resolver}
	value := []byte(`{"t":"` + realtime.EventMessageCreated + `","d":{"id":"1","channel_id":"7001"}}`)
	permanent, err := server.dispatchRecord(t.Context(), &kgo.Record{Value: value})
	require.NoError(t, err)
	require.False(t, permanent)
	require.Equal(t, discovery.RouteChannel, resolver.kind)
	require.Equal(t, int64(7001), resolver.id)
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
	require.Equal(t, "reaction.added", realtime.EventReactionAdded)
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
