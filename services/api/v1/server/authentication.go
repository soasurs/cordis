package server

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
)

const bearerPrefix = "Bearer "

func authenticate(
	ctx context.Context,
	client authenticatorv1.AuthenticatorServiceClient,
) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	callInfo, ok := connect.CallInfoForHandlerContext(ctx)
	if !ok {
		return nil, invalidAccessTokenError()
	}

	authorization := callInfo.RequestHeader().Get("Authorization")
	if !strings.HasPrefix(authorization, bearerPrefix) {
		return nil, invalidAccessTokenError()
	}
	accessToken := strings.TrimSpace(strings.TrimPrefix(authorization, bearerPrefix))
	if accessToken == "" {
		return nil, invalidAccessTokenError()
	}

	req := new(authenticatorv1.VerifyAccessTokenRequest)
	req.SetAccessToken(accessToken)
	resp, err := client.VerifyAccessToken(ctx, req)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	if !resp.GetOk() || resp.GetUserId() <= 0 {
		return nil, invalidAccessTokenError()
	}
	return resp, nil
}

func invalidAccessTokenError() error {
	return apierror.FromRPC(rpcerror.New(
		codes.Unauthenticated,
		rpcerror.AuthenticatorDomain,
		rpcerror.AuthenticatorInvalidAccessToken,
		"invalid access token",
	))
}
