package server

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

const (
	EventTypeGuildCreated = "guild.created"
	EventTypeGuildUpdated = "guild.updated"
	EventTypeGuildDeleted = "guild.deleted"
)

type eventEnvelope[T any] struct {
	Type string `json:"t"`
	Data T      `json:"d"`
}

type guildEvent struct {
	Key     []byte
	Payload []byte
}

type guildPayload struct {
	ID        string `json:"id"`
	OwnerID   string `json:"owner_id"`
	Name      string `json:"name"`
	IconURI   string `json:"icon_uri"`
	Revision  int64  `json:"revision"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type guildDeletedPayload struct {
	ID        string `json:"id"`
	Revision  int64  `json:"revision"`
	DeletedAt int64  `json:"deleted_at"`
}

func newGuildCreatedEvent(guild *model.Guild) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildCreated, guild.ID, guildPayloadFromModel(guild))
}

func newGuildUpdatedEvent(guild *model.Guild) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildUpdated, guild.ID, guildPayloadFromModel(guild))
}

func newGuildDeletedEvent(guild *model.Guild) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildDeleted, guild.ID, guildDeletedPayload{
		ID:        strconv.FormatInt(guild.ID, 10),
		Revision:  guild.Revision,
		DeletedAt: guild.DeletedAt,
	})
}

func guildPayloadFromModel(guild *model.Guild) guildPayload {
	return guildPayload{
		ID:        strconv.FormatInt(guild.ID, 10),
		OwnerID:   strconv.FormatInt(guild.OwnerID, 10),
		Name:      guild.Name,
		IconURI:   guild.IconURI,
		Revision:  guild.Revision,
		CreatedAt: guild.CreatedAt,
		UpdatedAt: guild.UpdatedAt,
	}
}

func newGuildEvent[T any](eventType string, guildID int64, data T) (guildEvent, error) {
	payload, err := json.Marshal(eventEnvelope[T]{Type: eventType, Data: data})
	if err != nil {
		return guildEvent{}, fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	return guildEvent{
		Key:     strconv.AppendInt(nil, guildID, 10),
		Payload: payload,
	}, nil
}
