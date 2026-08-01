package server

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func TestProfileProjectorRebuildsActiveMemberProfiles(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002)
	fakeStore.members[10][1002].Nickname = "Guild User"
	projector := &profileProjector{store: fakeStore, user: &fakeUserClient{}}

	require.NoError(t, projector.rebuildProfiles(t.Context()))
	require.Equal(t, "user_1001", fakeStore.profiles[10][1001].Username)
	require.Equal(t, "User 1002", fakeStore.profiles[10][1002].Name)
	require.Equal(t, "Guild User", fakeStore.profiles[10][1002].Nickname)
}

func TestProfileProjectorAppliesProfileEventsAndIgnoresStaleEvents(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.profiles[10] = map[int64]*model.GuildMemberProfile{
		1001: {GuildID: 10, UserID: 1001, Username: "old", Nickname: "Guild User", ProfileUpdatedAt: 5},
	}
	projector := &profileProjector{store: fakeStore}
	value, err := json.Marshal(eventEnvelope[userProfilePayload]{
		Type: realtime.EventUserProfileUpdated,
		Data: userProfilePayload{
			UserID: "1001", Username: "new_name", Name: "New Name", AvatarAssetID: "42", UpdatedAt: 6,
		},
	})
	require.NoError(t, err)
	require.NoError(t, projector.handleRecord(t.Context(), &kgo.Record{Value: value}))
	require.Equal(t, "new_name", fakeStore.profiles[10][1001].Username)
	require.Equal(t, int64(6), fakeStore.profiles[10][1001].ProfileUpdatedAt)
	require.Equal(t, "Guild User", fakeStore.profiles[10][1001].Nickname)

	stale, err := json.Marshal(eventEnvelope[userProfilePayload]{
		Type: realtime.EventUserProfileUpdated,
		Data: userProfilePayload{
			UserID: "1001", Username: "stale", Name: "Stale", AvatarAssetID: "7", UpdatedAt: 4,
		},
	})
	require.NoError(t, err)
	require.NoError(t, projector.handleRecord(t.Context(), &kgo.Record{Value: stale}))
	require.Equal(t, "new_name", fakeStore.profiles[10][1001].Username)
}

func TestProfileProjectorAppliesProfileFieldsWhenAvatarIsMissing(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.profiles[10] = map[int64]*model.GuildMemberProfile{
		1001: {GuildID: 10, UserID: 1001, Username: "old", Name: "Old", AvatarAssetID: 42, ProfileUpdatedAt: 5},
	}
	projector := &profileProjector{store: fakeStore}
	value := []byte(`{"t":"user.profile.updated","d":{"user_id":"1001","username":"new_name","name":"New Name","updated_at":6}}`)

	require.NoError(t, projector.handleRecord(t.Context(), &kgo.Record{Value: value}))
	profile := fakeStore.profiles[10][1001]
	require.Equal(t, "new_name", profile.Username)
	require.Equal(t, "New Name", profile.Name)
	require.Equal(t, int64(42), profile.AvatarAssetID)
	require.Equal(t, int64(6), profile.ProfileUpdatedAt)
}

func TestProfileProjectorBoundsStartupRebuildRetries(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001)
	userClient := &fakeUserClient{err: errors.New("user unavailable")}
	projector := &profileProjector{store: fakeStore, user: userClient}

	projector.rebuildProfilesWithRetry(t.Context(), 2, 0, 0)

	require.Equal(t, 2, userClient.batchCalls)
}
