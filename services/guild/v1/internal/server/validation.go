package server

import (
	"strings"
	"unicode/utf8"
)

const (
	defaultGuildLimit = 50
	maxGuildLimit     = 100
	maxGuildNameRunes = 100
	maxIconURILength  = 2048
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

func validateIconURI(iconURI string) error {
	if len(iconURI) > maxIconURILength {
		return invalidRequest("guild icon uri is too long")
	}
	return nil
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
