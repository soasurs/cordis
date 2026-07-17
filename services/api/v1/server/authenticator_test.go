package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

type fakeAuthenticatorClient struct {
	authenticatorv1.AuthenticatorServiceClient
	registerRequest                *authenticatorv1.RegisterRequest
	registerResponse               *authenticatorv1.RegisterResponse
	registerError                  error
	loginRequest                   *authenticatorv1.LoginRequest
	loginResponse                  *authenticatorv1.LoginResponse
	loginError                     error
	refreshRequest                 *authenticatorv1.RefreshRequest
	refreshResponse                *authenticatorv1.RefreshResponse
	refreshError                   error
	logoutRequest                  *authenticatorv1.LogoutRequest
	logoutResponse                 *authenticatorv1.LogoutResponse
	logoutError                    error
	verifyRequest                  *authenticatorv1.VerifyAccessTokenRequest
	verifyResponse                 *authenticatorv1.VerifyAccessTokenResponse
	verifyError                    error
	listSessionsRequest            *authenticatorv1.ListSessionsRequest
	listSessionsResponse           *authenticatorv1.ListSessionsResponse
	listSessionsError              error
	revokeUserSessionRequest       *authenticatorv1.RevokeUserSessionRequest
	revokeUserSessionResponse      *authenticatorv1.RevokeUserSessionResponse
	revokeUserSessionError         error
	revokeOtherSessionsRequest     *authenticatorv1.RevokeOtherSessionsRequest
	revokeOtherSessionsResponse    *authenticatorv1.RevokeOtherSessionsResponse
	revokeOtherSessionsError       error
	completeTwoFactorLoginRequest  *authenticatorv1.CompleteTwoFactorLoginRequest
	completeTwoFactorLoginResponse *authenticatorv1.CompleteTwoFactorLoginResponse
	completeTwoFactorLoginError    error
	twoFactorStatusRequest         *authenticatorv1.GetTwoFactorStatusRequest
	twoFactorStatusResponse        *authenticatorv1.GetTwoFactorStatusResponse
	twoFactorStatusError           error
	beginEnrollmentRequest         *authenticatorv1.BeginTwoFactorEnrollmentRequest
	beginEnrollmentResponse        *authenticatorv1.BeginTwoFactorEnrollmentResponse
	beginEnrollmentError           error
	confirmEnrollmentRequest       *authenticatorv1.ConfirmTwoFactorEnrollmentRequest
	confirmEnrollmentResponse      *authenticatorv1.ConfirmTwoFactorEnrollmentResponse
	confirmEnrollmentError         error
	disableTwoFactorRequest        *authenticatorv1.DisableTwoFactorRequest
	disableTwoFactorResponse       *authenticatorv1.DisableTwoFactorResponse
	disableTwoFactorError          error
	regenRecoveryCodesRequest      *authenticatorv1.RegenerateTwoFactorRecoveryCodesRequest
	regenRecoveryCodesResponse     *authenticatorv1.RegenerateTwoFactorRecoveryCodesResponse
	regenRecoveryCodesError        error
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

func (f *fakeAuthenticatorClient) VerifyAccessToken(_ context.Context, req *authenticatorv1.VerifyAccessTokenRequest, _ ...grpc.CallOption) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	f.verifyRequest = req
	if f.verifyError != nil {
		return nil, f.verifyError
	}
	return f.verifyResponse, nil
}

func (f *fakeAuthenticatorClient) ListSessions(_ context.Context, req *authenticatorv1.ListSessionsRequest, _ ...grpc.CallOption) (*authenticatorv1.ListSessionsResponse, error) {
	f.listSessionsRequest = req
	return f.listSessionsResponse, f.listSessionsError
}

func (f *fakeAuthenticatorClient) RevokeUserSession(_ context.Context, req *authenticatorv1.RevokeUserSessionRequest, _ ...grpc.CallOption) (*authenticatorv1.RevokeUserSessionResponse, error) {
	f.revokeUserSessionRequest = req
	return f.revokeUserSessionResponse, f.revokeUserSessionError
}

func (f *fakeAuthenticatorClient) RevokeOtherSessions(_ context.Context, req *authenticatorv1.RevokeOtherSessionsRequest, _ ...grpc.CallOption) (*authenticatorv1.RevokeOtherSessionsResponse, error) {
	f.revokeOtherSessionsRequest = req
	return f.revokeOtherSessionsResponse, f.revokeOtherSessionsError
}

func (f *fakeAuthenticatorClient) CompleteTwoFactorLogin(_ context.Context, req *authenticatorv1.CompleteTwoFactorLoginRequest, _ ...grpc.CallOption) (*authenticatorv1.CompleteTwoFactorLoginResponse, error) {
	f.completeTwoFactorLoginRequest = req
	return f.completeTwoFactorLoginResponse, f.completeTwoFactorLoginError
}

func (f *fakeAuthenticatorClient) GetTwoFactorStatus(_ context.Context, req *authenticatorv1.GetTwoFactorStatusRequest, _ ...grpc.CallOption) (*authenticatorv1.GetTwoFactorStatusResponse, error) {
	f.twoFactorStatusRequest = req
	return f.twoFactorStatusResponse, f.twoFactorStatusError
}

func (f *fakeAuthenticatorClient) BeginTwoFactorEnrollment(_ context.Context, req *authenticatorv1.BeginTwoFactorEnrollmentRequest, _ ...grpc.CallOption) (*authenticatorv1.BeginTwoFactorEnrollmentResponse, error) {
	f.beginEnrollmentRequest = req
	return f.beginEnrollmentResponse, f.beginEnrollmentError
}

func (f *fakeAuthenticatorClient) ConfirmTwoFactorEnrollment(_ context.Context, req *authenticatorv1.ConfirmTwoFactorEnrollmentRequest, _ ...grpc.CallOption) (*authenticatorv1.ConfirmTwoFactorEnrollmentResponse, error) {
	f.confirmEnrollmentRequest = req
	return f.confirmEnrollmentResponse, f.confirmEnrollmentError
}

func (f *fakeAuthenticatorClient) DisableTwoFactor(_ context.Context, req *authenticatorv1.DisableTwoFactorRequest, _ ...grpc.CallOption) (*authenticatorv1.DisableTwoFactorResponse, error) {
	f.disableTwoFactorRequest = req
	return f.disableTwoFactorResponse, f.disableTwoFactorError
}

func (f *fakeAuthenticatorClient) RegenerateTwoFactorRecoveryCodes(_ context.Context, req *authenticatorv1.RegenerateTwoFactorRecoveryCodesRequest, _ ...grpc.CallOption) (*authenticatorv1.RegenerateTwoFactorRecoveryCodesResponse, error) {
	f.regenRecoveryCodesRequest = req
	return f.regenRecoveryCodesResponse, f.regenRecoveryCodesError
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

func TestLoginMapsTwoFactorChallengeWithoutAuthenticationResult(t *testing.T) {
	challenge := new(authenticatorv1.TwoFactorLoginChallenge)
	challenge.SetToken("challenge-token")
	challenge.SetExpiresAt(3001)
	internalResp := new(authenticatorv1.LoginResponse)
	internalResp.SetTwoFactorChallenge(challenge)
	server := NewAuthenticator(&svc.ServiceContext{AuthenticatorClient: &fakeAuthenticatorClient{loginResponse: internalResp}})

	resp, err := server.Login(context.Background(), &apiv1.LoginRequest{
		Email:    new("user@example.com"),
		Password: new("password"),
	})
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

	resp, err := server.Refresh(context.Background(), &apiv1.RefreshRequest{
		RefreshToken: new("refresh-token"),
	})
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

	resp, err := server.Logout(context.Background(), &apiv1.LogoutRequest{
		RefreshToken: new("refresh-token"),
	})
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

	resp, err := client.ListSessions(context.Background(), &apiv1.ListSessionsRequest{})
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

	resp, err := client.RevokeSession(context.Background(), &apiv1.RevokeSessionRequest{
		SessionId: new(int64(2002)),
	})
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

func completeTwoFactorLoginResponse(result *authenticatorv1.AuthenticationResult) *authenticatorv1.CompleteTwoFactorLoginResponse {
	resp := new(authenticatorv1.CompleteTwoFactorLoginResponse)
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

func TestCompleteTwoFactorLoginMapsRequestAndResponse(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		completeTwoFactorLoginResponse: completeTwoFactorLoginResponse(authenticationResult()),
	}
	server := NewAuthenticator(&svc.ServiceContext{AuthenticatorClient: internalClient})

	resp, err := server.CompleteTwoFactorLogin(context.Background(), &apiv1.CompleteTwoFactorLoginRequest{
		ChallengeToken: new("challenge-token"),
		Code:           new("123456"),
	})
	require.NoError(t, err)
	require.Equal(t, "challenge-token", internalClient.completeTwoFactorLoginRequest.GetChallengeToken())
	require.Equal(t, "123456", internalClient.completeTwoFactorLoginRequest.GetCode())
	assertAPIAuthenticationResult(t, resp.GetResult())
}

func TestGetTwoFactorStatus(t *testing.T) {
	svcResp := new(authenticatorv1.GetTwoFactorStatusResponse)
	svcResp.SetEnabled(true)
	svcResp.SetRecoveryCodesRemaining(8)
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:          verifyAccessTokenResponse(1001),
		twoFactorStatusResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	resp, err := client.GetTwoFactorStatus(context.Background(), &apiv1.GetTwoFactorStatusRequest{})
	require.NoError(t, err)
	require.Equal(t, int64(1001), internalClient.twoFactorStatusRequest.GetUserId())
	require.True(t, resp.GetEnabled())
	require.Equal(t, int32(8), resp.GetRecoveryCodesRemaining())
}

func TestBeginTwoFactorEnrollment(t *testing.T) {
	svcResp := new(authenticatorv1.BeginTwoFactorEnrollmentResponse)
	svcResp.SetEnrollmentToken("enroll-token")
	svcResp.SetOtpauthUri("otpauth://totp/...")
	svcResp.SetManualEntryKey("ABCDEFGHIJKLMNOP")
	svcResp.SetExpiresAt(3001)
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:          verifyAccessTokenResponse(1001),
		beginEnrollmentResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	resp, err := client.BeginTwoFactorEnrollment(context.Background(), &apiv1.BeginTwoFactorEnrollmentRequest{
		Password: new("password"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), internalClient.beginEnrollmentRequest.GetUserId())
	require.Equal(t, "password", internalClient.beginEnrollmentRequest.GetPassword())
	require.Equal(t, "enroll-token", resp.GetEnrollmentToken())
	require.Equal(t, "otpauth://totp/...", resp.GetOtpauthUri())
	require.Equal(t, "ABCDEFGHIJKLMNOP", resp.GetManualEntryKey())
	require.Equal(t, int64(3001), resp.GetExpiresAt())
}

func TestConfirmTwoFactorEnrollment(t *testing.T) {
	svcResp := new(authenticatorv1.ConfirmTwoFactorEnrollmentResponse)
	svcResp.SetRecoveryCodes([]string{"code1", "code2"})
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:            verifyAccessTokenResponse(1001),
		confirmEnrollmentResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	resp, err := client.ConfirmTwoFactorEnrollment(context.Background(), &apiv1.ConfirmTwoFactorEnrollmentRequest{
		EnrollmentToken: new("enroll-token"),
		Code:            new("123456"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), internalClient.confirmEnrollmentRequest.GetUserId())
	require.Equal(t, int64(2001), internalClient.confirmEnrollmentRequest.GetCurrentSessionId())
	require.Equal(t, "enroll-token", internalClient.confirmEnrollmentRequest.GetEnrollmentToken())
	require.Equal(t, "123456", internalClient.confirmEnrollmentRequest.GetCode())
	require.Equal(t, []string{"code1", "code2"}, resp.GetRecoveryCodes())
}

func TestDisableTwoFactorWithCode(t *testing.T) {
	svcResp := new(authenticatorv1.DisableTwoFactorResponse)
	svcResp.SetOk(true)
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:           verifyAccessTokenResponse(1001),
		disableTwoFactorResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	resp, err := client.DisableTwoFactor(context.Background(), &apiv1.DisableTwoFactorRequest{
		Password:     new("password"),
		Verification: &apiv1.DisableTwoFactorRequest_Code{Code: "123456"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), internalClient.disableTwoFactorRequest.GetUserId())
	require.Equal(t, int64(2001), internalClient.disableTwoFactorRequest.GetCurrentSessionId())
	require.Equal(t, "password", internalClient.disableTwoFactorRequest.GetPassword())
	require.Equal(t, "123456", internalClient.disableTwoFactorRequest.GetCode())
	require.False(t, internalClient.disableTwoFactorRequest.HasRecoveryCode())
	require.True(t, resp.GetOk())
}

func TestDisableTwoFactorWithRecoveryCode(t *testing.T) {
	svcResp := new(authenticatorv1.DisableTwoFactorResponse)
	svcResp.SetOk(true)
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:           verifyAccessTokenResponse(1001),
		disableTwoFactorResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	resp, err := client.DisableTwoFactor(context.Background(), &apiv1.DisableTwoFactorRequest{
		Password:     new("password"),
		Verification: &apiv1.DisableTwoFactorRequest_RecoveryCode{RecoveryCode: "recovery-code"},
	})
	require.NoError(t, err)
	require.Equal(t, "recovery-code", internalClient.disableTwoFactorRequest.GetRecoveryCode())
	require.True(t, resp.GetOk())
}

func TestRegenerateTwoFactorRecoveryCodes(t *testing.T) {
	svcResp := new(authenticatorv1.RegenerateTwoFactorRecoveryCodesResponse)
	svcResp.SetRecoveryCodes([]string{"new1", "new2", "new3"})
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:             verifyAccessTokenResponse(1001),
		regenRecoveryCodesResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	resp, err := client.RegenerateTwoFactorRecoveryCodes(context.Background(), &apiv1.RegenerateTwoFactorRecoveryCodesRequest{
		Password: new("password"),
		Code:     new("123456"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), internalClient.regenRecoveryCodesRequest.GetUserId())
	require.Equal(t, int64(2001), internalClient.regenRecoveryCodesRequest.GetCurrentSessionId())
	require.Equal(t, "password", internalClient.regenRecoveryCodesRequest.GetPassword())
	require.Equal(t, "123456", internalClient.regenRecoveryCodesRequest.GetCode())
	require.Equal(t, []string{"new1", "new2", "new3"}, resp.GetRecoveryCodes())
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

func newAuthenticatorHTTPClient(
	t *testing.T,
	internalClient *fakeAuthenticatorClient,
	accessToken string,
) (apiv1connect.AuthenticatorServiceClient, func()) {
	t.Helper()

	path, handler := apiv1connect.NewAuthenticatorServiceHandler(NewAuthenticator(&svc.ServiceContext{
		AuthenticatorClient: internalClient,
	}))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)

	httpClient := &http.Client{Transport: bearerRoundTripper{
		base:        http.DefaultTransport,
		accessToken: accessToken,
	}}
	return apiv1connect.NewAuthenticatorServiceClient(httpClient, httpServer.URL), httpServer.Close
}
