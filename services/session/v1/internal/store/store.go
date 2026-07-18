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

type RouteKind string

const (
	RouteUser    RouteKind = "users"
	RouteGuild   RouteKind = "guilds"
	RouteChannel RouteKind = "channels"
)

type Route struct {
	Kind RouteKind
	ID   int64
}

type Store interface {
	ClaimAuthSession(ctx context.Context, authSessionID int64, logicalSessionID string, ttl time.Duration) (bool, error)
	RefreshAuthSession(ctx context.Context, authSessionID int64, logicalSessionID string, ttl time.Duration) (bool, error)
	DeleteAuthSession(ctx context.Context, authSessionID int64, logicalSessionID string) error
	SetOwner(ctx context.Context, owner Owner, ttl time.Duration) error
	DeleteOwner(ctx context.Context, sessionID, nodeID, generation string) error
	RefreshRoutes(ctx context.Context, nodeID, generation string, routes []Route, ttl time.Duration) error
	DetachRoutes(ctx context.Context, nodeID, generation string, routes []Route) error
}
