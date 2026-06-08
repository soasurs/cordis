package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/api/v1/svc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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
		Name:     proto.String("display name"),
		Email:    proto.String("user@example.com"),
		Password: proto.String("password"),
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if internalClient.registerRequest.GetName() != "display name" ||
		internalClient.registerRequest.GetEmail() != "user@example.com" ||
		internalClient.registerRequest.GetPassword() != "password" {
		t.Fatalf("unexpected internal request: %v", internalClient.registerRequest)
	}
	if internalClient.registerRequest.GetUserAgent() != "cordis-test-client" {
		t.Fatalf("unexpected user agent: %q", internalClient.registerRequest.GetUserAgent())
	}
	if internalClient.registerRequest.GetIp() == "" {
		t.Fatal("expected client ip")
	}

	result := resp.GetResult()
	if !result.GetOk() || result.GetUserId() != 1001 || result.GetSessionId() != 2001 {
		t.Fatalf("unexpected result: %v", result)
	}
	if result.GetAccessToken() != "access-token" || result.GetRefreshToken() != "refresh-token" {
		t.Fatalf("unexpected tokens: %v", result)
	}
}

func TestLoginMapsRequestAndResponse(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		loginResponse: loginResponse(authenticationResult()),
	}
	server := NewAuthenticator(&svc.ServiceContext{
		AuthenticatorClient: internalClient,
	})

	resp, err := server.Login(context.Background(), &apiv1.LoginRequest{
		Email:    proto.String("user@example.com"),
		Password: proto.String("password"),
	})
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if internalClient.loginRequest.GetEmail() != "user@example.com" ||
		internalClient.loginRequest.GetPassword() != "password" {
		t.Fatalf("unexpected internal request: %v", internalClient.loginRequest)
	}
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
		RefreshToken: proto.String("refresh-token"),
	})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if internalClient.refreshRequest.GetRefreshToken() != "refresh-token" {
		t.Fatalf("unexpected internal request: %v", internalClient.refreshRequest)
	}
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
		RefreshToken: proto.String("refresh-token"),
	})
	if err != nil {
		t.Fatalf("Logout returned error: %v", err)
	}
	if internalClient.logoutRequest.GetRefreshToken() != "refresh-token" {
		t.Fatalf("unexpected internal request: %v", internalClient.logoutRequest)
	}
	if !resp.GetOk() {
		t.Fatalf("unexpected response: %v", resp)
	}
}

func TestLoginFailure(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		loginError: rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidCredentials, "invalid credentials"),
	}
	server := NewAuthenticator(&svc.ServiceContext{
		AuthenticatorClient: internalClient,
	})

	_, err := server.Login(context.Background(), &apiv1.LoginRequest{
		Email:    proto.String("user@example.com"),
		Password: proto.String("wrong-password"),
	})
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("Login error code = %v, want %v: %v", connect.CodeOf(err), connect.CodeUnauthenticated, err)
	}

	publicInfo := publicErrorInfo(t, err)
	if publicInfo.GetCode() != apierror.CodeInvalidCredentials {
		t.Fatalf("public error code = %q, want %q", publicInfo.GetCode(), apierror.CodeInvalidCredentials)
	}
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
				Email:    proto.String("user@example.com"),
				Password: proto.String("password"),
			})
			if connect.CodeOf(err) != tt.connectCode {
				t.Fatalf("connect code = %v, want %v: %v", connect.CodeOf(err), tt.connectCode, err)
			}

			publicInfo := publicErrorInfo(t, err)
			if publicInfo.GetCode() != tt.publicCode {
				t.Fatalf("public error code = %q, want %q", publicInfo.GetCode(), tt.publicCode)
			}
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

	if !result.GetOk() ||
		result.GetUserId() != 1001 ||
		result.GetSessionId() != 2001 ||
		result.GetAccessToken() != "access-token" ||
		result.GetAccessTokenExpiresAt() != 3001 ||
		result.GetRefreshToken() != "refresh-token" ||
		result.GetRefreshTokenExpiresAt() != 4001 ||
		result.GetSessionExpiresAt() != 5001 {
		t.Fatalf("unexpected result: %v", result)
	}
}

func publicErrorInfo(t *testing.T, err error) *apiv1.PublicErrorInfo {
	t.Helper()

	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error: %v", err)
	}
	for _, detail := range connectErr.Details() {
		value, err := detail.Value()
		if err != nil {
			t.Fatalf("decode error detail: %v", err)
		}
		publicInfo, ok := value.(*apiv1.PublicErrorInfo)
		if ok {
			return publicInfo
		}
	}
	t.Fatal("missing public error info detail")
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
			if actual := clientIP(address); actual != expected {
				t.Fatalf("clientIP(%q) = %q, want %q", address, actual, expected)
			}
		})
	}
}
