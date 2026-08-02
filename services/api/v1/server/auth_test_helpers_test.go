package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

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

func registerResponse() *authenticatorv1.RegisterResponse {
	resp := new(authenticatorv1.RegisterResponse)
	resp.SetOk(true)
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

type originRoundTripper struct {
	base   http.RoundTripper
	origin string
}

func (t originRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Origin", t.origin)
	return t.base.RoundTrip(clone)
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
