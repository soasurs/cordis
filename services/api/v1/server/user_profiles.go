package server

import (
	"context"
	"errors"
	"slices"

	"connectrpc.com/connect"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/apierror"
)

const maxUserProfileBatchSize = 100

func getUserProfiles(
	ctx context.Context,
	client userv1.UserServiceClient,
	userIDs []int64,
) (map[int64]*userv1.UserProfile, error) {
	if client == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("user service client is unavailable"))
	}
	uniqueUserIDs := make([]int64, 0, len(userIDs))
	expected := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			return nil, connect.NewError(connect.CodeInternal, errors.New("upstream service returned an invalid user id"))
		}
		if _, ok := expected[userID]; ok {
			continue
		}
		expected[userID] = struct{}{}
		uniqueUserIDs = append(uniqueUserIDs, userID)
	}

	profiles := make(map[int64]*userv1.UserProfile, len(uniqueUserIDs))
	for chunk := range slices.Chunk(uniqueUserIDs, maxUserProfileBatchSize) {
		req := new(userv1.BatchGetUserProfilesRequest)
		req.SetUserIds(chunk)
		resp, err := client.BatchGetUserProfiles(ctx, req)
		if err != nil {
			return nil, apierror.FromRPC(err)
		}
		if resp == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("user service returned an invalid response"))
		}

		chunkExpected := make(map[int64]struct{}, len(chunk))
		for _, userID := range chunk {
			chunkExpected[userID] = struct{}{}
		}
		for _, profile := range resp.GetProfiles() {
			if profile == nil {
				return nil, connect.NewError(connect.CodeInternal, errors.New("user service returned an invalid profile"))
			}
			userID := profile.GetUserId()
			if _, ok := chunkExpected[userID]; !ok {
				return nil, connect.NewError(connect.CodeInternal, errors.New("user service returned an unexpected profile"))
			}
			if profiles[userID] != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.New("user service returned a duplicate profile"))
			}
			profiles[userID] = profile
		}
		for _, userID := range chunk {
			if profiles[userID] == nil {
				return nil, connect.NewError(connect.CodeInternal, errors.New("user service did not return all profiles"))
			}
		}
	}
	return profiles, nil
}
