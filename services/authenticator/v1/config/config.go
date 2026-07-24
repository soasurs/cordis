package config

import (
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/database"
)

type Config struct {
	zrpc.RpcServerConf
	Database     database.Config
	Tokens       TokenConfig
	Sessions     SessionConfig
	Password     PasswordConfig
	Registration RegistrationConfig
	TwoFactor    TwoFactorConfig
	Recovery     RecoveryConfig
	Services     ServiceConfig
}

const (
	// RegistrationModeOpen allows registrations without an invitation.
	RegistrationModeOpen = "open"
	// RegistrationModeInviteOnly requires a valid one-time invitation.
	RegistrationModeInviteOnly = "invite_only"
	// RegistrationModeClosed rejects all registrations.
	RegistrationModeClosed = "closed"
)

const defaultRegistrationReservationTTL = 15 * time.Minute

// RegistrationConfig controls whether new accounts may be created.
type RegistrationConfig struct {
	Mode           string        `json:",default=open"`
	ReservationTTL time.Duration `json:",default=15m"`
}

// EffectiveMode returns the configured mode, defaulting to open for
// programmatically constructed configurations.
func (c RegistrationConfig) EffectiveMode() string {
	if c.Mode == "" {
		return RegistrationModeOpen
	}
	return c.Mode
}

// EffectiveReservationTTL returns the configured invitation reservation
// lifetime, defaulting to fifteen minutes for programmatic configurations.
func (c RegistrationConfig) EffectiveReservationTTL() time.Duration {
	if c.ReservationTTL == 0 {
		return defaultRegistrationReservationTTL
	}
	return c.ReservationTTL
}

// PasswordConfig controls process-local protection for Argon2 work.
type PasswordConfig struct {
	MaxConcurrency int64 `json:",default=4"`
}

// RecoveryConfig bounds the lifetime of account recovery tokens and the
// re-request rate per target.
type RecoveryConfig struct {
	PasswordResetTTL     time.Duration `json:",default=30m"`
	EmailVerificationTTL time.Duration `json:",default=24h"`
	// RequestIntervalSeconds is the minimum delay between two recovery mails
	// for the same target. It also bounds how often an attacker can void a
	// victim's pending token, because a new request replaces the old one.
	RequestIntervalSeconds int `json:",default=60"`
	// Redis is optional; when unset, recovery request throttling is skipped.
	Redis redis.RedisConf `json:",optional"`
}

type TokenConfig struct {
	Issuer  string `json:",default=cordis.authenticator.v1"`
	Access  TokenKindConfig
	Refresh TokenKindConfig
}

type TokenKindConfig struct {
	Secret string
	TTL    time.Duration
}

type SessionConfig struct {
	TTL time.Duration
}

type TwoFactorConfig struct {
	Issuer            string
	EnrollmentTTL     time.Duration
	LoginChallengeTTL time.Duration
	MaxAttempts       int
	RecoveryCodeCount int
	Encryption        TwoFactorEncryptionConfig
}

type TwoFactorEncryptionConfig struct {
	PrimaryKeyID string
	Keys         []TwoFactorEncryptionKeyConfig
}

type TwoFactorEncryptionKeyConfig struct {
	ID     string
	Secret string
}

type ServiceConfig struct {
	User zrpc.RpcClientConf
	// Mailer is optional; when unset, recovery mail delivery is skipped.
	Mailer zrpc.RpcClientConf `json:",optional"`
}
