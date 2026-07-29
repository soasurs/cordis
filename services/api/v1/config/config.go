package config

import (
	"errors"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/probe"
	"github.com/soasurs/cordis/pkg/ratelimit"
	"github.com/soasurs/cordis/services/api/v1/observability"
)

type Config struct {
	Name          string
	ListenOn      string
	ProbeServer   probe.HTTPConfig
	Log           logx.LogConf
	Observability observability.Config
	Inbound       InboundConfig
	RateLimit     RateLimitConfig
	ReadStates    ReadStatesConfig
	BrowserAuth   BrowserAuthConfig
	Services      ServiceConfig
}

type BrowserAuthConfig struct {
	AccessCookieName              string        `json:",default=cordis_access"`
	RefreshCookieName             string        `json:",default=cordis_refresh"`
	Secure                        bool          `json:",default=false"`
	AllowedOrigins                []string      `json:",optional"`
	GatewayTicketMinimumAccessTTL time.Duration `json:",default=30s"`
}

func (c BrowserAuthConfig) EffectiveAccessCookieName() string {
	if c.AccessCookieName == "" {
		return "cordis_access"
	}
	return c.AccessCookieName
}

func (c BrowserAuthConfig) EffectiveRefreshCookieName() string {
	if c.RefreshCookieName == "" {
		return "cordis_refresh"
	}
	return c.RefreshCookieName
}

// InboundConfig controls public HTTP and Connect-RPC resource protection.
type InboundConfig struct {
	ReadTimeout            time.Duration            `json:",default=5s"`
	ReadHeaderTimeout      time.Duration            `json:",default=5s"`
	WriteTimeout           time.Duration            `json:",default=10s"`
	IdleTimeout            time.Duration            `json:",default=2m"`
	ShutdownTimeout        time.Duration            `json:",default=15s"`
	MaxHeaderBytes         int                      `json:",default=1048576"`
	MaxRequestBytes        int64                    `json:",default=2097152"`
	MaxMessageBytes        int                      `json:",default=1048576"`
	Timeout                time.Duration            `json:",default=3s"`
	ProcedureTimeouts      map[string]time.Duration `json:",optional"`
	ServiceMaxMessageBytes map[string]int           `json:",optional"`
	MaxConcurrency         int64                    `json:",default=10000"`
	CPUThreshold           int64                    `json:",default=900,range=[0:1000)"`
	Breaker                bool                     `json:",default=true"`
}

// Validate rejects resource-protection settings that would disable or invert
// the public server's safety bounds.
func (c InboundConfig) Validate() error {
	maxTimeout := c.Timeout
	for procedure, timeout := range c.ProcedureTimeouts {
		if procedure == "" || procedure[0] != '/' {
			return errors.New("inbound procedure timeout key must be a full procedure")
		}
		if timeout <= 0 {
			return errors.New("inbound procedure timeout must be positive")
		}
		maxTimeout = max(maxTimeout, timeout)
	}
	for service, maxMessageBytes := range c.ServiceMaxMessageBytes {
		switch service {
		case "authenticator", "user", "message", "guild", "presence":
		default:
			return errors.New("inbound service max message bytes has unknown service")
		}
		if maxMessageBytes <= 0 {
			return errors.New("inbound service max message bytes must be positive")
		}
		if int64(maxMessageBytes) >= c.MaxRequestBytes {
			return errors.New("inbound service max message bytes must be less than max request bytes")
		}
	}
	switch {
	case c.ReadTimeout <= 0:
		return errors.New("inbound read timeout must be positive")
	case c.ReadHeaderTimeout <= 0:
		return errors.New("inbound read header timeout must be positive")
	case c.WriteTimeout <= 0:
		return errors.New("inbound write timeout must be positive")
	case c.IdleTimeout <= 0:
		return errors.New("inbound idle timeout must be positive")
	case c.ShutdownTimeout <= c.WriteTimeout:
		return errors.New("inbound shutdown timeout must exceed write timeout")
	case c.MaxHeaderBytes <= 0:
		return errors.New("inbound max header bytes must be positive")
	case c.MaxRequestBytes <= 0:
		return errors.New("inbound max request bytes must be positive")
	case c.MaxMessageBytes <= 0:
		return errors.New("inbound max message bytes must be positive")
	case int64(c.MaxMessageBytes) >= c.MaxRequestBytes:
		return errors.New("inbound max message bytes must be less than max request bytes")
	case c.Timeout <= 0:
		return errors.New("inbound timeout must be positive")
	case c.WriteTimeout <= c.ReadTimeout || c.WriteTimeout-c.ReadTimeout <= maxTimeout:
		return errors.New("inbound write timeout must exceed read timeout plus request timeout")
	case c.MaxConcurrency <= 0:
		return errors.New("inbound max concurrency must be positive")
	case c.CPUThreshold < 0 || c.CPUThreshold >= 1000:
		return errors.New("inbound CPU threshold must be between 0 and 999")
	default:
		return nil
	}
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
	Presence      zrpc.RpcClientConf
}
