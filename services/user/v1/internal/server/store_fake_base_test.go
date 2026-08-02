package server

import (
	"context"

	"github.com/soasurs/cordis/services/user/v1/internal/model"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
)

type fakeStore struct {
	user                *model.User
	profile             *model.UserProfile
	eventProfiles       map[int64]*model.UserProfile
	batchProfiles       []*model.UserProfile
	listProfileIDs      []int64
	createUserErr       error
	createProfileErr    error
	updateUsernameErr   error
	getUserWithEmailErr error
	emailAvailable      bool
	usernameAvailable   bool
	relationships       map[[2]int64]*model.Relationship
	lockedPairs         [][2]int64
}

func (s *fakeStore) LockRelationshipPair(_ context.Context, userID, targetID int64) error {
	s.lockedPairs = append(s.lockedPairs, [2]int64{userID, targetID})
	return nil
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		relationships: make(map[[2]int64]*model.Relationship),
		eventProfiles: make(map[int64]*model.UserProfile),
	}
}

func (s *fakeStore) Transact(ctx context.Context, fn func(txStore store.Store) error) error {
	return fn(s)
}
