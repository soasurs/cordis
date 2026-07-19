package store

import (
	"context"
	"time"
)

type Owner struct {
	SessionID  string
	NodeID     string
	Generation string
	ExpiresAt  int64
}

type AuthSessionClaim struct {
	AuthSessionID    int64
	LogicalSessionID string
	NodeID           string
	Generation       string
}

type AuthSessionClaimResult struct {
	Claimed  bool
	Existing AuthSessionClaim
}

type RouteKind string

const (
	RouteUser  RouteKind = "users"
	RouteGuild RouteKind = "guilds"
)

type Route struct {
	Kind RouteKind
	ID   int64
}

type Store interface {
	ClaimAuthSession(ctx context.Context, claim AuthSessionClaim, ttl time.Duration) (AuthSessionClaimResult, error)
	TakeoverAuthSession(ctx context.Context, expected, replacement AuthSessionClaim, ttl time.Duration) (bool, error)
	RefreshAuthSession(ctx context.Context, claim AuthSessionClaim, ttl time.Duration) (bool, error)
	RefreshAuthSessions(ctx context.Context, claims []AuthSessionClaim, ttl time.Duration) ([]string, error)
	DeleteAuthSession(ctx context.Context, claim AuthSessionClaim) error
	SetOwner(ctx context.Context, owner Owner, ttl time.Duration) error
	SetOwners(ctx context.Context, owners []Owner, ttl time.Duration) error
	DeleteOwner(ctx context.Context, sessionID, nodeID, generation string) error
	RefreshRoutes(ctx context.Context, nodeID, generation string, routes []Route, ttl time.Duration) error
	DetachRoutes(ctx context.Context, nodeID, generation string, routes []Route) error
}
