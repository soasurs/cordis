package server

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
	"google.golang.org/grpc/codes"
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

	channel := &model.DmChannel{
		ID:        s.svcCtx.Snowflake.Generate().Int64(),
		UserLo:    userLo,
		UserHi:    userHi,
		CreatedAt: time.Now().UnixMilli(),
	}
	if err := s.svcCtx.Store.CreateDmChannel(ctx, channel); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Lost a concurrent open: the other insert won, reuse its row.
			channel, err = s.svcCtx.Store.GetDmChannelByPair(ctx, userLo, userHi)
			if err != nil {
				return nil, err
			}
			resp := new(messagev1.CreateDmChannelResponse)
			resp.SetChannel(dmChannelToProto(channel))
			return resp, nil
		}
		return nil, err
	}

	for _, recipientID := range []int64{channel.UserLo, channel.UserHi} {
		event, eventErr := newDmChannelCreatedEvent(channel, recipientID)
		s.publishEvent(ctx, event, eventErr)
	}

	resp := new(messagev1.CreateDmChannelResponse)
	resp.SetChannel(dmChannelToProto(channel))
	return resp, nil
}

func (s *messageServer) ListDmChannels(ctx context.Context, req *messagev1.ListDmChannelsRequest) (*messagev1.ListDmChannelsResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	if req.GetBeforeId() < 0 {
		return nil, invalidRequest("before id must not be negative")
	}
	limit, err := normalizeLimit(req.GetLimit(), defaultMessageLimit, maxMessageLimit)
	if err != nil {
		return nil, err
	}

	channels, err := s.svcCtx.Store.ListDmChannels(ctx, store.ListDmChannelsParams{
		UserID:   req.GetUserId(),
		BeforeID: req.GetBeforeId(),
		Limit:    limit,
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(messagev1.ListDmChannelsResponse)
	values := make([]*messagev1.DmChannel, 0, len(channels))
	for _, channel := range channels {
		values = append(values, dmChannelToProto(channel))
	}
	resp.SetChannels(values)
	if len(channels) > 0 {
		resp.SetBeforeId(channels[len(channels)-1].ID)
	}
	return resp, nil
}

func (s *messageServer) AuthorizeDmChannel(ctx context.Context, req *messagev1.AuthorizeDmChannelRequest) (*messagev1.AuthorizeDmChannelResponse, error) {
	if req.GetChannelId() <= 0 {
		return nil, invalidRequest("channel id is required")
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}

	channel, err := s.svcCtx.Store.GetDmChannel(ctx, req.GetChannelId())
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(messagev1.AuthorizeDmChannelResponse)
	resp.SetAllowed(channel.Participates(req.GetUserId()))
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
	ChannelID   string `json:"channel_id"`
	UserID      string `json:"user_id"`
	RecipientID string `json:"recipient_id"`
	CreatedAt   int64  `json:"created_at"`
}

// newDmChannelCreatedEvent builds one user-routed record; the key is the
// decimal recipient user ID so the dispatcher reaches their sessions even
// though nobody subscribes to the channel yet.
func newDmChannelCreatedEvent(channel *model.DmChannel, recipientID int64) (messageEvent, error) {
	payload := dmChannelCreatedPayload{
		ChannelID:   strconv.FormatInt(channel.ID, 10),
		UserID:      strconv.FormatInt(recipientID, 10),
		RecipientID: strconv.FormatInt(channel.OtherParticipant(recipientID), 10),
		CreatedAt:   channel.CreatedAt,
	}
	return newUserRoutedEvent(EventTypeDmChannelCreated, recipientID, payload)
}

func dmRequiresFriendship() error {
	return rpcerror.New(codes.PermissionDenied, rpcerror.MessageDomain, rpcerror.MessageDmRequiresFriendship, "direct messages require friendship")
}
