package server

import (
	"strings"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

const (
	defaultMessageLimit = 50
	maxMessageLimit     = 100
	maxContentLength    = 2000
)

func normalizeMessageType(messageType messagev1.MessageType) (messagev1.MessageType, error) {
	if messageType == messagev1.MessageType_MESSAGE_TYPE_UNSPECIFIED {
		return messagev1.MessageType_MESSAGE_TYPE_DEFAULT, nil
	}
	switch messageType {
	case messagev1.MessageType_MESSAGE_TYPE_DEFAULT,
		messagev1.MessageType_MESSAGE_TYPE_REPLY:
		return messageType, nil
	default:
		return messagev1.MessageType_MESSAGE_TYPE_UNSPECIFIED, invalidRequest("invalid message type")
	}
}

func validateFlags(flags int32) error {
	const allowedFlags = int32(messagev1.MessageFlag_MESSAGE_FLAG_SUPPRESS_NOTIFICATIONS)
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

func validateAttachments(attachments []model.Attachment, limit int) error {
	if len(attachments) > limit {
		return resourceLimitExceeded("attachment limit exceeded")
	}
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

func validateMentionUserIDs(userIDs []int64, limit int) error {
	if len(userIDs) > limit {
		return resourceLimitExceeded("mention limit exceeded")
	}
	seen := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			return invalidRequest("mention user id must be positive")
		}
		if _, ok := seen[userID]; ok {
			return invalidRequest("mention user ids must be unique")
		}
		seen[userID] = struct{}{}
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
