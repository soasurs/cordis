package server

import (
	"context"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

const maxUserProfileBatch = 100

func (s *userServer) BatchGetUserProfiles(ctx context.Context, req *userv1.BatchGetUserProfilesRequest) (*userv1.BatchGetUserProfilesResponse, error) {
	userIDs := req.GetUserIds()
	if len(userIDs) > maxUserProfileBatch {
		return nil, errProfileBatchTooLarge
	}
	uniqueUserIDs := make([]int64, 0, len(userIDs))
	seen := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			return nil, errUserIDRequired
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		uniqueUserIDs = append(uniqueUserIDs, userID)
	}

	resp := new(userv1.BatchGetUserProfilesResponse)
	if len(uniqueUserIDs) == 0 {
		return resp, nil
	}
	profiles, err := s.svcCtx.Store.ListUserProfiles(ctx, uniqueUserIDs)
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp.SetProfiles(userProfilesToProto(profiles))
	return resp, nil
}
