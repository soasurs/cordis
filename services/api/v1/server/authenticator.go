package server

import (
	"context"
	"net"

	"connectrpc.com/connect"
	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"google.golang.org/protobuf/proto"
)

func (s *authenticatorServer) Register(ctx context.Context, req *apiv1.RegisterRequest) (*apiv1.RegisterResponse, error) {
	internalReq := new(authenticatorv1.RegisterRequest)
	internalReq.SetName(req.GetName())
	internalReq.SetEmail(req.GetEmail())
	internalReq.SetPassword(req.GetPassword())
	setClientMetadata(ctx, internalReq.SetUserAgent, internalReq.SetIp)

	internalResp, err := s.svcCtx.AuthenticatorClient.Register(ctx, internalReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	return &apiv1.RegisterResponse{
		Result: toAPIAuthenticationResult(internalResp.GetResult()),
	}, nil
}

func (s *authenticatorServer) Login(ctx context.Context, req *apiv1.LoginRequest) (*apiv1.LoginResponse, error) {
	internalReq := new(authenticatorv1.LoginRequest)
	internalReq.SetEmail(req.GetEmail())
	internalReq.SetPassword(req.GetPassword())
	setClientMetadata(ctx, internalReq.SetUserAgent, internalReq.SetIp)

	internalResp, err := s.svcCtx.AuthenticatorClient.Login(ctx, internalReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	return &apiv1.LoginResponse{
		Result: toAPIAuthenticationResult(internalResp.GetResult()),
	}, nil
}

func (s *authenticatorServer) Refresh(ctx context.Context, req *apiv1.RefreshRequest) (*apiv1.RefreshResponse, error) {
	internalReq := new(authenticatorv1.RefreshRequest)
	internalReq.SetRefreshToken(req.GetRefreshToken())

	internalResp, err := s.svcCtx.AuthenticatorClient.Refresh(ctx, internalReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	return &apiv1.RefreshResponse{
		Result: toAPIAuthenticationResult(internalResp.GetResult()),
	}, nil
}

func (s *authenticatorServer) Logout(ctx context.Context, req *apiv1.LogoutRequest) (*apiv1.LogoutResponse, error) {
	internalReq := new(authenticatorv1.LogoutRequest)
	internalReq.SetRefreshToken(req.GetRefreshToken())

	internalResp, err := s.svcCtx.AuthenticatorClient.Logout(ctx, internalReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	return &apiv1.LogoutResponse{
		Ok: proto.Bool(internalResp.GetOk()),
	}, nil
}

func setClientMetadata(ctx context.Context, setUserAgent, setIP func(string)) {
	callInfo, ok := connect.CallInfoForHandlerContext(ctx)
	if !ok {
		return
	}

	setUserAgent(callInfo.RequestHeader().Get("User-Agent"))
	setIP(clientIP(callInfo.Peer().Addr))
}

func clientIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return address
}

func toAPIAuthenticationResult(result *authenticatorv1.AuthenticationResult) *apiv1.AuthenticationResult {
	if result == nil {
		return &apiv1.AuthenticationResult{
			Ok: proto.Bool(false),
		}
	}

	return &apiv1.AuthenticationResult{
		Ok:                    proto.Bool(result.GetOk()),
		UserId:                proto.Int64(result.GetUserId()),
		SessionId:             proto.Int64(result.GetSessionId()),
		AccessToken:           proto.String(result.GetAccessToken()),
		AccessTokenExpiresAt:  proto.Int64(result.GetAccessTokenExpiresAt()),
		RefreshToken:          proto.String(result.GetRefreshToken()),
		RefreshTokenExpiresAt: proto.Int64(result.GetRefreshTokenExpiresAt()),
		SessionExpiresAt:      proto.Int64(result.GetSessionExpiresAt()),
	}
}
