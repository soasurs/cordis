package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
)

const maxRelationshipBatch = 100

// relationshipMutation captures what a transaction decided so events are
// published only after the commit succeeds.
type relationshipMutation struct {
	events         []userEvent
	result         *model.Relationship
	profiles       map[int64]*model.UserProfile
	idempotencyKey int64
}

func (m *relationshipMutation) updated(relationship *model.Relationship) {
	event, err := newRelationshipUpdatedEvent(relationship, m.profiles[relationship.TargetID], m.idempotencyKey)
	if err == nil {
		m.events = append(m.events, event)
	}
}

func (m *relationshipMutation) removed(userID, targetID int64) {
	event, err := newRelationshipRemovedEvent(userID, targetID, m.idempotencyKey)
	if err == nil {
		m.events = append(m.events, event)
	}
}

func (s *userServer) SendFriendRequest(ctx context.Context, req *userv1.SendFriendRequestRequest) (*userv1.SendFriendRequestResponse, error) {
	if err := validateRelationshipPair(req.GetUserId(), req.GetTargetId()); err != nil {
		return nil, err
	}
	if err := s.requireUserExists(ctx, req.GetTargetId()); err != nil {
		return nil, err
	}
	profiles, err := s.getRelationshipEventProfiles(ctx, req.GetUserId(), req.GetTargetId())
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	mutation := &relationshipMutation{profiles: profiles, idempotencyKey: s.svcCtx.Snowflake.Generate().Int64()}
	err = s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		if err := tx.LockRelationshipPair(ctx, req.GetUserId(), req.GetTargetId()); err != nil {
			return err
		}
		own, err := getRelationship(ctx, tx, req.GetUserId(), req.GetTargetId())
		if err != nil {
			return err
		}
		reverse, err := getRelationship(ctx, tx, req.GetTargetId(), req.GetUserId())
		if err != nil {
			return err
		}
		if relationshipType(own) == model.RelationshipBlocked || relationshipType(reverse) == model.RelationshipBlocked {
			return relationshipBlocked()
		}

		switch relationshipType(own) {
		case model.RelationshipFriend:
			return relationshipAlreadyExists()
		case model.RelationshipOutgoing:
			// Repeating a pending request is a no-op.
			mutation.result = own
			return nil
		case model.RelationshipIncoming:
			// The target already asked: mutual intent becomes a friendship.
			return acceptIntoFriendship(ctx, tx, mutation, req.GetUserId(), req.GetTargetId(), now)
		}

		ownRow := &model.Relationship{UserID: req.GetUserId(), TargetID: req.GetTargetId(), Type: model.RelationshipOutgoing, CreatedAt: now}
		reverseRow := &model.Relationship{UserID: req.GetTargetId(), TargetID: req.GetUserId(), Type: model.RelationshipIncoming, CreatedAt: now}
		if err := upsertPairOrdered(ctx, tx, ownRow, reverseRow); err != nil {
			return err
		}
		mutation.result = ownRow
		mutation.updated(ownRow)
		mutation.updated(reverseRow)
		return nil
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	s.publishEvents(ctx, mutation.events...)

	resp := new(userv1.SendFriendRequestResponse)
	resp.SetRelationship(relationshipToProto(mutation.result))
	return resp, nil
}

func (s *userServer) AcceptFriendRequest(ctx context.Context, req *userv1.AcceptFriendRequestRequest) (*userv1.AcceptFriendRequestResponse, error) {
	if err := validateRelationshipPair(req.GetUserId(), req.GetTargetId()); err != nil {
		return nil, err
	}
	current, err := getRelationship(ctx, s.svcCtx.Store, req.GetUserId(), req.GetTargetId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	if relationshipType(current) != model.RelationshipIncoming {
		return nil, relationshipNotFound()
	}
	profiles, err := s.getRelationshipEventProfiles(ctx, req.GetUserId(), req.GetTargetId())
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	mutation := &relationshipMutation{profiles: profiles, idempotencyKey: s.svcCtx.Snowflake.Generate().Int64()}
	err = s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		if err := tx.LockRelationshipPair(ctx, req.GetUserId(), req.GetTargetId()); err != nil {
			return err
		}
		own, err := getRelationship(ctx, tx, req.GetUserId(), req.GetTargetId())
		if err != nil {
			return err
		}
		if relationshipType(own) != model.RelationshipIncoming {
			return relationshipNotFound()
		}
		return acceptIntoFriendship(ctx, tx, mutation, req.GetUserId(), req.GetTargetId(), now)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	s.publishEvents(ctx, mutation.events...)

	resp := new(userv1.AcceptFriendRequestResponse)
	resp.SetRelationship(relationshipToProto(mutation.result))
	return resp, nil
}

func (s *userServer) DeclineFriendRequest(ctx context.Context, req *userv1.DeclineFriendRequestRequest) (*userv1.DeclineFriendRequestResponse, error) {
	if err := validateRelationshipPair(req.GetUserId(), req.GetTargetId()); err != nil {
		return nil, err
	}

	mutation := &relationshipMutation{idempotencyKey: s.svcCtx.Snowflake.Generate().Int64()}
	err := s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		if err := tx.LockRelationshipPair(ctx, req.GetUserId(), req.GetTargetId()); err != nil {
			return err
		}
		own, err := getRelationship(ctx, tx, req.GetUserId(), req.GetTargetId())
		if err != nil {
			return err
		}
		if relationshipType(own) != model.RelationshipIncoming {
			return relationshipNotFound()
		}
		return removePair(ctx, tx, mutation, req.GetUserId(), req.GetTargetId())
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	s.publishEvents(ctx, mutation.events...)

	resp := new(userv1.DeclineFriendRequestResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *userServer) RemoveFriend(ctx context.Context, req *userv1.RemoveFriendRequest) (*userv1.RemoveFriendResponse, error) {
	if err := validateRelationshipPair(req.GetUserId(), req.GetTargetId()); err != nil {
		return nil, err
	}

	mutation := &relationshipMutation{idempotencyKey: s.svcCtx.Snowflake.Generate().Int64()}
	err := s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		if err := tx.LockRelationshipPair(ctx, req.GetUserId(), req.GetTargetId()); err != nil {
			return err
		}
		own, err := getRelationship(ctx, tx, req.GetUserId(), req.GetTargetId())
		if err != nil {
			return err
		}
		switch relationshipType(own) {
		case model.RelationshipFriend, model.RelationshipOutgoing, model.RelationshipIncoming:
			return removePair(ctx, tx, mutation, req.GetUserId(), req.GetTargetId())
		default:
			// Blocks are lifted through UnblockUser only.
			return relationshipNotFound()
		}
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	s.publishEvents(ctx, mutation.events...)

	resp := new(userv1.RemoveFriendResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *userServer) BlockUser(ctx context.Context, req *userv1.BlockUserRequest) (*userv1.BlockUserResponse, error) {
	if err := validateRelationshipPair(req.GetUserId(), req.GetTargetId()); err != nil {
		return nil, err
	}
	if err := s.requireUserExists(ctx, req.GetTargetId()); err != nil {
		return nil, err
	}
	profiles, err := s.getRelationshipEventProfiles(ctx, req.GetUserId(), req.GetTargetId())
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	mutation := &relationshipMutation{profiles: profiles, idempotencyKey: s.svcCtx.Snowflake.Generate().Int64()}
	err = s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		if err := tx.LockRelationshipPair(ctx, req.GetUserId(), req.GetTargetId()); err != nil {
			return err
		}
		own, err := getRelationship(ctx, tx, req.GetUserId(), req.GetTargetId())
		if err != nil {
			return err
		}
		if relationshipType(own) == model.RelationshipBlocked {
			// Re-blocking is a no-op.
			mutation.result = own
			return nil
		}

		reverse, err := getRelationship(ctx, tx, req.GetTargetId(), req.GetUserId())
		if err != nil {
			return err
		}
		stripReverse := reverse != nil && reverse.Type != model.RelationshipBlocked

		ownRow := &model.Relationship{UserID: req.GetUserId(), TargetID: req.GetTargetId(), Type: model.RelationshipBlocked, CreatedAt: now}
		writeOwn := func() error { return tx.UpsertRelationship(ctx, ownRow) }
		deleteReverse := func() error {
			if !stripReverse {
				return nil
			}
			return tx.DeleteRelationshipExceptBlocked(ctx, req.GetTargetId(), req.GetUserId())
		}
		// Same fixed lock order as upsertPairOrdered: lower user ID first.
		first, second := writeOwn, deleteReverse
		if req.GetTargetId() < req.GetUserId() {
			first, second = deleteReverse, writeOwn
		}
		if err := first(); err != nil {
			return err
		}
		if err := second(); err != nil {
			return err
		}

		mutation.result = ownRow
		// The block is private: only the blocker's devices learn the type.
		mutation.updated(ownRow)
		if stripReverse {
			// The other side only sees the relationship disappear.
			mutation.removed(req.GetTargetId(), req.GetUserId())
		}
		return nil
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	s.publishEvents(ctx, mutation.events...)

	resp := new(userv1.BlockUserResponse)
	resp.SetRelationship(relationshipToProto(mutation.result))
	return resp, nil
}

func (s *userServer) UnblockUser(ctx context.Context, req *userv1.UnblockUserRequest) (*userv1.UnblockUserResponse, error) {
	if err := validateRelationshipPair(req.GetUserId(), req.GetTargetId()); err != nil {
		return nil, err
	}

	mutation := &relationshipMutation{idempotencyKey: s.svcCtx.Snowflake.Generate().Int64()}
	err := s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		if err := tx.LockRelationshipPair(ctx, req.GetUserId(), req.GetTargetId()); err != nil {
			return err
		}
		own, err := getRelationship(ctx, tx, req.GetUserId(), req.GetTargetId())
		if err != nil {
			return err
		}
		if relationshipType(own) != model.RelationshipBlocked {
			return relationshipNotFound()
		}
		// Only the caller's row is removed; a block held by the other side
		// stays in place.
		if err := tx.DeleteRelationship(ctx, req.GetUserId(), req.GetTargetId()); err != nil {
			return err
		}
		mutation.removed(req.GetUserId(), req.GetTargetId())
		return nil
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	s.publishEvents(ctx, mutation.events...)

	resp := new(userv1.UnblockUserResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *userServer) ListRelationships(ctx context.Context, req *userv1.ListRelationshipsRequest) (*userv1.ListRelationshipsResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, errUserIDRequired
	}
	token, err := readCursor(req.HasCursor(), req.GetCursor())
	if err != nil {
		return nil, err
	}
	payload, ok, err := cursor.Decode[relationshipCursorPayload](s.svcCtx.Cursors, cursor.KindRelationships, token)
	if err != nil {
		return nil, errInvalidCursor
	}
	var beforeCreatedAt, beforeTargetID int64
	if ok {
		if payload.UserID != req.GetUserId() || payload.Type != int32(req.GetType()) || payload.Time <= 0 || payload.ID <= 0 {
			return nil, errInvalidCursor
		}
		beforeCreatedAt, beforeTargetID = payload.Time, payload.ID
	}
	limit, err := normalizeRelationshipLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}

	relationships, err := s.svcCtx.Store.ListRelationships(ctx, store.ListRelationshipsParams{
		UserID:          req.GetUserId(),
		Type:            int16(req.GetType()),
		BeforeCreatedAt: beforeCreatedAt,
		BeforeTargetID:  beforeTargetID,
		Limit:           limit + 1,
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	page, hasMore := cursor.Trim(relationships, limit)

	resp := new(userv1.ListRelationshipsResponse)
	resp.SetRelationships(relationshipsToProto(page))
	if hasMore && len(page) > 0 {
		last := page[len(page)-1]
		if last.CreatedAt <= 0 || last.TargetID <= 0 {
			return nil, status.Error(codes.Internal, "failed to encode cursor")
		}
		next, err := s.svcCtx.Cursors.Encode(cursor.KindRelationships, relationshipCursorPayload{
			UserID: req.GetUserId(),
			Type:   int32(req.GetType()),
			Time:   last.CreatedAt,
			ID:     last.TargetID,
		})
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to encode cursor")
		}
		resp.SetNextCursor(next)
	}
	return resp, nil
}

func (s *userServer) CheckRelationships(ctx context.Context, req *userv1.CheckRelationshipsRequest) (*userv1.CheckRelationshipsResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, errUserIDRequired
	}
	targetIDs := req.GetTargetIds()
	if len(targetIDs) == 0 {
		return new(userv1.CheckRelationshipsResponse), nil
	}
	if len(targetIDs) > maxRelationshipBatch {
		return nil, errBatchTooLarge
	}

	list := s.svcCtx.Store.ListRelationshipsByTargets
	if req.GetIncludeReverse() {
		list = s.svcCtx.Store.ListRelationshipsBidirectional
	}
	relationships, err := list(ctx, req.GetUserId(), targetIDs)
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(userv1.CheckRelationshipsResponse)
	resp.SetRelationships(relationshipsToProto(relationships))
	return resp, nil
}

// acceptIntoFriendship flips a pending pair into a friendship and queues
// both users' update events.
func acceptIntoFriendship(ctx context.Context, tx store.Store, mutation *relationshipMutation, userID, targetID, now int64) error {
	ownRow := &model.Relationship{UserID: userID, TargetID: targetID, Type: model.RelationshipFriend, CreatedAt: now}
	reverseRow := &model.Relationship{UserID: targetID, TargetID: userID, Type: model.RelationshipFriend, CreatedAt: now}
	if err := upsertPairOrdered(ctx, tx, ownRow, reverseRow); err != nil {
		return err
	}
	mutation.result = ownRow
	mutation.updated(ownRow)
	mutation.updated(reverseRow)
	return nil
}

// removePair deletes both directions of a non-blocked pair and queues both
// users' removal events. Deletions run in ascending row-owner order for the
// same reason upsertPairOrdered exists.
func removePair(ctx context.Context, tx store.Store, mutation *relationshipMutation, userID, targetID int64) error {
	deleteOwn := func() error { return tx.DeleteRelationship(ctx, userID, targetID) }
	deleteReverse := func() error { return tx.DeleteRelationshipExceptBlocked(ctx, targetID, userID) }
	first, second := deleteOwn, deleteReverse
	if targetID < userID {
		first, second = deleteReverse, deleteOwn
	}
	if err := first(); err != nil {
		return err
	}
	if err := second(); err != nil {
		return err
	}
	mutation.removed(userID, targetID)
	mutation.removed(targetID, userID)
	return nil
}

// upsertPairOrdered writes both directions of a pair with the lower user ID
// first. Concurrent mutations of the same pair from both sides then acquire
// row locks in the same order and cannot deadlock.
func upsertPairOrdered(ctx context.Context, tx store.Store, first, second *model.Relationship) error {
	if second.UserID < first.UserID {
		first, second = second, first
	}
	if err := tx.UpsertRelationship(ctx, first); err != nil {
		return err
	}
	return tx.UpsertRelationship(ctx, second)
}

// getRelationship returns nil when no row exists.
func getRelationship(ctx context.Context, tx store.Store, userID, targetID int64) (*model.Relationship, error) {
	relationship, err := tx.GetRelationship(ctx, userID, targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return relationship, nil
}

func relationshipType(relationship *model.Relationship) int16 {
	if relationship == nil {
		return 0
	}
	return relationship.Type
}

// requireUserExists rejects relationship mutations aimed at unknown users.
func (s *userServer) requireUserExists(ctx context.Context, userID int64) error {
	if _, err := s.svcCtx.Store.GetUser(ctx, userID); err != nil {
		return mapStoreError(err)
	}
	return nil
}

func (s *userServer) getRelationshipEventProfiles(
	ctx context.Context,
	userIDs ...int64,
) (map[int64]*model.UserProfile, error) {
	profiles := make(map[int64]*model.UserProfile, len(userIDs))
	for _, userID := range userIDs {
		if profiles[userID] != nil {
			continue
		}
		profile, err := s.svcCtx.Store.GetUserProfile(ctx, userID)
		if err != nil {
			return nil, mapStoreError(err)
		}
		profiles[userID] = profile
	}
	return profiles, nil
}

func validateRelationshipPair(userID, targetID int64) error {
	if userID <= 0 {
		return errUserIDRequired
	}
	if targetID <= 0 {
		return errTargetIDRequired
	}
	if userID == targetID {
		return errSelfRelationship
	}
	return nil
}

func relationshipNotFound() error {
	return rpcerror.New(codes.NotFound, rpcerror.UserDomain, rpcerror.UserRelationshipNotFound, "relationship not found")
}

func relationshipAlreadyExists() error {
	return rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserRelationshipAlreadyExists, "relationship already exists")
}

func relationshipBlocked() error {
	return rpcerror.New(codes.PermissionDenied, rpcerror.UserDomain, rpcerror.UserRelationshipBlocked, "relationship is blocked")
}
