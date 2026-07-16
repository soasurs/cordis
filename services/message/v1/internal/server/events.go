package server

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

const (
	EventTypeMessageCreated = realtime.EventMessageCreated
	EventTypeMessageUpdated = realtime.EventMessageUpdated
	EventTypeMessageDeleted = realtime.EventMessageDeleted
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
	MessageID           string           `json:"id"`
	ChannelID           string           `json:"channel_id"`
	AuthorID            string           `json:"author_id"`
	Content             string           `json:"content"`
	Type                int32            `json:"type"`
	Flags               int32            `json:"flags"`
	ReferencedMessageID string           `json:"referenced_message_id,omitempty"`
	ReferencedChannelID string           `json:"referenced_channel_id,omitempty"`
	Attachments         []attachmentJSON `json:"attachments"`
	MentionUserIDs      []string         `json:"mention_user_ids"`
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
	MessageID string `json:"id"`
	ChannelID string `json:"channel_id"`
	Revision  int64  `json:"revision"`
	DeletedAt int64  `json:"deleted_at"`
}

func newMessageCreatedEvent(message *model.Message, mentionUserIDs []int64) (messageEvent, error) {
	return newEvent(EventTypeMessageCreated, message.ChannelID, messagePayloadFromModel(message, mentionUserIDs))
}

func newMessageUpdatedEvent(message *model.Message, mentionUserIDs []int64) (messageEvent, error) {
	return newEvent(EventTypeMessageUpdated, message.ChannelID, messagePayloadFromModel(message, mentionUserIDs))
}

func newMessageDeletedEvent(message *model.Message) (messageEvent, error) {
	return newEvent(EventTypeMessageDeleted, message.ChannelID, messageDeletedPayload{
		MessageID: strconv.FormatInt(message.ID, 10),
		ChannelID: strconv.FormatInt(message.ChannelID, 10),
		Revision:  message.Revision,
		DeletedAt: message.DeletedAt,
	})
}

func messagePayloadFromModel(message *model.Message, mentionUserIDs []int64) messagePayload {
	return messagePayload{
		MessageID:           strconv.FormatInt(message.ID, 10),
		ChannelID:           strconv.FormatInt(message.ChannelID, 10),
		AuthorID:            strconv.FormatInt(message.AuthorID, 10),
		Content:             message.Content,
		Type:                message.Type,
		Flags:               message.Flags,
		ReferencedMessageID: optionalIDString(message.ReferencedMessageID),
		ReferencedChannelID: optionalIDString(message.ReferencedChannelID),
		Attachments:         attachmentsForEvent(message.Attachments),
		MentionUserIDs:      idStrings(mentionUserIDs),
		EditedAt:            message.EditedAt,
		CreatedAt:           message.CreatedAt,
		UpdatedAt:           message.UpdatedAt,
		Revision:            message.Revision,
	}
}

func optionalIDString(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func idStrings(ids []int64) []string {
	if ids == nil {
		return nil
	}
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = strconv.FormatInt(id, 10)
	}
	return values
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
