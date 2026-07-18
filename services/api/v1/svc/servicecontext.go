package svc

import (
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	coreratelimit "github.com/soasurs/cordis/pkg/ratelimit"
	"github.com/soasurs/cordis/services/api/v1/config"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
)

type ServiceContext struct {
	Cfg                 config.Config
	AuthenticatorClient authenticatorv1.AuthenticatorServiceClient
	UserClient          userv1.UserServiceClient
	MessageClient       messagev1.MessageServiceClient
	GuildClient         guildv1.GuildServiceClient
	RateLimiter         coreratelimit.Limiter
}

type Dependencies struct {
	AuthenticatorClient authenticatorv1.AuthenticatorServiceClient
	UserClient          userv1.UserServiceClient
	MessageClient       messagev1.MessageServiceClient
	GuildClient         guildv1.GuildServiceClient
	RateLimiter         coreratelimit.Limiter
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	authenticatorClient, err := zrpc.NewClient(cfg.Services.Authenticator)
	if err != nil {
		return Dependencies{}, err
	}
	userClient, err := zrpc.NewClient(cfg.Services.User)
	if err != nil {
		return Dependencies{}, err
	}
	messageClient, err := zrpc.NewClient(cfg.Services.Message)
	if err != nil {
		return Dependencies{}, err
	}
	guildClient, err := zrpc.NewClient(cfg.Services.Guild)
	if err != nil {
		return Dependencies{}, err
	}
	for _, policy := range apiratelimit.RequiredPolicies() {
		if _, ok := cfg.RateLimit.Policies[policy]; !ok {
			return Dependencies{}, fmt.Errorf("api rate limit policy %q is required", policy)
		}
	}
	rds, err := redis.NewRedis(cfg.RateLimit.Redis)
	if err != nil {
		return Dependencies{}, err
	}
	limiter, err := coreratelimit.NewManager(
		coreratelimit.NewRedisBackend(rds),
		cfg.RateLimit.Policies,
		coreratelimit.Options{
			KeyPrefix:             cfg.RateLimit.KeyPrefix,
			FallbackMaxKeys:       cfg.RateLimit.FallbackMaxKeys,
			FallbackRetryInterval: cfg.RateLimit.FallbackRetryInterval,
		},
	)
	if err != nil {
		return Dependencies{}, err
	}
	return Dependencies{
		AuthenticatorClient: authenticatorv1.NewAuthenticatorServiceClient(authenticatorClient.Conn()),
		UserClient:          userv1.NewUserServiceClient(userClient.Conn()),
		MessageClient:       messagev1.NewMessageServiceClient(messageClient.Conn()),
		GuildClient:         guildv1.NewGuildServiceClient(guildClient.Conn()),
		RateLimiter:         limiter,
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
	if deps.AuthenticatorClient == nil {
		panic("authenticator client is required")
	}
	if deps.UserClient == nil {
		panic("user client is required")
	}
	if deps.MessageClient == nil {
		panic("message client is required")
	}
	if deps.GuildClient == nil {
		panic("guild client is required")
	}
	if len(cfg.RateLimit.Policies) > 0 && deps.RateLimiter == nil {
		panic("api rate limiter is required")
	}
	return &ServiceContext{
		Cfg:                 cfg,
		AuthenticatorClient: deps.AuthenticatorClient,
		UserClient:          deps.UserClient,
		MessageClient:       deps.MessageClient,
		GuildClient:         deps.GuildClient,
		RateLimiter:         deps.RateLimiter,
	}
}
