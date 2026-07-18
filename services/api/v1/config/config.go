package config

import (
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/ratelimit"
	"github.com/soasurs/cordis/services/api/v1/observability"
)

type Config struct {
	Name          string
	ListenOn      string
	Log           logx.LogConf
	Observability observability.Config
	RateLimit     RateLimitConfig
	ReadStates    ReadStatesConfig
	Services      ServiceConfig
}

// RateLimitConfig defines API rate-limit storage, policies, and proxy trust.
type RateLimitConfig struct {
	Redis                 redis.RedisConf
	KeyPrefix             string        `json:",default=cordis:api:rate_limit:"`
	FallbackMaxKeys       int           `json:",default=10000"`
	FallbackRetryInterval time.Duration `json:",default=1s"`
	TrustedProxies        []string      `json:",optional"`
	Policies              map[string]ratelimit.Policy
}

// ReadStatesConfig controls API-side per-user concurrency for GetReadStates.
type ReadStatesConfig struct {
	MaxConcurrencyPerUser int64 `json:",default=2"`
}

type ServiceConfig struct {
	Authenticator zrpc.RpcClientConf
	User          zrpc.RpcClientConf
	Message       zrpc.RpcClientConf
	Guild         zrpc.RpcClientConf
}
