package server

import "strings"

func isValidEmail(email string) bool {
	if !strings.Contains(email, "@") {
		return false
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	return strings.Contains(parts[1], ".")
}
