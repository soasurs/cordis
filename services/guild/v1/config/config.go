package config

import (
	"time"

	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/kafka"
)

type Config struct {
	zrpc.RpcServerConf
	Database    database.Config `json:",optional"`
	Kafka       KafkaConfig     `json:",optional"`
	Cursor      CursorConfig
	Limits      ResourceLimitsConfig
	Idempotency IdempotencyConfig
	Services    ServiceConfig
}

// CursorConfig holds the HMAC secret for opaque list continuation tokens.
type CursorConfig struct {
	Secret string
}

// ResourceLimitsConfig controls persistent Guild resource quotas.
type ResourceLimitsConfig struct {
	OwnedGuildsPerUser    int `json:",default=10"`
	JoinedGuildsPerUser   int `json:",default=100"`
	RolesPerGuild         int `json:",default=250"`
	ChannelsPerGuild      int `json:",default=500"`
	ActiveInvitesPerGuild int `json:",default=100"`
	OverwritesPerChannel  int `json:",default=100"`
}

// OwnedGuilds returns the active ownership limit per user.
func (c ResourceLimitsConfig) OwnedGuilds() int {
	if c.OwnedGuildsPerUser <= 0 {
		return 10
	}
	return c.OwnedGuildsPerUser
}

// JoinedGuilds returns the active membership limit per user.
func (c ResourceLimitsConfig) JoinedGuilds() int {
	if c.JoinedGuildsPerUser <= 0 {
		return 100
	}
	return c.JoinedGuildsPerUser
}

// Roles returns the active role limit per guild, including the default role.
func (c ResourceLimitsConfig) Roles() int {
	if c.RolesPerGuild <= 0 {
		return 250
	}
	return c.RolesPerGuild
}

// Channels returns the active channel limit per guild.
func (c ResourceLimitsConfig) Channels() int {
	if c.ChannelsPerGuild <= 0 {
		return 500
	}
	return c.ChannelsPerGuild
}

// ActiveInvites returns the usable invite limit per guild.
func (c ResourceLimitsConfig) ActiveInvites() int {
	if c.ActiveInvitesPerGuild <= 0 {
		return 100
	}
	return c.ActiveInvitesPerGuild
}

// Overwrites returns the permission overwrite limit per channel.
func (c ResourceLimitsConfig) Overwrites() int {
	if c.OverwritesPerChannel <= 0 {
		return 100
	}
	return c.OverwritesPerChannel
}

// IdempotencyConfig controls request-level idempotency retention and key
// validation for resource creation RPCs. Each operation has its own retention
// period so that high-cost creations can outlive short-lived ones.
type IdempotencyConfig struct {
	KeyMaxLength                 int `json:",default=255"`
	CreateGuildTTLSeconds        int `json:",default=86400"`
	CreateGuildRoleTTLSeconds    int `json:",default=86400"`
	CreateGuildChannelTTLSeconds int `json:",default=86400"`
	CreateGuildInviteTTLSeconds  int `json:",default=86400"`
}

// KeyLength returns the maximum accepted idempotency key length in bytes.
func (c IdempotencyConfig) KeyLength() int {
	if c.KeyMaxLength <= 0 {
		return 255
	}
	return c.KeyMaxLength
}

// CreateGuildTTL returns the retention period for CreateGuild idempotency keys.
func (c IdempotencyConfig) CreateGuildTTL() time.Duration {
	return idempotencyTTL(c.CreateGuildTTLSeconds, 24*time.Hour)
}

// CreateGuildRoleTTL returns the retention period for CreateGuildRole
// idempotency keys.
func (c IdempotencyConfig) CreateGuildRoleTTL() time.Duration {
	return idempotencyTTL(c.CreateGuildRoleTTLSeconds, 24*time.Hour)
}

// CreateGuildChannelTTL returns the retention period for CreateGuildChannel
// idempotency keys.
func (c IdempotencyConfig) CreateGuildChannelTTL() time.Duration {
	return idempotencyTTL(c.CreateGuildChannelTTLSeconds, 24*time.Hour)
}

// CreateGuildInviteTTL returns the retention period for CreateGuildInvite
// idempotency keys.
func (c IdempotencyConfig) CreateGuildInviteTTL() time.Duration {
	return idempotencyTTL(c.CreateGuildInviteTTLSeconds, 24*time.Hour)
}

func idempotencyTTL(seconds int, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

type ServiceConfig struct {
	User  zrpc.RpcClientConf
	Media zrpc.RpcClientConf
}

type KafkaConfig struct {
	Seeds            []string
	Topic            string `json:",default=cordis.guild.events.v1"`
	PublishTimeoutMs int    `json:",default=1000"`
}

func (c KafkaConfig) ProducerConfig() kafka.ProducerConfig {
	return kafka.ProducerConfig{
		Seeds:           c.Seeds,
		DeliveryTimeout: c.PublishTimeout(),
	}
}

func (c KafkaConfig) PublishTimeout() time.Duration {
	if c.PublishTimeoutMs <= 0 {
		return time.Second
	}
	return time.Duration(c.PublishTimeoutMs) * time.Millisecond
}
