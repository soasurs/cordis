package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/api/v1/svc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeAuthenticatorClient struct {
	authenticatorv1.AuthenticatorServiceClient
	registerRequest  *authenticatorv1.RegisterRequest
	registerResponse *authenticatorv1.RegisterResponse
	registerError    error
	loginRequest     *authenticatorv1.LoginRequest
	loginResponse    *authenticatorv1.LoginResponse
	loginError       error
	refreshRequest   *authenticatorv1.RefreshRequest
	refreshResponse  *authenticatorv1.RefreshResponse
	refreshError     error
	logoutRequest    *authenticatorv1.LogoutRequest
	logoutResponse   *authenticatorv1.LogoutResponse
	logoutError      error
}

func (f *fakeAuthenticatorClient) Register(_ context.Context, req *authenticatorv1.RegisterRequest, _ ...grpc.CallOption) (*authenticatorv1.RegisterResponse, error) {
	f.registerRequest = req
	if f.registerError != nil {
		return nil, f.registerError
	}
	return f.registerResponse, nil
}

func (f *fakeAuthenticatorClient) Login(_ context.Context, req *authenticatorv1.LoginRequest, _ ...grpc.CallOption) (*authenticatorv1.LoginResponse, error) {
	f.loginRequest = req
	if f.loginError != nil {
		return nil, f.loginError
	}
	return f.loginResponse, nil
}

func (f *fakeAuthenticatorClient) Refresh(_ context.Context, req *authenticatorv1.RefreshRequest, _ ...grpc.CallOption) (*authenticatorv1.RefreshResponse, error) {
	f.refreshRequest = req
	if f.refreshError != nil {
		return nil, f.refreshError
	}
	return f.refreshResponse, nil
}

func (f *fakeAuthenticatorClient) Logout(_ context.Context, req *authenticatorv1.LogoutRequest, _ ...grpc.CallOption) (*authenticatorv1.LogoutResponse, error) {
	f.logoutRequest = req
	if f.logoutError != nil {
		return nil, f.logoutError
	}
	return f.logoutResponse, nil
}

func TestRegisterOverConnectHTTP(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		registerResponse: registerResponse(authenticationResult()),
	}
	svcCtx := &svc.ServiceContext{
		AuthenticatorClient: internalClient,
	}

	path, handler := apiv1connect.NewAuthenticatorServiceHandler(NewAuthenticator(svcCtx))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	httpClient := &http.Client{
		Transport: userAgentRoundTripper{
			base:      http.DefaultTransport,
			userAgent: "cordis-test-client",
		},
	}
	client := apiv1connect.NewAuthenticatorServiceClient(httpClient, httpServer.URL)

	resp, err := client.Register(context.Background(), &apiv1.RegisterRequest{
		Name:     new("display name"),
		Email:    new("user@example.com"),
		Password: new("password"),
	})
	require.NoError(t, err)

	require.Equal(t, "display name", internalClient.registerRequest.GetName())
	require.Equal(t, "user@example.com", internalClient.registerRequest.GetEmail())
	require.Equal(t, "password", internalClient.registerRequest.GetPassword())
	require.Equal(t, "cordis-test-client", internalClient.registerRequest.GetUserAgent())
	require.NotEmpty(t, internalClient.registerRequest.GetIp())

	result := resp.GetResult()
	require.True(t, result.GetOk())
	require.Equal(t, int64(1001), result.GetUserId())
	require.Equal(t, int64(2001), result.GetSessionId())
	require.Equal(t, "access-token", result.GetAccessToken())
	require.Equal(t, "refresh-token", result.GetRefreshToken())
}

func TestLoginMapsRequestAndResponse(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		loginResponse: loginResponse(authenticationResult()),
	}
	server := NewAuthenticator(&svc.ServiceContext{
		AuthenticatorClient: internalClient,
	})

	resp, err := server.Login(context.Background(), &apiv1.LoginRequest{
		Email:    new("user@example.com"),
		Password: new("password"),
	})
	require.NoError(t, err)
	require.Equal(t, "user@example.com", internalClient.loginRequest.GetEmail())
	require.Equal(t, "password", internalClient.loginRequest.GetPassword())
	assertAPIAuthenticationResult(t, resp.GetResult())
}

func TestRefreshMapsRequestAndResponse(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		refreshResponse: refreshResponse(authenticationResult()),
	}
	server := NewAuthenticator(&svc.ServiceContext{
		AuthenticatorClient: internalClient,
	})

	resp, err := server.Refresh(context.Background(), &apiv1.RefreshRequest{
		RefreshToken: new("refresh-token"),
	})
	require.NoError(t, err)
	require.Equal(t, "refresh-token", internalClient.refreshRequest.GetRefreshToken())
	assertAPIAuthenticationResult(t, resp.GetResult())
}

func TestLogoutMapsRequestAndResponse(t *testing.T) {
	internalResp := new(authenticatorv1.LogoutResponse)
	internalResp.SetOk(true)

	internalClient := &fakeAuthenticatorClient{
		logoutResponse: internalResp,
	}
	server := NewAuthenticator(&svc.ServiceContext{
		AuthenticatorClient: internalClient,
	})

	resp, err := server.Logout(context.Background(), &apiv1.LogoutRequest{
		RefreshToken: new("refresh-token"),
	})
	require.NoError(t, err)
	require.Equal(t, "refresh-token", internalClient.logoutRequest.GetRefreshToken())
	require.True(t, resp.GetOk())
}

func TestLoginFailure(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		loginError: rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidCredentials, "invalid credentials"),
	}
	server := NewAuthenticator(&svc.ServiceContext{
		AuthenticatorClient: internalClient,
	})

	_, err := server.Login(context.Background(), &apiv1.LoginRequest{
		Email:    new("user@example.com"),
		Password: new("wrong-password"),
	})
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))

	publicInfo := publicErrorInfo(t, err)
	require.Equal(t, apierror.CodeInvalidCredentials, publicInfo.GetCode())
}

func TestErrorMappings(t *testing.T) {
	tests := map[string]struct {
		err         error
		connectCode connect.Code
		publicCode  string
	}{
		"email already exists": {
			err:         rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists, "email already exists"),
			connectCode: connect.CodeAlreadyExists,
			publicCode:  apierror.CodeEmailAlreadyExists,
		},
		"invalid argument": {
			err:         status.Error(codes.InvalidArgument, "email is required"),
			connectCode: connect.CodeInvalidArgument,
			publicCode:  apierror.CodeInvalidArgument,
		},
		"unknown reason": {
			err:         rpcerror.New(codes.NotFound, "unknown.cordis", "unknown_reason", "unknown reason"),
			connectCode: connect.CodeInternal,
			publicCode:  apierror.CodeInternal,
		},
		"invalid refresh token": {
			err:         rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRefreshToken, "invalid refresh token"),
			connectCode: connect.CodeUnauthenticated,
			publicCode:  apierror.CodeInvalidRefreshToken,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			internalClient := &fakeAuthenticatorClient{
				loginError: tt.err,
			}
			server := NewAuthenticator(&svc.ServiceContext{
				AuthenticatorClient: internalClient,
			})

			_, err := server.Login(context.Background(), &apiv1.LoginRequest{
				Email:    new("user@example.com"),
				Password: new("password"),
			})
			require.Equal(t, tt.connectCode, connect.CodeOf(err))

			publicInfo := publicErrorInfo(t, err)
			require.Equal(t, tt.publicCode, publicInfo.GetCode())
		})
	}
}

func authenticationResult() *authenticatorv1.AuthenticationResult {
	result := new(authenticatorv1.AuthenticationResult)
	result.SetOk(true)
	result.SetUserId(1001)
	result.SetSessionId(2001)
	result.SetAccessToken("access-token")
	result.SetAccessTokenExpiresAt(3001)
	result.SetRefreshToken("refresh-token")
	result.SetRefreshTokenExpiresAt(4001)
	result.SetSessionExpiresAt(5001)
	return result
}

func registerResponse(result *authenticatorv1.AuthenticationResult) *authenticatorv1.RegisterResponse {
	resp := new(authenticatorv1.RegisterResponse)
	resp.SetResult(result)
	return resp
}

func loginResponse(result *authenticatorv1.AuthenticationResult) *authenticatorv1.LoginResponse {
	resp := new(authenticatorv1.LoginResponse)
	resp.SetResult(result)
	return resp
}

func refreshResponse(result *authenticatorv1.AuthenticationResult) *authenticatorv1.RefreshResponse {
	resp := new(authenticatorv1.RefreshResponse)
	resp.SetResult(result)
	return resp
}

func assertAPIAuthenticationResult(t *testing.T, result *apiv1.AuthenticationResult) {
	t.Helper()

	require.True(t, result.GetOk())
	require.Equal(t, int64(1001), result.GetUserId())
	require.Equal(t, int64(2001), result.GetSessionId())
	require.Equal(t, "access-token", result.GetAccessToken())
	require.Equal(t, int64(3001), result.GetAccessTokenExpiresAt())
	require.Equal(t, "refresh-token", result.GetRefreshToken())
	require.Equal(t, int64(4001), result.GetRefreshTokenExpiresAt())
	require.Equal(t, int64(5001), result.GetSessionExpiresAt())
}

func publicErrorInfo(t *testing.T, err error) *apiv1.PublicErrorInfo {
	t.Helper()

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	for _, detail := range connectErr.Details() {
		value, err := detail.Value()
		require.NoError(t, err)
		publicInfo, ok := value.(*apiv1.PublicErrorInfo)
		if ok {
			return publicInfo
		}
	}
	require.Fail(t, "missing public error info detail")
	return nil
}

type userAgentRoundTripper struct {
	base      http.RoundTripper
	userAgent string
}

func (r userAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	cloned.Header.Set("User-Agent", r.userAgent)
	return r.base.RoundTrip(cloned)
}

func TestClientIP(t *testing.T) {
	tests := map[string]string{
		"127.0.0.1:8080": "127.0.0.1",
		"[::1]:8080":     "::1",
		"client":         "client",
	}

	for address, expected := range tests {
		t.Run(strings.ReplaceAll(address, ":", "_"), func(t *testing.T) {
			require.Equal(t, expected, clientIP(address))
		})
	}
}
