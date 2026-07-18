package svc

import (
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/pkg/clientip"
	coreratelimit "github.com/soasurs/cordis/pkg/ratelimit"
	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/pkg/socketlimit"
	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/discovery"
	gatewayratelimit "github.com/soasurs/cordis/services/gateway/v1/ratelimit"
)

type ServiceContext struct {
	Cfg              config.Config
	Resolver         discovery.Resolver
	Registry         sessionregistry.Directory
	ClientIPResolver *clientip.Resolver
	RateLimiter      coreratelimit.Limiter
	SocketLimiter    socketlimit.Limiter
}

type Dependencies struct {
	Resolver         discovery.Resolver
	Registry         sessionregistry.Directory
	ClientIPResolver *clientip.Resolver
	RateLimiter      coreratelimit.Limiter
	SocketLimiter    socketlimit.Limiter
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	rds, err := redis.NewRedis(cfg.Redis)
	if err != nil {
		return Dependencies{}, err
	}
	registry, err := sessionregistry.New(cfg.SessionRegistry)
	if err != nil {
		return Dependencies{}, err
	}
	clientIPResolver, err := clientip.New(cfg.RateLimit.TrustedProxies)
	if err != nil {
		_ = registry.Close()
		return Dependencies{}, err
	}
	policies := make(map[string]coreratelimit.Policy, len(cfg.RateLimit.Policies))
	for name, policy := range cfg.RateLimit.Policies {
		policies[name] = coreratelimit.Policy{Limit: policy.Limit, Window: policy.Window}
	}
	for _, name := range gatewayratelimit.RequiredPolicies() {
		if _, ok := policies[name]; !ok {
			_ = registry.Close()
			return Dependencies{}, fmt.Errorf("gateway rate limit policy %q is required", name)
		}
	}
	limiter, err := coreratelimit.NewManager(coreratelimit.NewRedisBackend(rds), policies, coreratelimit.Options{
		KeyPrefix:             cfg.RateLimit.KeyPrefix,
		FallbackMaxKeys:       cfg.RateLimit.FallbackMaxKeys,
		FallbackRetryInterval: cfg.RateLimit.FallbackRetryInterval,
	})
	if err != nil {
		_ = registry.Close()
		return Dependencies{}, err
	}
	return Dependencies{
		Resolver:         discovery.New(rds, registry),
		Registry:         registry,
		ClientIPResolver: clientIPResolver,
		RateLimiter:      limiter,
		SocketLimiter:    socketlimit.NewManager(),
	}, nil
}

func NewServiceContext(cfg config.Config) *ServiceContext {
	deps, err := NewDependencies(cfg)
	if err != nil {
		panic(err)
	}
	return NewServiceContextWithDependencies(cfg, deps)
}

func NewServiceContextWithDependencies(cfg config.Config, deps Dependencies) *ServiceContext {
	if deps.Resolver == nil {
		panic("session resolver is required")
	}
	if deps.ClientIPResolver == nil {
		var err error
		deps.ClientIPResolver, err = clientip.New(cfg.RateLimit.TrustedProxies)
		if err != nil {
			panic(err)
		}
	}
	if len(cfg.RateLimit.Policies) > 0 && deps.RateLimiter == nil {
		panic("gateway rate limiter is required")
	}
	if deps.SocketLimiter == nil {
		deps.SocketLimiter = socketlimit.NewManager()
	}
	return &ServiceContext{
		Cfg: cfg, Resolver: deps.Resolver, Registry: deps.Registry,
		ClientIPResolver: deps.ClientIPResolver, RateLimiter: deps.RateLimiter,
		SocketLimiter: deps.SocketLimiter,
	}
}

func (s *ServiceContext) Close() error {
	if s.Registry != nil {
		return s.Registry.Close()
	}
	return nil
}
