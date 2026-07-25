// Package cursor encodes and decodes opaque list continuation tokens.
//
// Wire format is three '.'-separated segments: p.k.h
//   - p: base64url(JSON payload)
//   - k: cursor kind
//   - h: base64url(HMAC-SHA256(secret, p+"."+k))
package cursor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	// MinSecretLen is the minimum accepted HMAC secret length.
	MinSecretLen = 32

	KindUserGuilds       = "user_guilds"
	KindGuildMembers     = "guild_members"
	KindGuildBans        = "guild_bans"
	KindGuildInvites     = "guild_invites"
	KindGuildRoleMembers = "guild_role_members"
	KindDmChannels       = "dm_channels"
	KindRelationships    = "relationships"
)

// ErrInvalid is returned when a cursor string cannot be decoded or verified.
var ErrInvalid = errors.New("invalid cursor")

// Codec signs and verifies opaque continuation tokens.
type Codec struct {
	secret []byte
}

// NewCodec constructs a Codec. secret must be at least MinSecretLen bytes.
func NewCodec(secret string) (*Codec, error) {
	if len(secret) < MinSecretLen {
		return nil, fmt.Errorf("cursor secret must be at least %d bytes", MinSecretLen)
	}
	return &Codec{secret: []byte(secret)}, nil
}

// Encode serializes payload as a signed p.k.h token for kind.
func (c *Codec) Encode(kind string, payload any) (string, error) {
	if c == nil {
		return "", errors.New("cursor codec is required")
	}
	if err := validateKind(kind); err != nil {
		return "", err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal cursor payload: %w", err)
	}
	p := base64.RawURLEncoding.EncodeToString(raw)
	h := c.mac(p, kind)
	return p + "." + kind + "." + h, nil
}

// Decode verifies a signed token for the expected kind and unmarshals the payload.
// An empty or unset cursor returns ok=false with a nil error.
func Decode[T any](c *Codec, kind, token string) (payload T, ok bool, err error) {
	var zero T
	if token == "" {
		return zero, false, nil
	}
	if c == nil {
		return zero, false, errors.New("cursor codec is required")
	}
	if err := validateKind(kind); err != nil {
		return zero, false, err
	}
	p, k, h, err := split(token)
	if err != nil {
		return zero, false, err
	}
	if k != kind {
		return zero, false, ErrInvalid
	}
	if !hmac.Equal([]byte(h), []byte(c.mac(p, k))) {
		return zero, false, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return zero, false, ErrInvalid
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return zero, false, ErrInvalid
	}
	return payload, true, nil
}

// Trim returns the first limit items and whether a next page exists.
// Callers should query with LIMIT limit+1.
func Trim[T any](items []T, limit int) (page []T, hasMore bool) {
	if limit < 0 {
		limit = 0
	}
	if len(items) > limit {
		return items[:limit], true
	}
	return items, false
}

func (c *Codec) mac(p, kind string) string {
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write([]byte(p))
	_, _ = mac.Write([]byte{'.'})
	_, _ = mac.Write([]byte(kind))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func split(token string) (p, kind, h string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", ErrInvalid
	}
	return parts[0], parts[1], parts[2], nil
}

func validateKind(kind string) error {
	if kind == "" || strings.Contains(kind, ".") {
		return fmt.Errorf("%w: invalid kind", ErrInvalid)
	}
	return nil
}
