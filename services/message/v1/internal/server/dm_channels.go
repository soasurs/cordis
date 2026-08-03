package server

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/message/v1/internal/eventoutbox"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

const (
	relationshipTypeFriend  = int32(userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND)
	relationshipTypeBlocked = int32(userv1.RelationshipType_RELATIONSHIP_TYPE_BLOCKED)
)

func (s *messageServer) CreateDmChannel(ctx context.Context, req *messagev1.CreateDmChannelRequest) (*messagev1.CreateDmChannelResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	if req.GetTargetId() <= 0 {
		return nil, invalidRequest("target id is required")
	}
	if req.GetUserId() == req.GetTargetId() {
		return nil, invalidRequest("cannot open a direct message channel with yourself")
	}

	// Opening a DM requires an active friendship. Friendship rows are
	// symmetric, so the caller's perspective is enough.
	relationships, err := s.checkRelationships(ctx, req.GetUserId(), req.GetTargetId(), false)
	if err != nil {
		return nil, err
	}
	if relationships[req.GetUserId()] != relationshipTypeFriend {
		return nil, dmRequiresFriendship()
	}

	userLo, userHi := orderedPair(req.GetUserId(), req.GetTargetId())

	// Idempotent open: return the existing channel without a new event.
	existing, err := s.svcCtx.Store.GetDmChannelByPair(ctx, userLo, userHi)
	if err == nil {
		resp := new(messagev1.CreateDmChannelResponse)
		resp.SetChannel(dmChannelToProto(existing))
		return resp, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	profiles, err := s.getDmParticipantProfiles(ctx, userLo, userHi)
	if err != nil {
		return nil, err
	}

	var channel *model.DmChannel
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		draft := &model.DmChannel{
			ID:        s.svcCtx.Snowflake.Generate().Int64(),
			UserLo:    userLo,
			UserHi:    userHi,
			CreatedAt: time.Now().UnixMilli(),
		}
		if err := txStore.CreateDmChannel(ctx, draft); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			// Lost a concurrent open: the other insert won, reuse its row
			// without writing outbox rows.
			existing, getErr := txStore.GetDmChannelByPair(ctx, userLo, userHi)
			if getErr != nil {
				return getErr
			}
			channel = existing
			return nil
		}
		channel = draft

		eventID := s.svcCtx.Snowflake.Generate().Int64()
		events := make([]messageEvent, 0, 2)
		for index, recipientID := range []int64{draft.UserLo, draft.UserHi} {
			event, err := newDmChannelCreatedEvent(
				draft,
				recipientID,
				profiles[draft.OtherParticipant(recipientID)],
				eventID,
				index,
			)
			if err != nil {
				return err
			}
			events = append(events, event)
		}
		return s.enqueueMessageEvents(
			ctx,
			txStore,
			events,
			s.svcCtx.Cfg.Kafka.EventTopic(),
			s.svcCtx.Cfg.Outbox.MessageShards(),
			eventoutbox.MessageNotifyChannel,
		)
	})
	if err != nil {
		return nil, err
	}

	resp := new(messagev1.CreateDmChannelResponse)
	resp.SetChannel(dmChannelToProto(channel))
	return resp, nil
}

func (s *messageServer) ListDmChannels(ctx context.Context, req *messagev1.ListDmChannelsRequest) (*messagev1.ListDmChannelsResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	token, err := readCursor(req.HasCursor(), req.GetCursor())
	if err != nil {
		return nil, err
	}
	payload, ok, err := cursor.Decode[dmChannelsPayload](s.svcCtx.Cursors, cursor.KindDmChannels, token)
	if err != nil {
		return nil, invalidRequest("cursor is invalid")
	}
	var beforeID int64
	if ok {
		if payload.UserID != req.GetUserId() || payload.ID <= 0 {
			return nil, invalidRequest("cursor is invalid")
		}
		beforeID = payload.ID
	}
	limit, err := normalizeLimit(req.GetLimit(), defaultMessageLimit, maxMessageLimit)
	if err != nil {
		return nil, err
	}

	channels, err := s.svcCtx.Store.ListDmChannels(ctx, store.ListDmChannelsParams{
		UserID:   req.GetUserId(),
		BeforeID: beforeID,
		Limit:    limit + 1,
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	page, hasMore := cursor.Trim(channels, limit)

	resp := new(messagev1.ListDmChannelsResponse)
	values := make([]*messagev1.DmChannel, 0, len(page))
	for _, channel := range page {
		values = append(values, dmChannelToProto(channel))
	}
	resp.SetChannels(values)
	if hasMore && len(page) > 0 {
		id := page[len(page)-1].ID
		if id <= 0 {
			return nil, status.Error(codes.Internal, "failed to encode cursor")
		}
		next, err := s.svcCtx.Cursors.Encode(cursor.KindDmChannels, dmChannelsPayload{UserID: req.GetUserId(), ID: id})
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to encode cursor")
		}
		resp.SetNextCursor(next)
	}
	return resp, nil
}

// authorizeDmMessage enforces DM semantics for the message RPCs: only
// participants may act, nobody holds moderator powers, and sending requires
// that neither side blocks the other.
func (s *messageServer) authorizeDmMessage(ctx context.Context, channel *model.DmChannel, userID int64, permission uint64) error {
	if !channel.Participates(userID) {
		// Hide the channel's existence from outsiders.
		return notFound()
	}
	if permission&permissionManageMessages != 0 {
		// DMs have no moderators; non-authors can never edit or delete.
		return permissionDenied()
	}
	if permission&permissionSendMessages == 0 {
		return nil
	}

	// Writing requires that neither direction holds a block. Both rows come
	// from one snapshot query on the User side.
	otherID := channel.OtherParticipant(userID)
	relationships, err := s.checkRelationships(ctx, userID, otherID, true)
	if err != nil {
		return err
	}
	if relationships[userID] == relationshipTypeBlocked || relationships[otherID] == relationshipTypeBlocked {
		return permissionDenied()
	}
	return nil
}

// checkRelationships returns the relationship type each side holds toward
// the other, keyed by the row owner's user ID. Missing rows are absent from
// the map.
func (s *messageServer) checkRelationships(ctx context.Context, userID, targetID int64, includeReverse bool) (map[int64]int32, error) {
	req := new(userv1.CheckRelationshipsRequest)
	req.SetUserId(userID)
	req.SetTargetIds([]int64{targetID})
	req.SetIncludeReverse(includeReverse)
	resp, err := s.svcCtx.UserClient.CheckRelationships(ctx, req)
	if err != nil {
		return nil, err
	}
	types := make(map[int64]int32, 2)
	for _, relationship := range resp.GetRelationships() {
		types[relationship.GetUserId()] = int32(relationship.GetType())
	}
	return types, nil
}

func orderedPair(a, b int64) (int64, int64) {
	if a < b {
		return a, b
	}
	return b, a
}

func dmChannelToProto(channel *model.DmChannel) *messagev1.DmChannel {
	if channel == nil {
		return nil
	}
	value := new(messagev1.DmChannel)
	value.SetId(channel.ID)
	value.SetUserLo(channel.UserLo)
	value.SetUserHi(channel.UserHi)
	value.SetCreatedAt(channel.CreatedAt)
	return value
}

type dmChannelCreatedPayload struct {
	ChannelID   string             `json:"channel_id"`
	UserID      string             `json:"user_id"`
	RecipientID string             `json:"recipient_id"`
	Recipient   userProfilePayload `json:"recipient"`
	CreatedAt   int64              `json:"created_at"`
}

// newDmChannelCreatedEvent builds one user-routed record. The Kafka key is the
// channel ID so both recipient records share the channel stream; Dispatcher
// still routes each record from the payload user ID.
func newDmChannelCreatedEvent(
	channel *model.DmChannel,
	recipientID int64,
	recipient *userv1.UserProfile,
	idempotencyKey int64,
	deliveryIndex int,
) (messageEvent, error) {
	payload := dmChannelCreatedPayload{
		ChannelID:   strconv.FormatInt(channel.ID, 10),
		UserID:      strconv.FormatInt(recipientID, 10),
		RecipientID: strconv.FormatInt(channel.OtherParticipant(recipientID), 10),
		Recipient:   userProfilePayloadFromProto(recipient),
		CreatedAt:   channel.CreatedAt,
	}
	return newUserRoutedEvent(
		EventTypeDmChannelCreated,
		eventoutbox.MessageStreamKey(channel.ID),
		[]byte(strconv.FormatInt(channel.ID, 10)),
		payload,
		idempotencyKey,
		deliveryIndex,
	)
}

func (s *messageServer) getDmParticipantProfiles(
	ctx context.Context,
	userIDs ...int64,
) (map[int64]*userv1.UserProfile, error) {
	req := new(userv1.BatchGetUserProfilesRequest)
	req.SetUserIds(userIDs)
	resp, err := s.svcCtx.UserClient.BatchGetUserProfiles(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "user service returned an invalid response")
	}
	expected := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		expected[userID] = struct{}{}
	}
	profiles := make(map[int64]*userv1.UserProfile, len(expected))
	for _, profile := range resp.GetProfiles() {
		if profile == nil {
			return nil, status.Error(codes.Internal, "user service returned an invalid profile")
		}
		userID := profile.GetUserId()
		if _, ok := expected[userID]; !ok || profiles[userID] != nil {
			return nil, status.Error(codes.Internal, "user service returned unexpected profiles")
		}
		profiles[userID] = profile
	}
	for userID := range expected {
		if profiles[userID] == nil {
			return nil, status.Error(codes.Internal, "user service did not return all profiles")
		}
	}
	return profiles, nil
}

func dmRequiresFriendship() error {
	return rpcerror.New(codes.PermissionDenied, rpcerror.MessageDomain, rpcerror.MessageDmRequiresFriendship, "direct messages require friendship")
}
