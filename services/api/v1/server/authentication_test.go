package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

func TestRegisterOverConnectHTTP(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		registerResponse: registerResponse(),
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

	req := new(apiv1.RegisterRequest)
	req.SetName("display name")
	req.SetEmail("user@example.com")
	req.SetPassword("password")
	req.SetRegistrationInviteCode("invite-code")
	resp, err := client.Register(context.Background(), req)
	require.NoError(t, err)

	require.Equal(t, "display name", internalClient.registerRequest.GetName())
	require.Equal(t, "user@example.com", internalClient.registerRequest.GetEmail())
	require.Equal(t, "password", internalClient.registerRequest.GetPassword())
	require.Equal(t, "invite-code", internalClient.registerRequest.GetRegistrationInviteCode())

	require.True(t, resp.GetOk())
}

func TestLoginMapsRequestAndResponse(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		loginResponse: loginResponse(authenticationResult()),
	}
	server := NewAuthenticator(&svc.ServiceContext{
		AuthenticatorClient: internalClient,
	})

	req := new(apiv1.LoginRequest)
	req.SetEmail("user@example.com")
	req.SetPassword("password")
	resp, err := server.Login(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "user@example.com", internalClient.loginRequest.GetEmail())
	require.Equal(t, "password", internalClient.loginRequest.GetPassword())
	assertAPIAuthenticationResult(t, resp.GetResult())
}

func TestLoginMapsTwoFactorChallengeWithoutAuthenticationResult(t *testing.T) {
	challenge := new(authenticatorv1.TwoFactorLoginChallenge)
	challenge.SetToken("challenge-token")
	challenge.SetExpiresAt(3001)
	internalResp := new(authenticatorv1.LoginResponse)
	internalResp.SetTwoFactorChallenge(challenge)
	server := NewAuthenticator(&svc.ServiceContext{AuthenticatorClient: &fakeAuthenticatorClient{loginResponse: internalResp}})

	loginReq := new(apiv1.LoginRequest)
	loginReq.SetEmail("user@example.com")
	loginReq.SetPassword("password")
	resp, err := server.Login(context.Background(), loginReq)
	require.NoError(t, err)
	require.Nil(t, resp.GetResult())
	require.Equal(t, "challenge-token", resp.GetTwoFactorChallenge().GetToken())
	require.Equal(t, int64(3001), resp.GetTwoFactorChallenge().GetExpiresAt())
}

func TestRefreshMapsRequestAndResponse(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		refreshResponse: refreshResponse(authenticationResult()),
	}
	server := NewAuthenticator(&svc.ServiceContext{
		AuthenticatorClient: internalClient,
	})

	refreshReq := new(apiv1.RefreshRequest)
	refreshReq.SetRefreshToken("refresh-token")
	resp, err := server.Refresh(context.Background(), refreshReq)
	require.NoError(t, err)
	require.Equal(t, "refresh-token", internalClient.refreshRequest.GetRefreshToken())
	assertAPIAuthenticationResult(t, resp.GetResult())
}

func TestLogoutMapsRequestAndResponse(t *testing.T) {
	svcResp := new(authenticatorv1.LogoutResponse)
	svcResp.SetOk(true)

	internalClient := &fakeAuthenticatorClient{
		logoutResponse: svcResp,
	}
	server := NewAuthenticator(&svc.ServiceContext{
		AuthenticatorClient: internalClient,
	})

	logoutReq := new(apiv1.LogoutRequest)
	logoutReq.SetRefreshToken("refresh-token")
	resp, err := server.Logout(context.Background(), logoutReq)
	require.NoError(t, err)
	require.Equal(t, "refresh-token", internalClient.logoutRequest.GetRefreshToken())
	require.True(t, resp.GetOk())
}

func TestListSessionsMarksCurrentSession(t *testing.T) {
	internalSession := new(authenticatorv1.Session)
	internalSession.SetSessionId(2001)
	internalSession.SetUserId(1001)
	internalSession.SetUserAgent("agent")
	internalSession.SetExpiresAt(3001)
	svcResp := new(authenticatorv1.ListSessionsResponse)
	svcResp.SetSessions([]*authenticatorv1.Session{internalSession})

	internalClient := &fakeAuthenticatorClient{
		verifyResponse:       verifyAccessTokenResponse(1001),
		listSessionsResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	resp, err := client.ListSessions(context.Background(), new(apiv1.ListSessionsRequest))
	require.NoError(t, err)
	require.Equal(t, int64(1001), internalClient.listSessionsRequest.GetUserId())
	require.Len(t, resp.GetSessions(), 1)
	require.True(t, resp.GetSessions()[0].GetCurrent())
}

func TestRevokeSessionUsesAuthenticatedUser(t *testing.T) {
	svcResp := new(authenticatorv1.RevokeUserSessionResponse)
	svcResp.SetOk(true)
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:            verifyAccessTokenResponse(1001),
		revokeUserSessionResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	revokeReq := new(apiv1.RevokeSessionRequest)
	revokeReq.SetSessionId(int64(2002))
	resp, err := client.RevokeSession(context.Background(), revokeReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), internalClient.revokeUserSessionRequest.GetUserId())
	require.Equal(t, int64(2002), internalClient.revokeUserSessionRequest.GetSessionId())
	require.True(t, resp.GetOk())
}

func TestLoginFailure(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		loginError: rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidCredentials, "invalid credentials"),
	}
	server := NewAuthenticator(&svc.ServiceContext{
		AuthenticatorClient: internalClient,
	})

	loginReq := new(apiv1.LoginRequest)
	loginReq.SetEmail("user@example.com")
	loginReq.SetPassword("wrong-password")
	_, err := server.Login(context.Background(), loginReq)
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

			loginReq := new(apiv1.LoginRequest)
			loginReq.SetEmail("user@example.com")
			loginReq.SetPassword("password")
			_, err := server.Login(context.Background(), loginReq)
			require.Equal(t, tt.connectCode, connect.CodeOf(err))

			publicInfo := publicErrorInfo(t, err)
			require.Equal(t, tt.publicCode, publicInfo.GetCode())
		})
	}
}
