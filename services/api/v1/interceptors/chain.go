// Package interceptors assembles the public API's ordered Connect-RPC
// observability, admission, failure-protection, and rate-limit interceptors.
package interceptors

import (
	"errors"
	"time"

	"connectrpc.com/connect"

	coreratelimit "github.com/soasurs/cordis/pkg/ratelimit"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
)

// Config controls the public API interceptor chain.
type Config struct {
	Timeout                time.Duration
	ProcedureTimeouts      map[string]time.Duration
	MaxConcurrency         int64
	CPUThreshold           int64
	Breaker                bool
	MaxMessageBytes        int
	ServiceMaxMessageBytes map[string]int
	RateLimiter            coreratelimit.Limiter
	ClientIPResolver       *apiratelimit.ClientIPResolver
}

// Service identifies one generated public API service handler.
type Service string

const (
	// AuthenticatorService identifies the public authentication API.
	AuthenticatorService Service = "authenticator"
	// UserService identifies the public user API.
	UserService Service = "user"
	// MessageService identifies the public message API.
	MessageService Service = "message"
	// GuildService identifies the public guild API.
	GuildService Service = "guild"
)

// Runtime owns the process-local controls and ordered handler options for the
// public API interceptor chain.
type Runtime struct {
	interceptors           []connect.Interceptor
	maxMessageBytes        int
	serviceMaxMessageBytes map[string]int
}

// New constructs the complete public API interceptor chain.
func New(cfg Config) (*Runtime, error) {
	if cfg.MaxMessageBytes <= 0 {
		return nil, errors.New("api inbound max message bytes must be positive")
	}
	for procedure, timeout := range cfg.ProcedureTimeouts {
		if procedure == "" || procedure[0] != '/' {
			return nil, errors.New("api procedure timeout key must be a full procedure")
		}
		if timeout <= 0 {
			return nil, errors.New("api procedure timeout must be positive")
		}
	}
	for service, maxMessageBytes := range cfg.ServiceMaxMessageBytes {
		if !knownService(Service(service)) {
			return nil, errors.New("api service max message bytes has unknown service")
		}
		if maxMessageBytes <= 0 {
			return nil, errors.New("api service max message bytes must be positive")
		}
	}
	if cfg.RateLimiter == nil {
		return nil, errors.New("api rate limiter is required")
	}
	if cfg.ClientIPResolver == nil {
		return nil, errors.New("api client IP resolver is required")
	}
	protection, err := newProtectionRuntime(protectionConfig{
		Timeout:           cfg.Timeout,
		ProcedureTimeouts: cfg.ProcedureTimeouts,
		MaxConcurrency:    cfg.MaxConcurrency,
		CPUThreshold:      cfg.CPUThreshold,
		Breaker:           cfg.Breaker,
	})
	if err != nil {
		return nil, err
	}
	return &Runtime{
		interceptors: assembleInterceptors(
			observabilityInterceptors(),
			protection.interceptors(),
			[]connect.Interceptor{
				apiratelimit.UnaryInterceptor(cfg.RateLimiter, cfg.ClientIPResolver),
			},
		),
		maxMessageBytes:        cfg.MaxMessageBytes,
		serviceMaxMessageBytes: cloneMap(cfg.ServiceMaxMessageBytes),
	}, nil
}

// HandlerOptions returns the ordered Connect-RPC options shared by every
// public API service handler.
func (r *Runtime) HandlerOptions(service Service) []connect.HandlerOption {
	return []connect.HandlerOption{
		connect.WithInterceptors(r.interceptors...),
		connect.WithReadMaxBytes(r.maxMessageBytesFor(service)),
		connect.WithRecover(recoverPanic),
	}
}

func (r *Runtime) maxMessageBytesFor(service Service) int {
	if serviceMaxMessageBytes, ok := r.serviceMaxMessageBytes[string(service)]; ok {
		return serviceMaxMessageBytes
	}
	return r.maxMessageBytes
}

func knownService(service Service) bool {
	switch service {
	case AuthenticatorService, UserService, MessageService, GuildService:
		return true
	default:
		return false
	}
}

func assembleInterceptors(groups ...[]connect.Interceptor) []connect.Interceptor {
	var total int
	for _, group := range groups {
		total += len(group)
	}
	interceptors := make([]connect.Interceptor, 0, total)
	for _, group := range groups {
		interceptors = append(interceptors, group...)
	}
	return interceptors
}
