package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

const (
	EventTypeMessageCreated     = realtime.EventMessageCreated
	EventTypeMessageUpdated     = realtime.EventMessageUpdated
	EventTypeMessageDeleted     = realtime.EventMessageDeleted
	EventTypeMessageReadUpdated = realtime.EventMessageReadUpdated
	EventTypeDmChannelCreated   = realtime.EventDmChannelCreated
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
	MessageID              string           `json:"id"`
	GuildID                string           `json:"guild_id,omitempty"`
	ChannelID              string           `json:"channel_id"`
	UserID                 string           `json:"user_id,omitempty"`
	AuthorID               string           `json:"author_id"`
	Content                string           `json:"content"`
	Type                   int32            `json:"type"`
	Flags                  int32            `json:"flags"`
	ReferencedMessageID    string           `json:"referenced_message_id,omitempty"`
	ReferencedChannelID    string           `json:"referenced_channel_id,omitempty"`
	Attachments            []attachmentJSON `json:"attachments"`
	MentionUserIDs         []string         `json:"mention_user_ids"`
	PreviousMentionUserIDs []string         `json:"previous_mention_user_ids,omitempty"`
	EditedAt               int64            `json:"edited_at"`
	CreatedAt              int64            `json:"created_at"`
	UpdatedAt              int64            `json:"updated_at"`
	Revision               int64            `json:"revision"`
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
	MessageID      string   `json:"id"`
	GuildID        string   `json:"guild_id,omitempty"`
	ChannelID      string   `json:"channel_id"`
	UserID         string   `json:"user_id,omitempty"`
	Revision       int64    `json:"revision"`
	DeletedAt      int64    `json:"deleted_at"`
	LastMessageID  string   `json:"last_message_id"`
	MentionUserIDs []string `json:"mention_user_ids"`
}

type messageReadUpdatedPayload struct {
	UserID            string `json:"user_id"`
	ChannelID         string `json:"channel_id"`
	LastMessageID     string `json:"last_message_id"`
	LastReadMessageID string `json:"last_read_message_id"`
	MentionCount      int32  `json:"mention_count"`
}

func newMessageCreatedEvents(message *model.Message, mentionUserIDs []int64, audience messageAudience) ([]messageEvent, error) {
	return newMessageEvents(EventTypeMessageCreated, audience, messagePayloadFromModel(message, mentionUserIDs))
}

func newMessageUpdatedEvents(message *model.Message, mentionUserIDs, previousMentionUserIDs []int64, audience messageAudience) ([]messageEvent, error) {
	payload := messagePayloadFromModel(message, mentionUserIDs)
	payload.PreviousMentionUserIDs = idStrings(previousMentionUserIDs)
	return newMessageEvents(EventTypeMessageUpdated, audience, payload)
}

func newMessageDeletedEvents(message *model.Message, lastMessageID int64, mentionUserIDs []int64, audience messageAudience) ([]messageEvent, error) {
	return newMessageDeletedRoutingEvents(EventTypeMessageDeleted, audience, messageDeletedPayload{
		MessageID:      strconv.FormatInt(message.ID, 10),
		ChannelID:      strconv.FormatInt(message.ChannelID, 10),
		Revision:       message.Revision,
		DeletedAt:      message.DeletedAt,
		LastMessageID:  strconv.FormatInt(lastMessageID, 10),
		MentionUserIDs: idStrings(mentionUserIDs),
	})
}

func newMessageReadUpdatedEvent(state *model.ChannelReadState) (messageEvent, error) {
	return newUserRoutedEvent(EventTypeMessageReadUpdated, state.UserID, messageReadUpdatedPayload{
		UserID:            strconv.FormatInt(state.UserID, 10),
		ChannelID:         strconv.FormatInt(state.ChannelID, 10),
		LastMessageID:     strconv.FormatInt(state.LastMessageID, 10),
		LastReadMessageID: strconv.FormatInt(state.LastReadMessageID, 10),
		MentionCount:      state.MentionCount,
	})
}

func newMessageEvents(eventType string, audience messageAudience, data messagePayload) ([]messageEvent, error) {
	if audience.guildID > 0 {
		data.GuildID = strconv.FormatInt(audience.guildID, 10)
		event, err := newEvent(eventType, audience.guildID, data)
		return singleEvent(event, err)
	}
	if err := validateDmAudience(audience.userIDs); err != nil {
		return nil, err
	}
	events := make([]messageEvent, 0, len(audience.userIDs))
	for _, userID := range audience.userIDs {
		data.UserID = strconv.FormatInt(userID, 10)
		event, err := newUserRoutedEvent(eventType, userID, data)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func newMessageDeletedRoutingEvents(eventType string, audience messageAudience, data messageDeletedPayload) ([]messageEvent, error) {
	if audience.guildID > 0 {
		data.GuildID = strconv.FormatInt(audience.guildID, 10)
		event, err := newEvent(eventType, audience.guildID, data)
		return singleEvent(event, err)
	}
	if err := validateDmAudience(audience.userIDs); err != nil {
		return nil, err
	}
	events := make([]messageEvent, 0, len(audience.userIDs))
	for _, userID := range audience.userIDs {
		data.UserID = strconv.FormatInt(userID, 10)
		event, err := newUserRoutedEvent(eventType, userID, data)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func validateDmAudience(userIDs []int64) error {
	if len(userIDs) == 0 {
		return errors.New("dm message audience is empty")
	}
	for _, userID := range userIDs {
		if userID <= 0 {
			return errors.New("dm message audience contains invalid user id")
		}
	}
	return nil
}

func singleEvent(event messageEvent, err error) ([]messageEvent, error) {
	if err != nil {
		return nil, err
	}
	return []messageEvent{event}, nil
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

// newUserRoutedEvent keys the record by the decimal recipient user ID so
// the dispatcher fans it out through user routes instead of channel routes.
func newUserRoutedEvent[T any](eventType string, recipientID int64, data T) (messageEvent, error) {
	payload, err := json.Marshal(eventEnvelope[T]{
		Type: eventType,
		Data: data,
	})
	if err != nil {
		return messageEvent{}, fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	return messageEvent{
		Key:     fmt.Appendf(nil, "%d", recipientID),
		Payload: payload,
	}, nil
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
