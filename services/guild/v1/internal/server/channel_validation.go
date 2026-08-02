package server

import (
	"context"
	"strings"
	"unicode/utf8"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

const (
	maxChannelNameRunes  = 100
	maxChannelTopicRunes = 1024
)

func validateChannelActorRequest(channelID, actorUserID int64) error {
	if channelID <= 0 {
		return invalidRequest("channel id is required")
	}
	if actorUserID <= 0 {
		return invalidRequest("actor user id is required")
	}
	return nil
}

func validateExpectedChannelLayoutRevision(present bool, revision int64) error {
	if !present || revision <= 0 {
		return invalidRequest("expected channel layout revision is required")
	}
	return nil
}

func requireGuildChannelLayoutRevision(
	ctx context.Context,
	guildStore store.Store,
	guildID, expectedRevision int64,
) error {
	currentRevision, err := guildStore.GetGuildChannelLayoutRevision(ctx, guildID)
	if err != nil {
		return err
	}
	if currentRevision != expectedRevision {
		return channelLayoutConflict()
	}
	return nil
}

func normalizeChannelName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalidRequest("channel name is required")
	}
	if utf8.RuneCountInString(name) > maxChannelNameRunes {
		return "", invalidRequest("channel name is too long")
	}
	return name, nil
}

func validateChannelTopic(topic string) error {
	if utf8.RuneCountInString(topic) > maxChannelTopicRunes {
		return invalidRequest("channel topic is too long")
	}
	return nil
}

func normalizeChannelType(value guildv1.GuildChannelType) (guildv1.GuildChannelType, error) {
	if value == guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_UNSPECIFIED {
		return guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT, nil
	}
	if value != guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT &&
		value != guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY &&
		value != guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_VOICE {
		return 0, invalidRequest("unsupported channel type")
	}
	return value, nil
}

func validateChannelParent(
	ctx context.Context,
	guildStore store.Store,
	guildID int64,
	channelType guildv1.GuildChannelType,
	parentID int64,
) error {
	if parentID == 0 {
		return nil
	}
	if parentID < 0 {
		return invalidRequest("parent id must not be negative")
	}
	if channelType == guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY {
		return invalidRequest("category channels cannot have a parent")
	}
	parent, err := guildStore.GetGuildChannel(ctx, parentID)
	if err != nil {
		return err
	}
	if parent.GuildID != guildID || parent.Type != int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY) {
		return invalidRequest("parent channel must be a category in the same guild")
	}
	return nil
}
