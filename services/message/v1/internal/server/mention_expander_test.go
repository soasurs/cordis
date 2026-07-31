package server

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

func expanderRecord(t *testing.T, envelope eventEnvelope[messagePayload]) *kgo.Record {
	t.Helper()
	payload, err := json.Marshal(envelope)
	require.NoError(t, err)
	return &kgo.Record{Value: payload}
}

func TestMentionExpanderIgnoresNonExpansionEvents(t *testing.T) {
	fakeStore := newFakeStore()
	expander := &mentionExpander{store: fakeStore, guild: &fakeGuildClient{}}

	tests := []eventEnvelope[messagePayload]{
		{Type: EventTypeMessageDeleted, Data: messagePayload{MessageID: "100", GuildID: "9001", ChannelID: "10"}},
		{Type: EventTypeMessageCreated, Data: messagePayload{MessageID: "100", GuildID: "9001", ChannelID: "10"}},
		{Type: EventTypeMessageCreated, Data: messagePayload{
			MessageID: "100", GuildID: "9001", ChannelID: "10",
			MentionUserIDs: []string{"30"},
		}},
		{Type: EventTypeMessageUpdated, Data: messagePayload{
			MessageID: "100", GuildID: "9001", ChannelID: "10", Revision: 3,
			MentionRoleIDs: []string{"40"}, MentionEveryone: true,
		}},
		{Type: EventTypeMessageCreated, Data: messagePayload{
			MessageID: "100", ChannelID: "10", MentionRoleIDs: []string{"40"},
		}},
		{Type: EventTypeMessageCreated, Data: messagePayload{
			MessageID: "100", GuildID: "9001", ChannelID: "10", MentionRoleIDs: []string{"not-a-number"},
		}},
	}
	for _, envelope := range tests {
		require.NoError(t, expander.handleRecord(t.Context(), expanderRecord(t, envelope)))
	}
	require.Empty(t, fakeStore.rebuildBatches)
}

func TestMentionExpanderSkipsMissingOrStaleMessages(t *testing.T) {
	fakeStore := newFakeStore()
	expander := &mentionExpander{store: fakeStore, guild: &fakeGuildClient{}}

	envelope := eventEnvelope[messagePayload]{Type: EventTypeMessageCreated, Data: messagePayload{
		MessageID: "100", GuildID: "9001", ChannelID: "10", Revision: 3,
		MentionRoleIDs: []string{"40"}, MentionEveryone: true,
	}}
	require.NoError(t, expander.handleRecord(t.Context(), expanderRecord(t, envelope)))
	require.Empty(t, fakeStore.rebuildBatches)

	fakeStore.messages[100] = &model.Message{
		ID: 100, ChannelID: 10, AuthorID: 20, Revision: 2, Content: "x",
	}
	require.NoError(t, expander.handleRecord(t.Context(), expanderRecord(t, envelope)))
	require.Empty(t, fakeStore.rebuildBatches)

	fakeStore.messages[100].Revision = 3
	fakeStore.messages[100].ChannelID = 11
	require.NoError(t, expander.handleRecord(t.Context(), expanderRecord(t, envelope)))
	require.Empty(t, fakeStore.rebuildBatches)
}

func TestMentionExpanderExpandsInPagesAndBatches(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[100] = &model.Message{
		ID: 100, ChannelID: 10, AuthorID: 20, Revision: 3, Content: "x",
	}
	targets := make([]int64, 20_001)
	for i := range targets {
		targets[i] = int64(i + 1)
	}
	guildClient := &fakeGuildClient{mentionTargetPages: [][]int64{
		targets[:10_000], targets[10_000:20_000], targets[20_000:],
	}}
	expander := &mentionExpander{store: fakeStore, guild: guildClient}

	envelope := eventEnvelope[messagePayload]{Type: EventTypeMessageUpdated, Data: messagePayload{
		MessageID: "100", GuildID: "9001", ChannelID: "10", Revision: 3,
		MentionRoleIDs: []string{"40", "50"}, MentionEveryone: true, RebuildMentions: true,
	}}
	require.NoError(t, expander.handleRecord(t.Context(), expanderRecord(t, envelope)))

	require.Equal(t, [][]int64{targets}, fakeStore.rebuildBatches)
	require.Len(t, fakeStore.mentions[100].UserIDs, 20_001)
}

func TestMentionExpanderSkipsStaleAtomicRebuild(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[100] = &model.Message{
		ID: 100, ChannelID: 10, AuthorID: 20, Revision: 3, Content: "x",
	}
	// The revision matched when paging started, but the store detects the
	// message was edited (or deleted) before the rebuild transaction ran.
	fakeStore.rebuildStale = true
	expander := &mentionExpander{store: fakeStore, guild: &fakeGuildClient{mentionTargets: []int64{30, 40}}}

	envelope := eventEnvelope[messagePayload]{Type: EventTypeMessageCreated, Data: messagePayload{
		MessageID: "100", GuildID: "9001", ChannelID: "10", Revision: 3,
		MentionEveryone: true,
	}}
	require.NoError(t, expander.handleRecord(t.Context(), expanderRecord(t, envelope)))
	require.Empty(t, fakeStore.rebuildBatches)
	require.Empty(t, fakeStore.mentions[100].UserIDs)
}

func TestMentionExpanderPropagatesStoreErrors(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.getMessageErr = errors.New("database down")
	expander := &mentionExpander{store: fakeStore, guild: &fakeGuildClient{}}

	envelope := eventEnvelope[messagePayload]{Type: EventTypeMessageCreated, Data: messagePayload{
		MessageID: "100", GuildID: "9001", ChannelID: "10",
		MentionRoleIDs: []string{"40"},
	}}
	require.Error(t, expander.handleRecord(t.Context(), expanderRecord(t, envelope)))
}
