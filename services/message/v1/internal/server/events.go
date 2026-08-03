package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/message/v1/internal/eventoutbox"
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
	Type           string `json:"t"`
	Data           T      `json:"d"`
	IdempotencyKey string `json:"idempotency_key"`
	StreamSequence int64  `json:"stream_sequence,omitempty"`
	DeliveryIndex  int    `json:"delivery_index,omitempty"`
}

type messageEvent struct {
	EventID       int64
	DeliveryIndex int
	StreamKey     string
	EventType     string
	Key           []byte
	Payload       []byte
}

type messagePayload struct {
	MessageID               string             `json:"id"`
	GuildID                 string             `json:"guild_id,omitempty"`
	ChannelID               string             `json:"channel_id"`
	UserID                  string             `json:"user_id,omitempty"`
	Author                  userProfilePayload `json:"author"`
	Content                 string             `json:"content"`
	Type                    int32              `json:"type"`
	Flags                   int32              `json:"flags"`
	ReferencedMessageID     string             `json:"referenced_message_id,omitempty"`
	ReferencedChannelID     string             `json:"referenced_channel_id,omitempty"`
	Attachments             []attachmentJSON   `json:"attachments"`
	MentionUserIDs          []string           `json:"mention_user_ids"`
	MentionRoleIDs          []string           `json:"mention_role_ids"`
	MentionEveryone         bool               `json:"mention_everyone"`
	RebuildMentions         bool               `json:"rebuild_mentions,omitempty"`
	PreviousMentionUserIDs  []string           `json:"previous_mention_user_ids,omitempty"`
	PreviousMentionRoleIDs  []string           `json:"previous_mention_role_ids,omitempty"`
	PreviousMentionEveryone *bool              `json:"previous_mention_everyone,omitempty"`
	EditedAt                int64              `json:"edited_at"`
	CreatedAt               int64              `json:"created_at"`
	UpdatedAt               int64              `json:"updated_at"`
	Revision                int64              `json:"revision"`
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

type attachmentJSON struct {
	AssetID      string `json:"asset_id"`
	Filename     string `json:"filename"`
	Size         int64  `json:"size"`
	ContentType  string `json:"content_type"`
	Width        int32  `json:"width"`
	Height       int32  `json:"height"`
	Blurhash     string `json:"blurhash,omitempty"`
	URL          string `json:"url"`
	URLExpiresAt int64  `json:"url_expires_at"`
}

type messageDeletedPayload struct {
	MessageID       string   `json:"id"`
	GuildID         string   `json:"guild_id,omitempty"`
	ChannelID       string   `json:"channel_id"`
	UserID          string   `json:"user_id,omitempty"`
	Revision        int64    `json:"revision"`
	DeletedAt       int64    `json:"deleted_at"`
	LastMessageID   string   `json:"last_message_id"`
	MentionUserIDs  []string `json:"mention_user_ids"`
	MentionRoleIDs  []string `json:"mention_role_ids"`
	MentionEveryone bool     `json:"mention_everyone"`
}

type messageReadUpdatedPayload struct {
	UserID            string `json:"user_id"`
	ChannelID         string `json:"channel_id"`
	LastMessageID     string `json:"last_message_id"`
	LastReadMessageID string `json:"last_read_message_id"`
	MentionCount      int32  `json:"mention_count"`
}

func newMessageCreatedEvents(message *model.Message, author *userv1.UserProfile, mentions model.MessageMentions, audience messageAudience, idempotencyKey int64) ([]messageEvent, error) {
	return newMessageEvents(EventTypeMessageCreated, message.ChannelID, audience, messagePayloadFromModel(message, author, mentions), idempotencyKey)
}

func newMessageUpdatedEvents(message *model.Message, author *userv1.UserProfile, mentions, previousMentions model.MessageMentions, rebuildMentions bool, audience messageAudience, idempotencyKey int64) ([]messageEvent, error) {
	payload := messagePayloadFromModel(message, author, mentions)
	payload.RebuildMentions = rebuildMentions
	if len(previousMentions.UserIDs) > 0 || len(previousMentions.RoleIDs) > 0 || previousMentions.Everyone {
		payload.PreviousMentionUserIDs = idStrings(previousMentions.UserIDs)
		payload.PreviousMentionRoleIDs = idStrings(previousMentions.RoleIDs)
		previousEveryone := previousMentions.Everyone
		payload.PreviousMentionEveryone = &previousEveryone
	}
	return newMessageEvents(EventTypeMessageUpdated, message.ChannelID, audience, payload, idempotencyKey)
}

func newMessageDeletedEvents(message *model.Message, lastMessageID int64, mentions model.MessageMentions, audience messageAudience, idempotencyKey int64) ([]messageEvent, error) {
	return newMessageDeletedRoutingEvents(EventTypeMessageDeleted, message.ChannelID, audience, messageDeletedPayload{
		MessageID:       strconv.FormatInt(message.ID, 10),
		ChannelID:       strconv.FormatInt(message.ChannelID, 10),
		Revision:        message.Revision,
		DeletedAt:       message.DeletedAt,
		LastMessageID:   strconv.FormatInt(lastMessageID, 10),
		MentionUserIDs:  idStrings(mentions.UserIDs),
		MentionRoleIDs:  idStrings(mentions.RoleIDs),
		MentionEveryone: mentions.Everyone,
	}, idempotencyKey)
}

func newMessageReadUpdatedEvent(state *model.ChannelReadState, idempotencyKey int64) (messageEvent, error) {
	return newUserRoutedEvent(
		EventTypeMessageReadUpdated,
		eventoutbox.ReadStateStreamKey(state.UserID, state.ChannelID),
		eventoutbox.ReadStateKafkaKey(state.UserID, state.ChannelID),
		messageReadUpdatedPayload{
			UserID:            strconv.FormatInt(state.UserID, 10),
			ChannelID:         strconv.FormatInt(state.ChannelID, 10),
			LastMessageID:     strconv.FormatInt(state.LastMessageID, 10),
			LastReadMessageID: strconv.FormatInt(state.LastReadMessageID, 10),
			MentionCount:      state.MentionCount,
		},
		idempotencyKey,
		0,
	)
}

func newMessageEvents(eventType string, channelID int64, audience messageAudience, data messagePayload, idempotencyKey int64) ([]messageEvent, error) {
	if audience.guildID > 0 {
		data.GuildID = strconv.FormatInt(audience.guildID, 10)
		event, err := newEvent(eventType, channelID, data, idempotencyKey, 0)
		return singleEvent(event, err)
	}
	if err := validateDmAudience(audience.userIDs); err != nil {
		return nil, err
	}
	events := make([]messageEvent, 0, len(audience.userIDs))
	for index, userID := range audience.userIDs {
		data.UserID = strconv.FormatInt(userID, 10)
		event, err := newEvent(eventType, channelID, data, idempotencyKey, index)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func newMessageDeletedRoutingEvents(eventType string, channelID int64, audience messageAudience, data messageDeletedPayload, idempotencyKey int64) ([]messageEvent, error) {
	if audience.guildID > 0 {
		data.GuildID = strconv.FormatInt(audience.guildID, 10)
		event, err := newEvent(eventType, channelID, data, idempotencyKey, 0)
		return singleEvent(event, err)
	}
	if err := validateDmAudience(audience.userIDs); err != nil {
		return nil, err
	}
	events := make([]messageEvent, 0, len(audience.userIDs))
	for index, userID := range audience.userIDs {
		data.UserID = strconv.FormatInt(userID, 10)
		event, err := newEvent(eventType, channelID, data, idempotencyKey, index)
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

func messagePayloadFromModel(message *model.Message, author *userv1.UserProfile, mentions model.MessageMentions) messagePayload {
	return messagePayload{
		MessageID:           strconv.FormatInt(message.ID, 10),
		ChannelID:           strconv.FormatInt(message.ChannelID, 10),
		Author:              userProfilePayloadFromProto(author),
		Content:             message.Content,
		Type:                message.Type,
		Flags:               message.Flags,
		ReferencedMessageID: optionalIDString(message.ReferencedMessageID),
		ReferencedChannelID: optionalIDString(message.ReferencedChannelID),
		Attachments:         attachmentsForEvent(message.Attachments),
		MentionUserIDs:      idStrings(mentions.UserIDs),
		MentionRoleIDs:      idStrings(mentions.RoleIDs),
		MentionEveryone:     mentions.Everyone,
		EditedAt:            message.EditedAt,
		CreatedAt:           message.CreatedAt,
		UpdatedAt:           message.UpdatedAt,
		Revision:            message.Revision,
	}
}

func userProfilePayloadFromProto(profile *userv1.UserProfile) userProfilePayload {
	if profile == nil {
		return userProfilePayload{}
	}
	return userProfilePayload{
		UserID:        strconv.FormatInt(profile.GetUserId(), 10),
		Name:          profile.GetName(),
		Bio:           profile.GetBio(),
		AvatarAssetID: strconv.FormatInt(profile.GetAvatarAssetId(), 10),
		CreatedAt:     profile.GetCreatedAt(),
		UpdatedAt:     profile.GetUpdatedAt(),
		Username:      profile.GetUsername(),
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

// newUserRoutedEvent builds a user-routed record. The Kafka key and stream key
// are supplied by the caller; Dispatcher routing continues to use the payload.
func newUserRoutedEvent[T any](eventType, streamKey string, kafkaKey []byte, data T, idempotencyKey int64, deliveryIndex int) (messageEvent, error) {
	payload, err := json.Marshal(eventEnvelope[T]{
		Type:           eventType,
		Data:           data,
		IdempotencyKey: strconv.FormatInt(idempotencyKey, 10),
	})
	if err != nil {
		return messageEvent{}, fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	return messageEvent{
		EventID:       idempotencyKey,
		DeliveryIndex: deliveryIndex,
		StreamKey:     streamKey,
		EventType:     eventType,
		Key:           kafkaKey,
		Payload:       payload,
	}, nil
}

func newEvent[T any](eventType string, channelID int64, data T, idempotencyKey int64, deliveryIndex int) (messageEvent, error) {
	payload, err := json.Marshal(eventEnvelope[T]{
		Type:           eventType,
		Data:           data,
		IdempotencyKey: strconv.FormatInt(idempotencyKey, 10),
	})
	if err != nil {
		return messageEvent{}, fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	return messageEvent{
		EventID:       idempotencyKey,
		DeliveryIndex: deliveryIndex,
		StreamKey:     eventoutbox.MessageStreamKey(channelID),
		EventType:     eventType,
		Key:           fmt.Appendf(nil, "%d", channelID),
		Payload:       payload,
	}, nil
}

// finalizeEvent injects the assigned stream sequence and delivery index into
// a draft payload. encoding/json sorts map keys, so the result is
// deterministic.
func finalizeEvent(payload []byte, streamSequence int64, deliveryIndex int) ([]byte, error) {
	var envelope map[string]any
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, err
	}
	envelope["stream_sequence"] = streamSequence
	envelope["delivery_index"] = deliveryIndex
	return json.Marshal(envelope)
}

func attachmentsForEvent(attachments []model.Attachment) []attachmentJSON {
	values := make([]attachmentJSON, 0, len(attachments))
	for _, attachment := range attachments {
		values = append(values, attachmentJSON{
			AssetID:      strconv.FormatInt(attachment.AssetID, 10),
			Filename:     attachment.Filename,
			Size:         attachment.Size,
			ContentType:  attachment.ContentType,
			Width:        attachment.Width,
			Height:       attachment.Height,
			Blurhash:     attachment.Blurhash,
			URL:          attachment.URL,
			URLExpiresAt: attachment.URLExpiresAt,
		})
	}
	return values
}
