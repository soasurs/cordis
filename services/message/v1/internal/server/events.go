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
)

// messageCreatedPayload is the JSON body for a message_created event.
type messageCreatedPayload struct {
	MessageID           int64              `json:"message_id"`
	ChannelID           int64              `json:"channel_id"`
	AuthorID            int64              `json:"author_id"`
	Content             string             `json:"content"`
	Type                int32              `json:"type"`
	Flags               int32              `json:"flags"`
	ReferencedMessageID int64              `json:"referenced_message_id,omitempty"`
	ReferencedChannelID int64              `json:"referenced_channel_id,omitempty"`
	Attachments         []attachmentJSON   `json:"attachments"`
	CreatedAt           int64              `json:"created_at"`
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
func newMessageCreatedEvent(topic string, message *model.Message) (outbox.Event, error) {
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
	data, err := json.Marshal(payload)
	if err != nil {
		return outbox.Event{}, fmt.Errorf("marshal message_created payload: %w", err)
	}
	return outbox.Event{
		ID:         message.ID, // reuse message ID as event ID for correlation
		Topic:      topic,
		Key:        []byte(fmt.Sprintf("%d", message.ChannelID)),
		Payload:    data,
		RetryCount: 0,
		MaxRetries: 5,
		LockedAt:   0,
		CreatedAt:  outbox.Now(),
	}, nil
}

// newMessageUpdatedEvent builds an outbox event for an updated message.
func newMessageUpdatedEvent(topic string, message *model.Message) (outbox.Event, error) {
	payload := messageUpdatedPayload{
		MessageID:   message.ID,
		ChannelID:   message.ChannelID,
		AuthorID:    message.AuthorID,
		Content:     message.Content,
		Flags:       message.Flags,
		Attachments: toAttachmentJSON(message.Attachments),
		EditedAt:    message.EditedAt,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return outbox.Event{}, fmt.Errorf("marshal message_updated payload: %w", err)
	}
	return outbox.Event{
		ID:         outbox.Now(), // separate ID from message ID for updates
		Topic:      topic,
		Key:        []byte(fmt.Sprintf("%d", message.ChannelID)),
		Payload:    data,
		RetryCount: 0,
		MaxRetries: 5,
		LockedAt:   0,
		CreatedAt:  outbox.Now(),
	}, nil
}

// newMessageDeletedEvent builds an outbox event for a deleted message.
func newMessageDeletedEvent(topic string, messageID, channelID int64) (outbox.Event, error) {
	payload := messageDeletedPayload{
		MessageID: messageID,
		ChannelID: channelID,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return outbox.Event{}, fmt.Errorf("marshal message_deleted payload: %w", err)
	}
	return outbox.Event{
		ID:         outbox.Now(),
		Topic:      topic,
		Key:        []byte(fmt.Sprintf("%d", channelID)),
		Payload:    data,
		RetryCount: 0,
		MaxRetries: 5,
		LockedAt:   0,
		CreatedAt:  outbox.Now(),
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
