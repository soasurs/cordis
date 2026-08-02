package server

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/services/api/v1/config"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

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

func TestCookieLoginStoresTokensOutsideResponseBody(t *testing.T) {
	result := authenticationResult()
	result.SetAccessTokenExpiresAt(time.Now().Add(15 * time.Minute).UnixMilli())
	result.SetRefreshTokenExpiresAt(time.Now().Add(time.Hour).UnixMilli())
	internalClient := &fakeAuthenticatorClient{loginResponse: loginResponse(result)}
	cfg := config.BrowserAuthConfig{AllowedOrigins: []string{"https://app.example.com"}}
	path, handler := apiv1connect.NewAuthenticatorServiceHandler(NewAuthenticator(&svc.ServiceContext{
		Cfg: config.Config{BrowserAuth: cfg}, AuthenticatorClient: internalClient,
	}))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	httpClient := &http.Client{Jar: jar, Transport: originRoundTripper{base: http.DefaultTransport, origin: "https://app.example.com"}}
	client := apiv1connect.NewAuthenticatorServiceClient(httpClient, httpServer.URL)
	req := new(apiv1.LoginRequest)
	req.SetEmail("user@example.com")
	req.SetPassword("password")
	req.SetTokenTransport(apiv1.TokenTransport_TOKEN_TRANSPORT_COOKIE)

	resp, err := client.Login(t.Context(), req)
	require.NoError(t, err)
	require.Empty(t, resp.GetResult().GetAccessToken())
	require.Empty(t, resp.GetResult().GetRefreshToken())
	serverURL, err := url.Parse(httpServer.URL)
	require.NoError(t, err)
	cookies := jar.Cookies(serverURL)
	require.Len(t, cookies, 2)
	require.ElementsMatch(t, []string{"cordis_access", "cordis_refresh"}, []string{cookies[0].Name, cookies[1].Name})
}

func TestCookieAuthenticationTransparentlyRotatesTokens(t *testing.T) {
	rotated := authenticationResult()
	rotated.SetAccessTokenExpiresAt(time.Now().Add(15 * time.Minute).UnixMilli())
	rotated.SetRefreshTokenExpiresAt(time.Now().Add(time.Hour).UnixMilli())
	authenticated := new(authenticatorv1.AuthenticateCookieResponse)
	authenticated.SetOk(true)
	authenticated.SetUserId(1001)
	authenticated.SetSessionId(2001)
	authenticated.SetExpiresAt(rotated.GetAccessTokenExpiresAt())
	authenticated.SetRotated(rotated)
	internalClient := &fakeAuthenticatorClient{
		authenticateCookieResponse: authenticated,
		listSessionsResponse:       new(authenticatorv1.ListSessionsResponse),
	}
	cfg := config.BrowserAuthConfig{AllowedOrigins: []string{"https://app.example.com"}}
	configuredClient := svc.NewBrowserAuthenticatorClient(internalClient, cfg)
	path, handler := apiv1connect.NewAuthenticatorServiceHandler(NewAuthenticator(&svc.ServiceContext{
		Cfg: config.Config{BrowserAuth: cfg}, AuthenticatorClient: configuredClient,
	}))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	serverURL, err := url.Parse(httpServer.URL)
	require.NoError(t, err)
	jar.SetCookies(serverURL, []*http.Cookie{{Name: "cordis_refresh", Value: "old-refresh", Path: "/"}})
	httpClient := &http.Client{Jar: jar, Transport: originRoundTripper{base: http.DefaultTransport, origin: "https://app.example.com"}}
	client := apiv1connect.NewAuthenticatorServiceClient(httpClient, httpServer.URL)

	_, err = client.ListSessions(t.Context(), new(apiv1.ListSessionsRequest))
	require.NoError(t, err)
	require.Equal(t, "old-refresh", internalClient.authenticateCookieRequest.GetRefreshToken())
	require.Equal(t, int64(1001), internalClient.listSessionsRequest.GetUserId())
	cookies := jar.Cookies(serverURL)
	values := make(map[string]string, len(cookies))
	for _, cookie := range cookies {
		values[cookie.Name] = cookie.Value
	}
	require.Equal(t, "access-token", values["cordis_access"])
	require.Equal(t, "refresh-token", values["cordis_refresh"])
}

func TestCookieRotationSurvivesBusinessErrorResponse(t *testing.T) {
	rotated := authenticationResult()
	rotated.SetAccessTokenExpiresAt(time.Now().Add(15 * time.Minute).UnixMilli())
	rotated.SetRefreshTokenExpiresAt(time.Now().Add(time.Hour).UnixMilli())
	authenticated := new(authenticatorv1.AuthenticateCookieResponse)
	authenticated.SetOk(true)
	authenticated.SetUserId(1001)
	authenticated.SetSessionId(2001)
	authenticated.SetExpiresAt(rotated.GetAccessTokenExpiresAt())
	authenticated.SetRotated(rotated)
	internalClient := &fakeAuthenticatorClient{
		authenticateCookieResponse: authenticated,
		listSessionsError:          status.Error(codes.Unavailable, "unavailable"),
	}
	cfg := config.BrowserAuthConfig{AllowedOrigins: []string{"https://app.example.com"}}
	configuredClient := svc.NewBrowserAuthenticatorClient(internalClient, cfg)
	path, handler := apiv1connect.NewAuthenticatorServiceHandler(NewAuthenticator(&svc.ServiceContext{
		Cfg: config.Config{BrowserAuth: cfg}, AuthenticatorClient: configuredClient,
	}))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	serverURL, err := url.Parse(httpServer.URL)
	require.NoError(t, err)
	jar.SetCookies(serverURL, []*http.Cookie{{Name: "cordis_refresh", Value: "old-refresh", Path: "/"}})
	client := apiv1connect.NewAuthenticatorServiceClient(&http.Client{
		Jar: jar, Transport: originRoundTripper{base: http.DefaultTransport, origin: "https://app.example.com"},
	}, httpServer.URL)

	_, err = client.ListSessions(t.Context(), new(apiv1.ListSessionsRequest))
	require.Error(t, err)
	values := make(map[string]string)
	for _, cookie := range jar.Cookies(serverURL) {
		values[cookie.Name] = cookie.Value
	}
	require.Equal(t, "access-token", values["cordis_access"])
	require.Equal(t, "refresh-token", values["cordis_refresh"])
}
