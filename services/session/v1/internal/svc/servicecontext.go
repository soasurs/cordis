package svc

import (
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/services/session/v1/config"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
)

type ServiceContext struct {
	Cfg                 config.Config
	Store               store.Store
	SessionRegistry     sessionregistry.Directory
	AuthenticatorClient authenticatorv1.AuthenticatorServiceClient
	PresenceClient      presencev1.PresenceServiceClient
	GuildClient         guildv1.GuildServiceClient
}

type Dependencies struct {
	Store               store.Store
	SessionRegistry     sessionregistry.Directory
	AuthenticatorClient authenticatorv1.AuthenticatorServiceClient
	PresenceClient      presencev1.PresenceServiceClient
	GuildClient         guildv1.GuildServiceClient
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
	auth, err := zrpc.NewClient(cfg.Services.Authenticator)
	if err != nil {
		_ = registry.Close()
		return Dependencies{}, err
	}
	presence, err := zrpc.NewClient(cfg.Services.Presence)
	if err != nil {
		_ = registry.Close()
		return Dependencies{}, err
	}
	guild, err := zrpc.NewClient(cfg.Services.Guild)
	if err != nil {
		_ = registry.Close()
		return Dependencies{}, err
	}
	return Dependencies{
		Store:               store.NewRedisStore(rds),
		SessionRegistry:     registry,
		AuthenticatorClient: authenticatorv1.NewAuthenticatorServiceClient(auth.Conn()),
		PresenceClient:      presencev1.NewPresenceServiceClient(presence.Conn()),
		GuildClient:         guildv1.NewGuildServiceClient(guild.Conn()),
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
	if deps.Store == nil {
		panic("session store is required")
	}
	if deps.SessionRegistry == nil {
		panic("session registry is required")
	}
	if deps.AuthenticatorClient == nil {
		panic("authenticator client is required")
	}
	if deps.PresenceClient == nil {
		panic("presence client is required")
	}
	if deps.GuildClient == nil {
		panic("guild client is required")
	}
	return &ServiceContext{
		Cfg:                 cfg,
		Store:               deps.Store,
		SessionRegistry:     deps.SessionRegistry,
		AuthenticatorClient: deps.AuthenticatorClient,
		PresenceClient:      deps.PresenceClient,
		GuildClient:         deps.GuildClient,
	}
}

func (s *ServiceContext) Close() error {
	return s.SessionRegistry.Close()
}
