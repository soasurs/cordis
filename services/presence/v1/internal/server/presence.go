package server

import (
	"context"

	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/services/presence/v1/internal/store"
)

func (s *presenceServer) RegisterGateway(ctx context.Context, req *presencev1.RegisterGatewayRequest) (*presencev1.RegisterGatewayResponse, error) {
	if req.GetGatewayId() == "" {
		return nil, errGatewayIDRequired
	}
	if req.GetGeneration() == "" {
		return nil, errGenerationRequired
	}
	if req.GetRpcAddr() == "" {
		return nil, errRPCAddrRequired
	}

	gateway, err := s.svcCtx.Store.UpsertGateway(ctx, store.Gateway{
		GatewayID:  req.GetGatewayId(),
		Generation: req.GetGeneration(),
		RPCAddr:    req.GetRpcAddr(),
	})
	if err != nil {
		return nil, err
	}

	resp := new(presencev1.RegisterGatewayResponse)
	resp.SetGateway(gatewayToProto(gateway))
	return resp, nil
}

func (s *presenceServer) RefreshChannelRoutes(ctx context.Context, req *presencev1.RefreshChannelRoutesRequest) (*presencev1.RefreshChannelRoutesResponse, error) {
	if req.GetGatewayId() == "" {
		return nil, errGatewayIDRequired
	}
	if req.GetGeneration() == "" {
		return nil, errGenerationRequired
	}

	refreshed, err := s.svcCtx.Store.RefreshChannelRoutes(ctx, req.GetGatewayId(), req.GetGeneration(), req.GetChannelIds())
	if err != nil {
		return nil, err
	}

	resp := new(presencev1.RefreshChannelRoutesResponse)
	resp.SetRefreshed(int32(refreshed))
	return resp, nil
}

func (s *presenceServer) DetachChannelRoute(ctx context.Context, req *presencev1.DetachChannelRouteRequest) (*presencev1.DetachChannelRouteResponse, error) {
	if req.GetGatewayId() == "" {
		return nil, errGatewayIDRequired
	}
	if req.GetGeneration() == "" {
		return nil, errGenerationRequired
	}
	if req.GetChannelId() == 0 {
		return nil, errChannelIDRequired
	}

	if err := s.svcCtx.Store.DetachChannelRoute(ctx, req.GetGatewayId(), req.GetGeneration(), req.GetChannelId()); err != nil {
		return nil, err
	}

	resp := new(presencev1.DetachChannelRouteResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *presenceServer) ResolveChannelGateways(ctx context.Context, req *presencev1.ResolveChannelGatewaysRequest) (*presencev1.ResolveChannelGatewaysResponse, error) {
	if req.GetChannelId() == 0 {
		return nil, errChannelIDRequired
	}

	gateways, err := s.svcCtx.Store.ResolveChannelGateways(ctx, req.GetChannelId())
	if err != nil {
		return nil, err
	}

	resp := new(presencev1.ResolveChannelGatewaysResponse)
	resp.SetGateways(gatewaysToProto(gateways))
	return resp, nil
}

func gatewayToProto(gateway store.Gateway) *presencev1.GatewayInstance {
	msg := new(presencev1.GatewayInstance)
	msg.SetGatewayId(gateway.GatewayID)
	msg.SetGeneration(gateway.Generation)
	msg.SetRpcAddr(gateway.RPCAddr)
	msg.SetExpiresAt(gateway.ExpiresAt)
	return msg
}

func gatewaysToProto(gateways []store.Gateway) []*presencev1.GatewayInstance {
	values := make([]*presencev1.GatewayInstance, 0, len(gateways))
	for _, gateway := range gateways {
		values = append(values, gatewayToProto(gateway))
	}
	return values
}
