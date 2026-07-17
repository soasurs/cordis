package token

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// GenerateOpaqueToken returns a URL-safe single-use secret with 256 bits of
// entropy. Callers persist only its Hash.
func GenerateOpaqueToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate opaque token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
