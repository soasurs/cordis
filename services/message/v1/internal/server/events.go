package server

import (
	"encoding/json"
	"fmt"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

const (
	EventTypeMessageCreated = "message.created"
	EventTypeMessageUpdated = "message.updated"
	EventTypeMessageDeleted = "message.deleted"
)

type eventEnvelope[T any] struct {
	Type string `json:"t"`
	Data T      `json:"d"`
}

type messageEvent struct {
	Key     []byte
	Payload []byte
}

type messagePayload struct {
	MessageID           int64            `json:"id"`
	ChannelID           int64            `json:"channel_id"`
	AuthorID            int64            `json:"author_id"`
	Content             string           `json:"content"`
	Type                int32            `json:"type"`
	Flags               int32            `json:"flags"`
	ReferencedMessageID int64            `json:"referenced_message_id,omitempty"`
	ReferencedChannelID int64            `json:"referenced_channel_id,omitempty"`
	Attachments         []attachmentJSON `json:"attachments"`
	MentionUserIDs      []int64          `json:"mention_user_ids"`
	EditedAt            int64            `json:"edited_at"`
	CreatedAt           int64            `json:"created_at"`
	UpdatedAt           int64            `json:"updated_at"`
	Revision            int64            `json:"revision"`
}

type attachmentJSON struct {
	Key         string `json:"key"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Width       int32  `json:"width"`
	Height      int32  `json:"height"`
}

type messageDeletedPayload struct {
	MessageID int64 `json:"id"`
	ChannelID int64 `json:"channel_id"`
	Revision  int64 `json:"revision"`
	DeletedAt int64 `json:"deleted_at"`
}

func newMessageCreatedEvent(message *model.Message, mentionUserIDs []int64) (messageEvent, error) {
	return newEvent(EventTypeMessageCreated, message.ChannelID, messagePayloadFromModel(message, mentionUserIDs))
}

func newMessageUpdatedEvent(message *model.Message, mentionUserIDs []int64) (messageEvent, error) {
	return newEvent(EventTypeMessageUpdated, message.ChannelID, messagePayloadFromModel(message, mentionUserIDs))
}

func newMessageDeletedEvent(message *model.Message) (messageEvent, error) {
	return newEvent(EventTypeMessageDeleted, message.ChannelID, messageDeletedPayload{
		MessageID: message.ID,
		ChannelID: message.ChannelID,
		Revision:  message.Revision,
		DeletedAt: message.DeletedAt,
	})
}

func messagePayloadFromModel(message *model.Message, mentionUserIDs []int64) messagePayload {
	return messagePayload{
		MessageID:           message.ID,
		ChannelID:           message.ChannelID,
		AuthorID:            message.AuthorID,
		Content:             message.Content,
		Type:                message.Type,
		Flags:               message.Flags,
		ReferencedMessageID: message.ReferencedMessageID,
		ReferencedChannelID: message.ReferencedChannelID,
		Attachments:         attachmentsForEvent(message.Attachments),
		MentionUserIDs:      mentionUserIDs,
		EditedAt:            message.EditedAt,
		CreatedAt:           message.CreatedAt,
		UpdatedAt:           message.UpdatedAt,
		Revision:            message.Revision,
	}
}

func newEvent[T any](eventType string, channelID int64, data T) (messageEvent, error) {
	payload, err := json.Marshal(eventEnvelope[T]{
		Type: eventType,
		Data: data,
	})
	if err != nil {
		return messageEvent{}, fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	return messageEvent{
		Key:     fmt.Appendf(nil, "%d", channelID),
		Payload: payload,
	}, nil
}

func attachmentsForEvent(attachments []model.Attachment) []attachmentJSON {
	values := make([]attachmentJSON, 0, len(attachments))
	for _, attachment := range attachments {
		values = append(values, attachmentJSON{
			Key:         attachment.Key,
			Filename:    attachment.Filename,
			Size:        attachment.Size,
			ContentType: attachment.ContentType,
			Width:       attachment.Width,
			Height:      attachment.Height,
		})
	}
	return values
}
