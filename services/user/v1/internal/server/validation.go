package server

import "strings"

const maxNameLength = 64

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
	if len(trimmed) > maxNameLength {
		return errNameTooLong
	}
	return nil
}
