package server

import (
	"context"
	"strconv"
	"time"

	"google.golang.org/grpc"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
)

type fakeStore struct {
	refreshed     []store.Route
	detached      []store.Route
	batchOwners   []store.Owner
	batchOwnerTTL time.Duration
	ownerBatches  [][]store.Owner
}

type fakeUser struct {
	userv1.UserServiceClient
}

func (fakeUser) BatchGetUserProfiles(
	_ context.Context,
	req *userv1.BatchGetUserProfilesRequest,
	_ ...grpc.CallOption,
) (*userv1.BatchGetUserProfilesResponse, error) {
	profiles := make([]*userv1.UserProfile, 0, len(req.GetUserIds()))
	for _, userID := range req.GetUserIds() {
		profile := new(userv1.UserProfile)
		profile.SetUserId(userID)
		profile.SetUsername("user_" + strconv.FormatInt(userID, 10))
		profile.SetName("User " + strconv.FormatInt(userID, 10))
		profile.SetAvatarAssetId(userID + 1000)
		profiles = append(profiles, profile)
	}
	resp := new(userv1.BatchGetUserProfilesResponse)
	resp.SetProfiles(profiles)
	return resp, nil
}

func (*fakeStore) SetOwner(context.Context, store.Owner, time.Duration) error { return nil }

func (s *fakeStore) SetOwners(_ context.Context, owners []store.Owner, ttl time.Duration) error {
	s.batchOwners = append([]store.Owner(nil), owners...)
	s.batchOwnerTTL = ttl
	s.ownerBatches = append(s.ownerBatches, append([]store.Owner(nil), owners...))
	return nil
}

func (*fakeStore) DeleteOwner(context.Context, string, string, string) error { return nil }

func (s *fakeStore) RefreshRoutes(_ context.Context, _, _ string, routes []store.Route, _ time.Duration) error {
	s.refreshed = append([]store.Route(nil), routes...)
	return nil
}

func (s *fakeStore) DetachRoutes(_ context.Context, _, _ string, routes []store.Route) error {
	s.detached = append(s.detached, routes...)
	return nil
}
