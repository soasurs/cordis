package server

import (
	"strings"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

const (
	defaultMessageLimit      = 50
	maxMessageLimit          = 100
	defaultReactionUserLimit = 100
	maxReactionUserLimit     = 500
	maxContentLength         = 2000
)

func normalizeMessageType(messageType messagev1.MessageType) (messagev1.MessageType, error) {
	if messageType == messagev1.MessageType_MESSAGE_TYPE_UNSPECIFIED {
		return messagev1.MessageType_MESSAGE_TYPE_DEFAULT, nil
	}
	switch messageType {
	case messagev1.MessageType_MESSAGE_TYPE_DEFAULT,
		messagev1.MessageType_MESSAGE_TYPE_REPLY,
		messagev1.MessageType_MESSAGE_TYPE_THREAD_STARTER:
		return messageType, nil
	default:
		return messagev1.MessageType_MESSAGE_TYPE_UNSPECIFIED, invalidRequest("invalid message type")
	}
}

func validateFlags(flags int32) error {
	const allowedFlags = int32(messagev1.MessageFlag_MESSAGE_FLAG_HAS_THREAD | messagev1.MessageFlag_MESSAGE_FLAG_SUPPRESS_NOTIFICATIONS)
	if flags < 0 || flags&^allowedFlags != 0 {
		return invalidRequest("invalid message flags")
	}
	return nil
}

func validateContent(content string) error {
	if len(content) > maxContentLength {
		return invalidRequest("content is too long")
	}
	return nil
}

func validateAttachments(attachments []model.Attachment) error {
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.Key) == "" {
			return invalidRequest("attachment key is required")
		}
		if strings.TrimSpace(attachment.Filename) == "" {
			return invalidRequest("attachment filename is required")
		}
		if attachment.Size < 0 || attachment.Width < 0 || attachment.Height < 0 {
			return invalidRequest("attachment dimensions or size are invalid")
		}
	}
	return nil
}

func validateMentionUserIDs(userIDs []int64) error {
	for _, userID := range userIDs {
		if userID <= 0 {
			return invalidRequest("mention user id must be positive")
		}
	}
	return nil
}

func normalizeLimit(value int32, defaultValue, maxValue int) (int, error) {
	if value == 0 {
		return defaultValue, nil
	}
	if value < 0 || int(value) > maxValue {
		return 0, invalidRequest("limit is out of range")
	}
	return int(value), nil
}

func validateEmoji(emojiID int64, emojiName string) error {
	if emojiID < 0 {
		return invalidRequest("emoji id must not be negative")
	}
	if strings.TrimSpace(emojiName) == "" {
		return invalidRequest("emoji name is required")
	}
	return nil
}
