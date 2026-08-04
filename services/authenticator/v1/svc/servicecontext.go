package svc

import (
	"context"
	"errors"

	sn "github.com/bwmarrin/snowflake"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zeromicro/go-zero/core/limit"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"

	mailerv1 "github.com/soasurs/cordis/gen/mailer/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/concurrencylimit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/authenticator/v1/config"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/gatewayticket"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/twofactor"
)

type ServiceContext struct {
	Cfg        config.Config
	Store      store.Store
	Tokens     *token.Manager
	TwoFactor  *twofactor.Cipher
	Snowflake  *sn.Node
	UserClient userv1.UserServiceClient
	// MailerClient is optional; recovery flows skip delivery when nil.
	MailerClient mailerv1.MailerServiceClient
	// RecoveryLimiter is optional; recovery flows skip throttling when nil.
	RecoveryLimiter RecoveryLimiter
	// PasswordLimiter bounds process-local Argon2 work.
	PasswordLimiter PasswordLimiter
	GatewayTickets  gatewayticket.Store
}

// PasswordLimiter acquires weighted permits before Argon2 work.
type PasswordLimiter interface {
	Acquire(ctx context.Context, weight int64) (func(), error)
}

// RecoveryLimiter throttles recovery mail requests per target key.
type RecoveryLimiter interface {
	// Allow reports whether the request for key may proceed now.
	Allow(ctx context.Context, key string) (bool, error)
}

type periodRecoveryLimiter struct {
	limiter *limit.PeriodLimit
}

func (l *periodRecoveryLimiter) Allow(ctx context.Context, key string) (bool, error) {
	state, err := l.limiter.TakeCtx(ctx, key)
	if err != nil {
		return false, err
	}
	return state == limit.Allowed || state == limit.HitQuota, nil
}

type Dependencies struct {
	Store           store.Store
	Tokens          *token.Manager
	TwoFactor       *twofactor.Cipher
	Snowflake       *sn.Node
	UserClient      userv1.UserServiceClient
	MailerClient    mailerv1.MailerServiceClient
	RecoveryLimiter RecoveryLimiter
	PasswordLimiter PasswordLimiter
	GatewayTickets  gatewayticket.Store
	DB              *pgxpool.Pool
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	if err := validateRegistrationConfig(cfg.Registration); err != nil {
		return Dependencies{}, err
	}
	if err := validateSessionConfig(cfg.Sessions); err != nil {
		return Dependencies{}, err
	}
	if cfg.GatewayTickets.TTL <= 0 || cfg.GatewayTickets.Redis.Host == "" || cfg.GatewayTickets.KeyPrefix == "" {
		return Dependencies{}, errors.New("gateway ticket redis, ttl, and key prefix are required")
	}
	if cfg.TwoFactor.EnrollmentTTL <= 0 || cfg.TwoFactor.LoginChallengeTTL <= 0 {
		return Dependencies{}, errors.New("two-factor TTLs must be positive")
	}
	if cfg.TwoFactor.Issuer == "" {
		return Dependencies{}, errors.New("two-factor issuer is required")
	}
	if cfg.TwoFactor.MaxAttempts <= 0 || cfg.TwoFactor.RecoveryCodeCount <= 0 {
		return Dependencies{}, errors.New("two-factor limits must be positive")
	}
	if cfg.Recovery.PasswordResetTTL <= 0 || cfg.Recovery.EmailVerificationTTL <= 0 {
		return Dependencies{}, errors.New("recovery TTLs must be positive")
	}
	if cfg.Recovery.RequestIntervalSeconds <= 0 {
		return Dependencies{}, errors.New("recovery request interval must be positive")
	}
	if cfg.Password.MaxConcurrency <= 0 {
		return Dependencies{}, errors.New("password max concurrency must be positive")
	}

	twoFactorKeys := make([]twofactor.KeyConfig, 0, len(cfg.TwoFactor.Encryption.Keys))
	for _, key := range cfg.TwoFactor.Encryption.Keys {
		twoFactorKeys = append(twoFactorKeys, twofactor.KeyConfig{ID: key.ID, Secret: key.Secret})
	}
	twoFactorCipher, err := twofactor.NewCipher(cfg.TwoFactor.Encryption.PrimaryKeyID, twoFactorKeys)
	if err != nil {
		return Dependencies{}, err
	}

	node, err := snowflake.New()
	if err != nil {
		return Dependencies{}, err
	}

	tokenManager, err := token.NewManager(token.Config{
		Issuer:        cfg.Tokens.Issuer,
		AccessSecret:  cfg.Tokens.Access.Secret,
		RefreshSecret: cfg.Tokens.Refresh.Secret,
		AccessTTL:     cfg.Tokens.Access.TTL,
		RefreshTTL:    cfg.Tokens.Refresh.TTL,
	})
	if err != nil {
		return Dependencies{}, err
	}

	userRPCClient, err := zrpc.NewClient(cfg.Services.User)
	if err != nil {
		return Dependencies{}, err
	}

	var mailerClient mailerv1.MailerServiceClient
	if len(cfg.Services.Mailer.Endpoints) > 0 || cfg.Services.Mailer.Target != "" {
		mailerRPCClient, err := zrpc.NewClient(cfg.Services.Mailer)
		if err != nil {
			return Dependencies{}, err
		}
		mailerClient = mailerv1.NewMailerServiceClient(mailerRPCClient.Conn())
	}

	var recoveryLimiter RecoveryLimiter
	if cfg.Recovery.Redis.Host != "" {
		rds, err := redis.NewRedis(cfg.Recovery.Redis)
		if err != nil {
			return Dependencies{}, err
		}
		recoveryLimiter = &periodRecoveryLimiter{limiter: limit.NewPeriodLimit(
			cfg.Recovery.RequestIntervalSeconds,
			1,
			rds,
			"authenticator:recovery:",
		)}
	}

	passwordLimiter, err := concurrencylimit.New("authenticator_argon2", cfg.Password.MaxConcurrency)
	if err != nil {
		return Dependencies{}, err
	}
	ticketRedis, err := redis.NewRedis(cfg.GatewayTickets.Redis)
	if err != nil {
		return Dependencies{}, err
	}

	db, err := database.NewPostgresPool(context.Background(), cfg.Database)
	if err != nil {
		return Dependencies{}, err
	}

	return Dependencies{
		Store:           store.New(db),
		Tokens:          tokenManager,
		TwoFactor:       twoFactorCipher,
		Snowflake:       node,
		UserClient:      userv1.NewUserServiceClient(userRPCClient.Conn()),
		MailerClient:    mailerClient,
		RecoveryLimiter: recoveryLimiter,
		PasswordLimiter: passwordLimiter,
		GatewayTickets:  gatewayticket.NewRedisStore(ticketRedis, cfg.GatewayTickets.KeyPrefix),
		DB:              db,
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
	if err := validateRegistrationConfig(cfg.Registration); err != nil {
		panic(err)
	}
	if err := validateSessionConfig(cfg.Sessions); err != nil {
		panic(err)
	}
	if deps.Store == nil {
		panic("authenticator store is required")
	}
	if deps.Tokens == nil {
		panic("token manager is required")
	}
	if deps.GatewayTickets == nil {
		panic("gateway ticket store is required")
	}
	if deps.TwoFactor == nil {
		panic("two-factor cipher is required")
	}
	if deps.Snowflake == nil {
		panic("snowflake node is required")
	}
	if deps.UserClient == nil {
		panic("user client is required")
	}
	if cfg.Password.MaxConcurrency > 0 && deps.PasswordLimiter == nil {
		panic("password limiter is required")
	}
	return &ServiceContext{
		Cfg:             cfg,
		Store:           deps.Store,
		Tokens:          deps.Tokens,
		TwoFactor:       deps.TwoFactor,
		Snowflake:       deps.Snowflake,
		UserClient:      deps.UserClient,
		MailerClient:    deps.MailerClient,
		RecoveryLimiter: deps.RecoveryLimiter,
		PasswordLimiter: deps.PasswordLimiter,
		GatewayTickets:  deps.GatewayTickets,
	}
}

func validateSessionConfig(cfg config.SessionConfig) error {
	switch {
	case cfg.IdleTTL <= 0:
		return errors.New("session idle ttl must be positive")
	case cfg.AbsoluteTTL < cfg.IdleTTL:
		return errors.New("session absolute ttl must be at least idle ttl")
	case cfg.RotationGrace <= 0:
		return errors.New("session rotation grace must be positive")
	default:
		return nil
	}
}

func validateRegistrationConfig(cfg config.RegistrationConfig) error {
	switch cfg.EffectiveMode() {
	case config.RegistrationModeOpen, config.RegistrationModeInviteOnly, config.RegistrationModeClosed:
	default:
		return errors.New("registration mode must be open, invite_only, or closed")
	}
	if cfg.EffectiveReservationTTL() <= 0 {
		return errors.New("registration reservation ttl must be positive")
	}
	return nil
}
