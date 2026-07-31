package server

import (
	"strings"
	"unicode/utf8"
)

const (
	defaultGuildLimit        = 50
	maxGuildLimit            = 100
	maxGuildNameRunes        = 100
	maxGuildDescriptionRunes = 1024
	maxRoleNameRunes         = 100
	maxNicknameRunes         = 32
)

func normalizeGuildName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalidRequest("guild name is required")
	}
	if utf8.RuneCountInString(name) > maxGuildNameRunes {
		return "", invalidRequest("guild name is too long")
	}
	return name, nil
}

func normalizeGuildDescription(description string) (string, error) {
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) > maxGuildDescriptionRunes {
		return "", invalidRequest("guild description is too long")
	}
	return description, nil
}

func normalizeRoleName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalidRequest("role name is required")
	}
	if utf8.RuneCountInString(name) > maxRoleNameRunes {
		return "", invalidRequest("role name is too long")
	}
	return name, nil
}

func normalizeNickname(nickname string) (string, error) {
	nickname = strings.TrimSpace(nickname)
	if utf8.RuneCountInString(nickname) > maxNicknameRunes {
		return "", invalidRequest("guild nickname is too long")
	}
	return nickname, nil
}

func validateIdempotencyKey(hasKey bool, key string, maxLength int) error {
	if !hasKey {
		return nil
	}
	switch {
	case key == "":
		return invalidRequest("idempotency key must not be empty")
	case len(key) > maxLength:
		return invalidRequest("idempotency key is too long")
	case strings.TrimSpace(key) != key:
		return invalidRequest("idempotency key must not have leading or trailing whitespace")
	default:
		return nil
	}
}

func normalizeLimit(value int32) (int, error) {
	if value == 0 {
		return defaultGuildLimit, nil
	}
	if value < 0 || int(value) > maxGuildLimit {
		return 0, invalidRequest("limit is out of range")
	}
	return int(value), nil
}

func normalizeMentionTargetsLimit(value int32) (int, error) {
	if value == 0 {
		return defaultMentionTargetsLimit, nil
	}
	if value < 0 || int(value) > maxMentionTargetsLimit {
		return 0, invalidRequest("limit is out of range")
	}
	return int(value), nil
}
