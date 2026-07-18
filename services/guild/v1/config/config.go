package config

import (
	"time"

	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/kafka"
)

type Config struct {
	zrpc.RpcServerConf
	Database database.Config `json:",optional"`
	Kafka    KafkaConfig     `json:",optional"`
	Limits   ResourceLimitsConfig
	Services ServiceConfig
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

type ServiceConfig struct {
	User zrpc.RpcClientConf
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
