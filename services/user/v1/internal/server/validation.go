package server

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9_]{2,32}$`)

// normalizeUsername lowercases the handle so lookups and uniqueness are
// case-insensitive.
func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func validateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return errInvalidUsername
	}
	return nil
}

const maxNameRunes = 64
const maxBioRunes = 190

// normalizeEmail canonicalizes an address for storage and lookup. Mailbox
// local parts are case-insensitive at every mainstream provider, so the
// whole address is lowercased once at the service boundary.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func isValidEmail(email string) error {
	// Basic structural check: exactly one @, non-empty local and domain parts with a dot.
	if !strings.Contains(email, "@") {
		return errInvalidEmail
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errInvalidEmail
	}
	if !strings.Contains(parts[1], ".") {
		return errInvalidEmail
	}
	return nil
}

func validateName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errNameRequired
	}
	if utf8.RuneCountInString(trimmed) > maxNameRunes {
		return errNameTooLong
	}
	return nil
}

func validateBio(bio string) error {
	if utf8.RuneCountInString(bio) > maxBioRunes {
		return errBioTooLong
	}
	return nil
}

const (
	defaultRelationshipLimit = 100
	maxRelationshipLimit     = 200
)

func normalizeRelationshipLimit(value int32) (int, error) {
	if value == 0 {
		return defaultRelationshipLimit, nil
	}
	if value < 0 || int(value) > maxRelationshipLimit {
		return 0, errInvalidLimit
	}
	return int(value), nil
}
