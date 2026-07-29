package server

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/api/v1/config"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
)

const bearerPrefix = "Bearer "

func authenticate(
	ctx context.Context,
	client authenticatorv1.AuthenticatorServiceClient,
) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	return authenticateWithMinimumAccessTTL(ctx, client, 0)
}

// optionalAuthenticate verifies credentials when present and returns nil when
// the request is anonymous. Invalid credentials still fail.
func optionalAuthenticate(
	ctx context.Context,
	client authenticatorv1.AuthenticatorServiceClient,
) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	callInfo, ok := connect.CallInfoForHandlerContext(ctx)
	if !ok {
		return nil, nil
	}
	if callInfo.RequestHeader().Get("Authorization") != "" {
		return authenticate(ctx, client)
	}
	provider, ok := client.(interface {
		BrowserAuthConfig() config.BrowserAuthConfig
	})
	if !ok {
		return nil, nil
	}
	browserAuth := provider.BrowserAuthConfig()
	access := requestCookie(callInfo.RequestHeader(), browserAuth.EffectiveAccessCookieName())
	refresh := requestCookie(callInfo.RequestHeader(), browserAuth.EffectiveRefreshCookieName())
	if access == "" && refresh == "" {
		return nil, nil
	}
	return authenticate(ctx, client)
}

func authenticateWithMinimumAccessTTL(
	ctx context.Context,
	client authenticatorv1.AuthenticatorServiceClient,
	minimumAccessTTL time.Duration,
) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	callInfo, ok := connect.CallInfoForHandlerContext(ctx)
	if !ok {
		return nil, invalidAccessTokenError()
	}

	authorization := callInfo.RequestHeader().Get("Authorization")
	if authorization != "" {
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
		return finishAuthentication(ctx, resp, err)
	}
	provider, ok := client.(interface {
		BrowserAuthConfig() config.BrowserAuthConfig
	})
	if !ok {
		return nil, invalidAccessTokenError()
	}
	browserAuth := provider.BrowserAuthConfig()
	if !allowedBrowserOrigin(callInfo.RequestHeader().Get("Origin"), browserAuth.AllowedOrigins) {
		return nil, invalidAccessTokenError()
	}
	req := new(authenticatorv1.AuthenticateCookieRequest)
	req.SetAccessToken(requestCookie(callInfo.RequestHeader(), browserAuth.EffectiveAccessCookieName()))
	req.SetRefreshToken(requestCookie(callInfo.RequestHeader(), browserAuth.EffectiveRefreshCookieName()))
	req.SetMinimumAccessTtlMs(minimumAccessTTL.Milliseconds())
	cookieResp, err := client.AuthenticateCookie(ctx, req)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	if cookieResp.GetRotated() != nil {
		setAuthenticationCookies(callInfo.ResponseHeader(), browserAuth, cookieResp.GetRotated())
	}
	resp := new(authenticatorv1.VerifyAccessTokenResponse)
	resp.SetOk(cookieResp.GetOk())
	resp.SetUserId(cookieResp.GetUserId())
	resp.SetSessionId(cookieResp.GetSessionId())
	resp.SetExpiresAt(cookieResp.GetExpiresAt())
	return finishAuthentication(ctx, resp, nil)
}

func finishAuthentication(ctx context.Context, resp *authenticatorv1.VerifyAccessTokenResponse, err error) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	if !resp.GetOk() || resp.GetUserId() <= 0 {
		return nil, invalidAccessTokenError()
	}
	if err := apiratelimit.CheckAuthenticated(ctx, resp.GetUserId()); err != nil {
		return nil, err
	}
	return resp, nil
}

func requestCookie(header http.Header, name string) string {
	req := &http.Request{Header: header}
	cookie, err := req.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func allowedBrowserOrigin(origin string, allowed []string) bool {
	return slices.Contains(allowed, origin)
}

func setAuthenticationCookies(header http.Header, cfg config.BrowserAuthConfig, result *authenticatorv1.AuthenticationResult) {
	setTokenCookie(header, cfg.EffectiveAccessCookieName(), result.GetAccessToken(), result.GetAccessTokenExpiresAt(), cfg.Secure)
	setTokenCookie(header, cfg.EffectiveRefreshCookieName(), result.GetRefreshToken(), result.GetRefreshTokenExpiresAt(), cfg.Secure)
}

func clearAuthenticationCookies(header http.Header, cfg config.BrowserAuthConfig) {
	setTokenCookie(header, cfg.EffectiveAccessCookieName(), "", 0, cfg.Secure)
	setTokenCookie(header, cfg.EffectiveRefreshCookieName(), "", 0, cfg.Secure)
}

func setTokenCookie(header http.Header, name, value string, expiresAt int64, secure bool) {
	now := time.Now()
	expires := time.UnixMilli(expiresAt)
	maxAge := int(time.Until(expires).Seconds())
	if value == "" || expiresAt <= now.UnixMilli() {
		expires = time.Unix(1, 0)
		maxAge = -1
	}
	header.Add("Set-Cookie", (&http.Cookie{
		Name: name, Value: value, Path: "/", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: maxAge,
	}).String())
}

func invalidAccessTokenError() error {
	return apierror.FromRPC(rpcerror.New(
		codes.Unauthenticated,
		rpcerror.AuthenticatorDomain,
		rpcerror.AuthenticatorInvalidAccessToken,
		"invalid access token",
	))
}
