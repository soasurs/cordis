package server

import (
	"encoding/json"
	"fmt"

	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

// Event types published to Kafka.
const (
	EventTypeMessageCreated = "message_created"
	EventTypeMessageUpdated = "message_updated"
	EventTypeMessageDeleted = "message_deleted"
	eventSchemaVersion      = 1
)

type eventEnvelope[T any] struct {
	// EventID is both the idempotency key and the last-seen cursor for the
	// message aggregate.
	EventID       int64  `json:"event_id"`
	EventType     string `json:"event_type"`
	SchemaVersion int    `json:"schema_version"`
	OccurredAt    int64  `json:"occurred_at"`
	Data          T      `json:"data"`
}

// messageCreatedPayload is the JSON body for a message_created event.
type messageCreatedPayload struct {
	MessageID           int64            `json:"message_id"`
	ChannelID           int64            `json:"channel_id"`
	AuthorID            int64            `json:"author_id"`
	Content             string           `json:"content"`
	Type                int32            `json:"type"`
	Flags               int32            `json:"flags"`
	ReferencedMessageID int64            `json:"referenced_message_id,omitempty"`
	ReferencedChannelID int64            `json:"referenced_channel_id,omitempty"`
	Attachments         []attachmentJSON `json:"attachments"`
	CreatedAt           int64            `json:"created_at"`
}

type attachmentJSON struct {
	Key         string `json:"key"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Width       int32  `json:"width"`
	Height      int32  `json:"height"`
}

// messageUpdatedPayload is the JSON body for a message_updated event.
type messageUpdatedPayload struct {
	MessageID   int64            `json:"message_id"`
	ChannelID   int64            `json:"channel_id"`
	AuthorID    int64            `json:"author_id"`
	Content     string           `json:"content"`
	Flags       int32            `json:"flags"`
	Attachments []attachmentJSON `json:"attachments"`
	EditedAt    int64            `json:"edited_at"`
}

// messageDeletedPayload is the JSON body for a message_deleted event.
type messageDeletedPayload struct {
	MessageID int64 `json:"message_id"`
	ChannelID int64 `json:"channel_id"`
}

// newMessageCreatedEvent builds an outbox event for a newly created message.
func newMessageCreatedEvent(topic string, eventID int64, maxRetries int, message *model.Message) (outbox.Event, error) {
	payload := messageCreatedPayload{
		MessageID:           message.ID,
		ChannelID:           message.ChannelID,
		AuthorID:            message.AuthorID,
		Content:             message.Content,
		Type:                message.Type,
		Flags:               message.Flags,
		ReferencedMessageID: message.ReferencedMessageID,
		ReferencedChannelID: message.ReferencedChannelID,
		Attachments:         toAttachmentJSON(message.Attachments),
		CreatedAt:           message.CreatedAt,
	}
	return newEvent(topic, eventID, EventTypeMessageCreated, message.ChannelID, maxRetries, payload)
}

// newMessageUpdatedEvent builds an outbox event for an updated message.
func newMessageUpdatedEvent(topic string, eventID int64, maxRetries int, message *model.Message) (outbox.Event, error) {
	payload := messageUpdatedPayload{
		MessageID:   message.ID,
		ChannelID:   message.ChannelID,
		AuthorID:    message.AuthorID,
		Content:     message.Content,
		Flags:       message.Flags,
		Attachments: toAttachmentJSON(message.Attachments),
		EditedAt:    message.EditedAt,
	}
	return newEvent(topic, eventID, EventTypeMessageUpdated, message.ChannelID, maxRetries, payload)
}

// newMessageDeletedEvent builds an outbox event for a deleted message.
func newMessageDeletedEvent(topic string, eventID, messageID, channelID int64, maxRetries int) (outbox.Event, error) {
	payload := messageDeletedPayload{
		MessageID: messageID,
		ChannelID: channelID,
	}
	return newEvent(topic, eventID, EventTypeMessageDeleted, channelID, maxRetries, payload)
}

func newEvent[T any](topic string, eventID int64, eventType string, channelID int64, maxRetries int, payload T) (outbox.Event, error) {
	occurredAt := outbox.Now()
	key := []byte(fmt.Sprintf("%d", channelID))
	data, err := json.Marshal(eventEnvelope[T]{
		EventID:       eventID,
		EventType:     eventType,
		SchemaVersion: eventSchemaVersion,
		OccurredAt:    occurredAt,
		Data:          payload,
	})
	if err != nil {
		return outbox.Event{}, fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	return outbox.Event{
		ID:          eventID,
		Topic:       topic,
		Key:         key,
		Partition:   outbox.PartitionForKey(key),
		Payload:     data,
		RetryCount:  0,
		MaxRetries:  maxRetries,
		AvailableAt: occurredAt,
		LockedAt:    0,
		CreatedAt:   occurredAt,
	}, nil
}

func toAttachmentJSON(attachments []model.Attachment) []attachmentJSON {
	values := make([]attachmentJSON, 0, len(attachments))
	for _, a := range attachments {
		values = append(values, attachmentJSON{
			Key:         a.Key,
			Filename:    a.Filename,
			Size:        a.Size,
			ContentType: a.ContentType,
			Width:       a.Width,
			Height:      a.Height,
		})
	}
	return values
}
