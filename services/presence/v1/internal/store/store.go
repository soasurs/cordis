package store

import "context"

type Gateway struct {
	GatewayID  string
	Generation string
	RPCAddr    string
	ExpiresAt  int64
}

type Store interface {
	UpsertGateway(ctx context.Context, gateway Gateway) (Gateway, error)
	RefreshChannelRoutes(ctx context.Context, gatewayID, generation string, channelIDs []int64) (int, error)
	DetachChannelRoute(ctx context.Context, gatewayID, generation string, channelID int64) error
	ResolveChannelGateways(ctx context.Context, channelID int64) ([]Gateway, error)
}
