package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetUserProfilesDeduplicatesAndChunksRequests(t *testing.T) {
	userIDs := make([]int64, 0, 202)
	for userID := int64(1); userID <= 201; userID++ {
		userIDs = append(userIDs, userID)
	}
	userIDs = append(userIDs, 1)
	client := new(fakeUserClient)

	profiles, err := getUserProfiles(t.Context(), client, userIDs)

	require.NoError(t, err)
	require.Len(t, profiles, 201)
	require.Len(t, client.batchGetUserProfilesRequests, 3)
	require.Len(t, client.batchGetUserProfilesRequests[0].GetUserIds(), 100)
	require.Len(t, client.batchGetUserProfilesRequests[1].GetUserIds(), 100)
	require.Equal(t, []int64{201}, client.batchGetUserProfilesRequests[2].GetUserIds())
}
