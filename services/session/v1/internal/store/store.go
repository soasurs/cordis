package store

import (
	"context"
	"time"
)

type Node struct {
	ID         string
	Generation string
	RPCAddress string
	Status     string
	ExpiresAt  int64
}

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
	RegisterNode(ctx context.Context, node Node, ttl time.Duration) error
	SetOwner(ctx context.Context, owner Owner, ttl time.Duration) error
	DeleteOwner(ctx context.Context, sessionID, nodeID, generation string) error
	RefreshRoutes(ctx context.Context, nodeID, generation string, routes []Route, ttl time.Duration) error
	DetachRoutes(ctx context.Context, nodeID, generation string, routes []Route) error
}
