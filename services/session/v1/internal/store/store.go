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
	RouteUser  RouteKind = "users"
	RouteGuild RouteKind = "guilds"
)

type Route struct {
	Kind RouteKind
	ID   int64
}

type Store interface {
	SetOwner(ctx context.Context, owner Owner, ttl time.Duration) error
	SetOwners(ctx context.Context, owners []Owner, ttl time.Duration) error
	DeleteOwner(ctx context.Context, sessionID, nodeID, generation string) error
	RefreshRoutes(ctx context.Context, nodeID, generation string, routes []Route, ttl time.Duration) error
	DetachRoutes(ctx context.Context, nodeID, generation string, routes []Route) error
}
