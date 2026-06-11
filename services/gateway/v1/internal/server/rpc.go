package server

import (
	"context"

	gatewayv1 "github.com/soasurs/cordis/gen/gateway/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) DispatchChannelEvent(_ context.Context, req *gatewayv1.DispatchChannelEventRequest) (*gatewayv1.DispatchChannelEventResponse, error) {
	if req.GetChannelId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "channel_id is required")
	}
	event := req.GetEvent()
	payload, err := validateDispatchEvent(event.GetType(), event.GetJsonPayload())
	if err != nil {
		return nil, err
	}

	delivered := 0
	for _, c := range s.hub.channelClients(req.GetChannelId()) {
		if err := c.dispatch(event.GetType(), payload); err == nil {
			delivered++
		}
	}
	resp := new(gatewayv1.DispatchChannelEventResponse)
	resp.SetDelivered(int32(delivered))
	return resp, nil
}

func (s *Server) DispatchUserEvent(_ context.Context, req *gatewayv1.DispatchUserEventRequest) (*gatewayv1.DispatchUserEventResponse, error) {
	if req.GetUserId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	event := req.GetEvent()
	payload, err := validateDispatchEvent(event.GetType(), event.GetJsonPayload())
	if err != nil {
		return nil, err
	}

	delivered := 0
	for _, c := range s.hub.userClients(req.GetUserId()) {
		if err := c.dispatch(event.GetType(), payload); err == nil {
			delivered++
		}
	}
	resp := new(gatewayv1.DispatchUserEventResponse)
	resp.SetDelivered(int32(delivered))
	return resp, nil
}

func (s *Server) DispatchSessionEvent(_ context.Context, req *gatewayv1.DispatchSessionEventRequest) (*gatewayv1.DispatchSessionEventResponse, error) {
	if req.GetSessionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	event := req.GetEvent()
	payload, err := validateDispatchEvent(event.GetType(), event.GetJsonPayload())
	if err != nil {
		return nil, err
	}

	delivered := 0
	if c := s.hub.sessionClient(req.GetSessionId()); c != nil {
		if err := c.dispatch(event.GetType(), payload); err == nil {
			delivered = 1
		}
	}
	resp := new(gatewayv1.DispatchSessionEventResponse)
	resp.SetDelivered(int32(delivered))
	return resp, nil
}
