package server

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/user/v1/internal/eventoutbox"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

const (
	EventTypeRelationshipUpdated = realtime.EventRelationshipUpdated
	EventTypeRelationshipRemoved = realtime.EventRelationshipRemoved
	EventTypeUserProfileUpdated  = realtime.EventUserProfileUpdated
)

type eventEnvelope[T any] struct {
	Type           string `json:"t"`
	Data           T      `json:"d"`
	IdempotencyKey string `json:"idempotency_key"`
	StreamSequence int64  `json:"stream_sequence,omitempty"`
	DeliveryIndex  int    `json:"delivery_index,omitempty"`
}

type userEvent struct {
	EventID       int64
	DeliveryIndex int
	StreamKey     string
	EventType     string
	Key           []byte
	Payload       []byte
}

type relationshipPayload struct {
	UserID    string             `json:"user_id"`
	TargetID  string             `json:"target_id"`
	Profile   userProfilePayload `json:"profile"`
	Type      int16              `json:"type"`
	CreatedAt int64              `json:"created_at"`
	UpdatedAt int64              `json:"updated_at"`
}

type userProfilePayload struct {
	UserID        string `json:"user_id"`
	Name          string `json:"name"`
	Bio           string `json:"bio"`
	AvatarAssetID string `json:"avatar_asset_id"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	Username      string `json:"username"`
}

type relationshipRemovedPayload struct {
	UserID   string `json:"user_id"`
	TargetID string `json:"target_id"`
}

func newRelationshipUpdatedEvent(
	relationship *model.Relationship,
	profile *model.UserProfile,
	idempotencyKey int64,
	deliveryIndex int,
) (userEvent, error) {
	return newUserEvent(EventTypeRelationshipUpdated, relationship.UserID, relationshipPayload{
		UserID:    strconv.FormatInt(relationship.UserID, 10),
		TargetID:  strconv.FormatInt(relationship.TargetID, 10),
		Profile:   userProfilePayloadFromModel(profile),
		Type:      relationship.Type,
		CreatedAt: relationship.CreatedAt,
		UpdatedAt: relationship.UpdatedAt,
	}, idempotencyKey, deliveryIndex)
}

func userProfilePayloadFromModel(profile *model.UserProfile) userProfilePayload {
	if profile == nil {
		return userProfilePayload{}
	}
	return userProfilePayload{
		UserID:        strconv.FormatInt(profile.UserID, 10),
		Name:          profile.Name,
		Bio:           profile.Bio,
		AvatarAssetID: strconv.FormatInt(profile.AvatarAssetID, 10),
		CreatedAt:     profile.CreatedAt,
		UpdatedAt:     profile.UpdatedAt,
		Username:      profile.Username,
	}
}

func newRelationshipRemovedEvent(userID, targetID int64, idempotencyKey int64, deliveryIndex int) (userEvent, error) {
	return newUserEvent(EventTypeRelationshipRemoved, userID, relationshipRemovedPayload{
		UserID:   strconv.FormatInt(userID, 10),
		TargetID: strconv.FormatInt(targetID, 10),
	}, idempotencyKey, deliveryIndex)
}

func newUserProfileUpdatedEvent(profile *model.UserProfile, idempotencyKey int64) (userEvent, error) {
	return newUserEvent(
		EventTypeUserProfileUpdated,
		profile.UserID,
		userProfilePayloadFromModel(profile),
		idempotencyKey,
		0,
	)
}

func newUserEvent[T any](eventType string, recipientID int64, data T, idempotencyKey int64, deliveryIndex int) (userEvent, error) {
	payload, err := json.Marshal(eventEnvelope[T]{Type: eventType, Data: data, IdempotencyKey: strconv.FormatInt(idempotencyKey, 10)})
	if err != nil {
		return userEvent{}, fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	return userEvent{
		EventID:       idempotencyKey,
		DeliveryIndex: deliveryIndex,
		StreamKey:     eventoutbox.StreamKey(recipientID),
		EventType:     eventType,
		Key:           eventoutbox.KafkaKey(recipientID),
		Payload:       payload,
	}, nil
}

// finalizeEvent injects the assigned stream sequence and delivery index into
// a draft payload while preserving the original Data bytes exactly.
func finalizeEvent(payload []byte, streamSequence int64, deliveryIndex int) ([]byte, error) {
	var envelope eventEnvelope[json.RawMessage]
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, err
	}
	envelope.StreamSequence = streamSequence
	envelope.DeliveryIndex = deliveryIndex
	return json.Marshal(envelope)
}
