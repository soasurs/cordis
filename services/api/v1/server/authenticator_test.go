package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/services/api/v1/svc"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type fakeAuthenticatorClient struct {
	authenticatorv1.AuthenticatorServiceClient
	registerRequest *authenticatorv1.RegisterRequest
	loginResponse   *authenticatorv1.LoginResponse
}

func (f *fakeAuthenticatorClient) Register(_ context.Context, req *authenticatorv1.RegisterRequest, _ ...grpc.CallOption) (*authenticatorv1.RegisterResponse, error) {
	f.registerRequest = req

	result := new(authenticatorv1.AuthenticationResult)
	result.SetOk(true)
	result.SetUserId(1001)
	result.SetSessionId(2001)
	result.SetAccessToken("access-token")
	result.SetAccessTokenExpiresAt(3001)
	result.SetRefreshToken("refresh-token")
	result.SetRefreshTokenExpiresAt(4001)
	result.SetSessionExpiresAt(5001)

	resp := new(authenticatorv1.RegisterResponse)
	resp.SetResult(result)
	return resp, nil
}

func (f *fakeAuthenticatorClient) Login(_ context.Context, _ *authenticatorv1.LoginRequest, _ ...grpc.CallOption) (*authenticatorv1.LoginResponse, error) {
	return f.loginResponse, nil
}

func TestRegister(t *testing.T) {
	internalClient := new(fakeAuthenticatorClient)
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

func TestLoginFailure(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		loginResponse: new(authenticatorv1.LoginResponse),
	}
	server := NewAuthenticator(&svc.ServiceContext{
		AuthenticatorClient: internalClient,
	})

	resp, err := server.Login(context.Background(), &apiv1.LoginRequest{
		Email:    proto.String("user@example.com"),
		Password: proto.String("wrong-password"),
	})
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if resp.GetResult() == nil || resp.GetResult().GetOk() {
		t.Fatalf("expected failed authentication result: %v", resp.GetResult())
	}
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
