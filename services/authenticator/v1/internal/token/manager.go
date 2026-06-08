package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	AccessTokenType  = "access"
	RefreshTokenType = "refresh"
)

var ErrInvalidToken = errors.New("invalid token")

const minimumSecretLength = 32

type Manager struct {
	issuer        string
	accessSecret  []byte
	refreshSecret []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
}

type Config struct {
	Issuer        string
	AccessSecret  string
	RefreshSecret string
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
}

type Claims struct {
	TokenType string `json:"typ,omitempty"`
	SessionID string `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

type Token struct {
	Raw       string
	UserID    int64
	SessionID int64
	ExpiresAt int64
}

func NewManager(cfg Config) (*Manager, error) {
	if cfg.Issuer == "" {
		return nil, errors.New("token issuer is required")
	}
	if cfg.AccessSecret == "" {
		return nil, errors.New("access token secret is required")
	}
	if cfg.RefreshSecret == "" {
		return nil, errors.New("refresh token secret is required")
	}
	if len(cfg.AccessSecret) < minimumSecretLength {
		return nil, fmt.Errorf("access token secret must be at least %d bytes", minimumSecretLength)
	}
	if len(cfg.RefreshSecret) < minimumSecretLength {
		return nil, fmt.Errorf("refresh token secret must be at least %d bytes", minimumSecretLength)
	}
	if cfg.AccessSecret == cfg.RefreshSecret {
		return nil, errors.New("access and refresh token secrets must differ")
	}
	if cfg.AccessTTL <= 0 {
		return nil, errors.New("access token ttl must be positive")
	}
	if cfg.RefreshTTL <= 0 {
		return nil, errors.New("refresh token ttl must be positive")
	}

	return &Manager{
		issuer:        cfg.Issuer,
		accessSecret:  []byte(cfg.AccessSecret),
		refreshSecret: []byte(cfg.RefreshSecret),
		accessTTL:     cfg.AccessTTL,
		refreshTTL:    cfg.RefreshTTL,
	}, nil
}

func (m *Manager) IssueAccessToken(userID, sessionID int64, now time.Time) (Token, error) {
	expiresAt := now.Add(m.accessTTL)
	return m.issueToken(AccessTokenType, userID, sessionID, "", expiresAt, now, m.accessSecret)
}

func (m *Manager) IssueRefreshToken(userID, sessionID int64, sessionExpiresAt int64, now time.Time) (Token, error) {
	expiresAt := now.Add(m.refreshTTL)
	sessionExpires := time.UnixMilli(sessionExpiresAt)
	if expiresAt.After(sessionExpires) {
		expiresAt = sessionExpires
	}

	jti, err := randomID()
	if err != nil {
		return Token{}, err
	}

	return m.issueToken(RefreshTokenType, userID, sessionID, jti, expiresAt, now, m.refreshSecret)
}

func (m *Manager) ParseAccessToken(raw string) (Token, error) {
	return m.parseToken(raw, AccessTokenType, m.accessSecret)
}

func (m *Manager) ParseRefreshToken(raw string) (Token, error) {
	return m.parseToken(raw, RefreshTokenType, m.refreshSecret)
}

func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (m *Manager) issueToken(tokenType string, userID, sessionID int64, tokenID string, expiresAt, now time.Time, secret []byte) (Token, error) {
	issuedAt := jwt.NewNumericDate(now)
	expires := jwt.NewNumericDate(expiresAt)
	claims := Claims{
		TokenType: tokenType,
		SessionID: strconv.FormatInt(sessionID, 10),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   strconv.FormatInt(userID, 10),
			ID:        tokenID,
			IssuedAt:  issuedAt,
			ExpiresAt: expires,
		},
	}

	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		return Token{}, err
	}

	return Token{
		Raw:       raw,
		UserID:    userID,
		SessionID: sessionID,
		ExpiresAt: expires.Time.UnixMilli(),
	}, nil
}

func (m *Manager) parseToken(raw, tokenType string, secret []byte) (Token, error) {
	claims := new(Claims)
	parsed, err := jwt.ParseWithClaims(
		raw,
		claims,
		func(token *jwt.Token) (any, error) {
			return secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return Token{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if parsed == nil || !parsed.Valid || claims.TokenType != tokenType {
		return Token{}, ErrInvalidToken
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return Token{}, ErrInvalidToken
	}
	sessionID, err := strconv.ParseInt(claims.SessionID, 10, 64)
	if err != nil {
		return Token{}, ErrInvalidToken
	}
	if claims.ExpiresAt == nil {
		return Token{}, ErrInvalidToken
	}

	return Token{
		Raw:       raw,
		UserID:    userID,
		SessionID: sessionID,
		ExpiresAt: claims.ExpiresAt.Time.UnixMilli(),
	}, nil
}

func randomID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
