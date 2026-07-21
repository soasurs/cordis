package config

import (
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/trace"
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/sessionregistry"
)

type Config struct {
	Name            string
	ListenOn        string
	Timeout         int64 `json:",default=0"`
	Log             logx.LogConf
	DevServer       service.DevServerConfig `json:",optional"`
	Telemetry       trace.Config            `json:",optional"`
	Middlewares     zrpc.ServerMiddlewaresConf
	Node            NodeConfig
	RateLimit       RateLimitConfig
	Redis           redis.RedisConf
	SessionRegistry sessionregistry.Config
	Services        ServiceConfig
}

type RateLimitConfig struct {
	KeyPrefix             string        `json:",default=cordis:session:rate_limit:"`
	FallbackMaxKeys       int           `json:",default=10000"`
	FallbackRetryInterval time.Duration `json:",default=1s"`
	Policies              map[string]RateLimitPolicy
}

type RateLimitPolicy struct {
	Limit  int64
	Window time.Duration
}

type NodeConfig struct {
	ID                      string
	AdvertiseAddress        string
	HeartbeatSeconds        int   `json:",default=15"`
	NodeTTLSeconds          int   `json:",default=30"`
	SessionResumeSeconds    int   `json:",default=120"`
	MaxReplayEvents         int   `json:",default=2048"`
	MaxPendingDispatches    int   `json:",default=1024"`
	MaxPendingDispatchBytes int64 `json:",default=16777216"`
	RouteRefreshSeconds     int   `json:",default=15"`
	RouteTTLSeconds         int   `json:",default=30"`
	BindingQueueSize        int   `json:",default=4096"`
	DrainSeconds            int   `json:",default=30"`
	MaxVisibilityGuilds     int   `json:",default=100"`
	MaxVisibilityChannels   int   `json:",default=500"`
	MaxSnapshotReloads      int   `json:",default=16"`
	SnapshotReloadSeconds   int   `json:",default=2"`
	MaxPresenceUpdates      int   `json:",default=5"`
	PresenceWindowSeconds   int   `json:",default=20"`
}

// PresenceUpdateLimit returns the per-logical-session presence update quota.
func (c NodeConfig) PresenceUpdateLimit() int {
	if c.MaxPresenceUpdates <= 0 {
		return 5
	}
	return c.MaxPresenceUpdates
}

// PresenceUpdateWindow returns the per-logical-session presence quota window.
func (c NodeConfig) PresenceUpdateWindow() time.Duration {
	if c.PresenceWindowSeconds <= 0 {
		return 20 * time.Second
	}
	return time.Duration(c.PresenceWindowSeconds) * time.Second
}

// VisibilityGuildLimit bounds the number of Guild snapshots loaded for one user.
func (c NodeConfig) VisibilityGuildLimit() int {
	if c.MaxVisibilityGuilds <= 0 {
		return 100
	}
	return c.MaxVisibilityGuilds
}

// VisibilityChannelLimit bounds the visible channel IDs retained per Guild snapshot.
func (c NodeConfig) VisibilityChannelLimit() int {
	if c.MaxVisibilityChannels <= 0 {
		return 500
	}
	return c.MaxVisibilityChannels
}

// SnapshotReloadLimit bounds concurrent Guild visibility reloads per Session node.
func (c NodeConfig) SnapshotReloadLimit() int64 {
	if c.MaxSnapshotReloads <= 0 {
		return 16
	}
	return int64(c.MaxSnapshotReloads)
}

// SnapshotReloadTimeout bounds one on-demand Guild visibility reload.
func (c NodeConfig) SnapshotReloadTimeout() time.Duration {
	if c.SnapshotReloadSeconds <= 0 {
		return 2 * time.Second
	}
	return time.Duration(c.SnapshotReloadSeconds) * time.Second
}

type ServiceConfig struct {
	Authenticator zrpc.RpcClientConf
	Presence      zrpc.RpcClientConf
	Guild         zrpc.RpcClientConf
	Message       zrpc.RpcClientConf
}

func (c Config) RPCConfig() zrpc.RpcServerConf {
	return zrpc.RpcServerConf{
		ServiceConf: service.ServiceConf{
			Name: c.Name, Log: c.Log, DevServer: c.DevServer, Telemetry: c.Telemetry,
		},
		ListenOn: c.ListenOn, Timeout: c.Timeout, Middlewares: c.Middlewares,
	}
}

func (c NodeConfig) HeartbeatInterval() time.Duration {
	if c.HeartbeatSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(c.HeartbeatSeconds) * time.Second
}

func (c NodeConfig) NodeTTL() time.Duration {
	if c.NodeTTLSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.NodeTTLSeconds) * time.Second
}

func (c NodeConfig) ResumeTTL() time.Duration {
	if c.SessionResumeSeconds <= 0 {
		return 2 * time.Minute
	}
	return time.Duration(c.SessionResumeSeconds) * time.Second
}

// SessionLeaseRefreshInterval returns the base interval for renewing logical
// Session owner and Presence leases. The runtime adds jitter to this value.
func (c NodeConfig) SessionLeaseRefreshInterval() time.Duration {
	return c.ResumeTTL() / 4
}

// SessionLeaseSpreadWindow bounds how long one maintenance cycle may spread
// its Redis pipelines and Presence RPCs.
func (c NodeConfig) SessionLeaseSpreadWindow() time.Duration {
	return min(c.SessionLeaseRefreshInterval()/4, 5*time.Second)
}

func (c NodeConfig) ReplayLimit() int {
	if c.MaxReplayEvents <= 0 {
		return 2048
	}
	return c.MaxReplayEvents
}

// PendingDispatchLimit bounds events buffered while READY is assembled. It
// leaves one replay and binding slot for READY itself.
func (c NodeConfig) PendingDispatchLimit() int {
	limit := c.MaxPendingDispatches
	if limit <= 0 {
		limit = 1024
	}
	return min(limit, max(0, c.ReplayLimit()-1), max(0, c.QueueSize()-1))
}

// PendingDispatchByteLimit bounds buffered event type and payload bytes during READY.
func (c NodeConfig) PendingDispatchByteLimit() int64 {
	if c.MaxPendingDispatchBytes <= 0 {
		return 16 << 20
	}
	return c.MaxPendingDispatchBytes
}

func (c NodeConfig) RouteRefreshInterval() time.Duration {
	if c.RouteRefreshSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(c.RouteRefreshSeconds) * time.Second
}

func (c NodeConfig) RouteTTL() time.Duration {
	if c.RouteTTLSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.RouteTTLSeconds) * time.Second
}

func (c NodeConfig) QueueSize() int {
	if c.BindingQueueSize <= 0 {
		return 4096
	}
	return c.BindingQueueSize
}

func (c NodeConfig) DrainWindow() time.Duration {
	if c.DrainSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.DrainSeconds) * time.Second
}
