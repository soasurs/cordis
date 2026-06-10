package server

import (
	"context"
	"errors"
	"testing"

	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/services/presence/v1/config"
	"github.com/soasurs/cordis/services/presence/v1/internal/store"
	"github.com/soasurs/cordis/services/presence/v1/internal/svc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRegisterGateway(t *testing.T) {
	server, fake := newTestServer()

	req := new(presencev1.RegisterGatewayRequest)
	req.SetGatewayId("gw-a")
	req.SetGeneration("gen-1")
	req.SetRpcAddr("10.0.0.1:3004")

	resp, err := server.RegisterGateway(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "gw-a", resp.GetGateway().GetGatewayId())
	require.Equal(t, "gen-1", resp.GetGateway().GetGeneration())
	require.Equal(t, "10.0.0.1:3004", resp.GetGateway().GetRpcAddr())
	require.Equal(t, int64(12345), resp.GetGateway().GetExpiresAt())
	require.Equal(t, store.Gateway{
		GatewayID:  "gw-a",
		Generation: "gen-1",
		RPCAddr:    "10.0.0.1:3004",
	}, fake.upserted)
}

func TestRegisterGatewayValidation(t *testing.T) {
	server, _ := newTestServer()

	_, err := server.RegisterGateway(t.Context(), new(presencev1.RegisterGatewayRequest))
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestRefreshChannelRoutes(t *testing.T) {
	server, fake := newTestServer()

	req := new(presencev1.RefreshChannelRoutesRequest)
	req.SetGatewayId("gw-a")
	req.SetGeneration("gen-1")
	req.SetChannelIds([]int64{1001, 1002})

	resp, err := server.RefreshChannelRoutes(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int32(2), resp.GetRefreshed())
	require.Equal(t, "gw-a", fake.refreshedGatewayID)
	require.Equal(t, "gen-1", fake.refreshedGeneration)
	require.Equal(t, []int64{1001, 1002}, fake.refreshedChannelIDs)
}

func TestDetachChannelRoute(t *testing.T) {
	server, fake := newTestServer()

	req := new(presencev1.DetachChannelRouteRequest)
	req.SetGatewayId("gw-a")
	req.SetGeneration("gen-1")
	req.SetChannelId(1001)

	resp, err := server.DetachChannelRoute(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, "gw-a", fake.detachedGatewayID)
	require.Equal(t, "gen-1", fake.detachedGeneration)
	require.Equal(t, int64(1001), fake.detachedChannelID)
}

func TestResolveChannelGateways(t *testing.T) {
	server, fake := newTestServer()
	fake.gateways = []store.Gateway{
		{GatewayID: "gw-a", Generation: "gen-1", RPCAddr: "10.0.0.1:3004", ExpiresAt: 1000},
		{GatewayID: "gw-b", Generation: "gen-2", RPCAddr: "10.0.0.2:3004", ExpiresAt: 2000},
	}

	req := new(presencev1.ResolveChannelGatewaysRequest)
	req.SetChannelId(1001)

	resp, err := server.ResolveChannelGateways(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), fake.resolvedChannelID)
	require.Len(t, resp.GetGateways(), 2)
	require.Equal(t, "gw-a", resp.GetGateways()[0].GetGatewayId())
	require.Equal(t, "gen-2", resp.GetGateways()[1].GetGeneration())
}

func TestStoreErrorPropagates(t *testing.T) {
	server, fake := newTestServer()
	fake.err = errors.New("redis unavailable")

	req := new(presencev1.ResolveChannelGatewaysRequest)
	req.SetChannelId(1001)

	_, err := server.ResolveChannelGateways(t.Context(), req)
	require.ErrorIs(t, err, fake.err)
}

func newTestServer() (presencev1.PresenceServiceServer, *fakeStore) {
	fake := &fakeStore{}
	svcCtx := svc.NewServiceContextWithDependencies(config.Config{}, svc.Dependencies{Store: fake})
	return New(svcCtx), fake
}

type fakeStore struct {
	err error

	upserted store.Gateway

	refreshedGatewayID  string
	refreshedGeneration string
	refreshedChannelIDs []int64

	detachedGatewayID  string
	detachedGeneration string
	detachedChannelID  int64

	resolvedChannelID int64
	gateways          []store.Gateway
}

func (s *fakeStore) UpsertGateway(_ context.Context, gateway store.Gateway) (store.Gateway, error) {
	if s.err != nil {
		return store.Gateway{}, s.err
	}
	s.upserted = gateway
	gateway.ExpiresAt = 12345
	return gateway, nil
}

func (s *fakeStore) RefreshChannelRoutes(_ context.Context, gatewayID, generation string, channelIDs []int64) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	s.refreshedGatewayID = gatewayID
	s.refreshedGeneration = generation
	s.refreshedChannelIDs = append([]int64(nil), channelIDs...)
	return len(channelIDs), nil
}

func (s *fakeStore) DetachChannelRoute(_ context.Context, gatewayID, generation string, channelID int64) error {
	if s.err != nil {
		return s.err
	}
	s.detachedGatewayID = gatewayID
	s.detachedGeneration = generation
	s.detachedChannelID = channelID
	return nil
}

func (s *fakeStore) ResolveChannelGateways(_ context.Context, channelID int64) ([]store.Gateway, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.resolvedChannelID = channelID
	return s.gateways, nil
}
