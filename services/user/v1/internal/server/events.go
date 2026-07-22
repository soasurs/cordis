package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

const (
	EventTypeRelationshipUpdated = realtime.EventRelationshipUpdated
	EventTypeRelationshipRemoved = realtime.EventRelationshipRemoved
)

type eventEnvelope[T any] struct {
	Type           string `json:"t"`
	Data           T      `json:"d"`
	IdempotencyKey string `json:"idempotency_key"`
}

type userEvent struct {
	Key     []byte
	Payload []byte
}

type relationshipPayload struct {
	UserID    string `json:"user_id"`
	TargetID  string `json:"target_id"`
	Type      int16  `json:"type"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type relationshipRemovedPayload struct {
	UserID   string `json:"user_id"`
	TargetID string `json:"target_id"`
}

func newRelationshipUpdatedEvent(relationship *model.Relationship, idempotencyKey int64) (userEvent, error) {
	return newUserEvent(EventTypeRelationshipUpdated, relationship.UserID, relationshipPayload{
		UserID:    strconv.FormatInt(relationship.UserID, 10),
		TargetID:  strconv.FormatInt(relationship.TargetID, 10),
		Type:      relationship.Type,
		CreatedAt: relationship.CreatedAt,
		UpdatedAt: relationship.UpdatedAt,
	}, idempotencyKey)
}

func newRelationshipRemovedEvent(userID, targetID int64, idempotencyKey int64) (userEvent, error) {
	return newUserEvent(EventTypeRelationshipRemoved, userID, relationshipRemovedPayload{
		UserID:   strconv.FormatInt(userID, 10),
		TargetID: strconv.FormatInt(targetID, 10),
	}, idempotencyKey)
}

func newUserEvent[T any](eventType string, recipientID int64, data T, idempotencyKey int64) (userEvent, error) {
	payload, err := json.Marshal(eventEnvelope[T]{Type: eventType, Data: data, IdempotencyKey: strconv.FormatInt(idempotencyKey, 10)})
	if err != nil {
		return userEvent{}, fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	return userEvent{
		Key:     strconv.AppendInt(nil, recipientID, 10),
		Payload: payload,
	}, nil
}

func (s *userServer) publishEvents(ctx context.Context, events ...userEvent) {
	if s.svcCtx.Publisher == nil {
		return
	}
	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.svcCtx.Cfg.Kafka.PublishTimeout())
	defer cancel()
	for _, event := range events {
		if err := s.svcCtx.Publisher.Publish(publishCtx, event.Key, event.Payload); err != nil {
			logx.WithContext(ctx).Errorw(
				"publish user event",
				logx.Field("key", string(event.Key)),
				logx.Field("error", err),
			)
		}
	}
}
