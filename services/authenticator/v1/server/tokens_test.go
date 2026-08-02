package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
)

func TestRefresh(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	resp, err := server.Refresh(context.Background(), req)
	require.NoError(t, err)
	result := resp.GetResult()
	require.True(t, result.GetOk())
	require.Equal(t, int64(1001), result.GetUserId())
	require.Equal(t, int64(2001), result.GetSessionId())
	require.NotEqual(t, session.refreshToken.Raw, result.GetRefreshToken())
	require.Equal(t, token.Hash(session.refreshToken.Raw), store.rotatedOldHash)
	require.Equal(t, token.Hash(result.GetRefreshToken()), store.rotatedNewHash)
}

func TestRefreshRetryReturnsSameRefreshToken(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	session.session.AbsoluteExpiresAt = time.Now().Add(24 * time.Hour).UnixMilli()
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))
	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	first, err := server.Refresh(t.Context(), req)
	require.NoError(t, err)
	second, err := server.Refresh(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, first.GetResult().GetRefreshToken(), second.GetResult().GetRefreshToken())
}

func TestRefreshRetryRejectsRevokedSession(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	session.session.AbsoluteExpiresAt = time.Now().Add(24 * time.Hour).UnixMilli()
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient)).(*authenticatorServer)
	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	_, err := server.Refresh(t.Context(), req)
	require.NoError(t, err)
	session.session.RevokedAt = time.Now().UnixMilli()
	_, err = server.replayRefreshToken(t.Context(), session.session, token.Hash(session.refreshToken.Raw), time.Now())
	require.Error(t, err)
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorSessionRevoked))
}

func TestRefreshExtendsIdleExpiryWithinAbsoluteExpiry(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Minute).UnixMilli())
	absoluteExpiresAt := time.Now().Add(24 * time.Hour).UnixMilli()
	session.session.AbsoluteExpiresAt = absoluteExpiresAt
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))
	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	resp, err := server.Refresh(t.Context(), req)
	require.NoError(t, err)
	require.Greater(t, resp.GetResult().GetSessionExpiresAt(), time.Now().Add(30*time.Minute).UnixMilli())
	require.LessOrEqual(t, resp.GetResult().GetSessionExpiresAt(), absoluteExpiresAt)
	require.Equal(t, absoluteExpiresAt, resp.GetResult().GetAbsoluteSessionExpiresAt())
}

func TestRefreshRejectsPreviousTokenOutsideGrace(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	session.session.AbsoluteExpiresAt = time.Now().Add(24 * time.Hour).UnixMilli()
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))
	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	_, err := server.Refresh(t.Context(), req)
	require.NoError(t, err)
	session.session.PreviousRefreshTokenValidUntil = time.Now().Add(-time.Second).UnixMilli()
	_, err = server.Refresh(t.Context(), req)
	require.Error(t, err)
	require.NotZero(t, session.session.RevokedAt)
}

func TestAuthenticateCookieRotatesExpiredAccessToken(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	session.session.AbsoluteExpiresAt = time.Now().Add(24 * time.Hour).UnixMilli()
	expiredAccess, err := tokens.IssueAccessToken(1001, 2001, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))
	req := new(authenticatorv1.AuthenticateCookieRequest)
	req.SetAccessToken(expiredAccess.Raw)
	req.SetRefreshToken(session.refreshToken.Raw)

	resp, err := server.AuthenticateCookie(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), resp.GetUserId())
	require.NotNil(t, resp.GetRotated())
	require.NotEqual(t, session.refreshToken.Raw, resp.GetRotated().GetRefreshToken())
}

func TestGatewayTicketIsSingleUse(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	tickets := newFakeGatewayTicketStore()
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient)).(*authenticatorServer)
	server.svcCtx.GatewayTickets = tickets
	server.svcCtx.Cfg.GatewayTickets.TTL = 30 * time.Second
	createReq := new(authenticatorv1.CreateGatewayTicketRequest)
	createReq.SetUserId(1001)
	createReq.SetSessionId(2001)
	createReq.SetAccessTokenExpiresAt(time.Now().Add(15 * time.Minute).UnixMilli())

	created, err := server.CreateGatewayTicket(t.Context(), createReq)
	require.NoError(t, err)
	redeemReq := new(authenticatorv1.RedeemGatewayTicketRequest)
	redeemReq.SetGatewayTicket(created.GetGatewayTicket())
	redeemed, err := server.RedeemGatewayTicket(t.Context(), redeemReq)
	require.NoError(t, err)
	require.Equal(t, session.session.SessionID, redeemed.GetSessionId())
	_, err = server.RedeemGatewayTicket(t.Context(), redeemReq)
	require.Error(t, err)
}

func TestRefreshInvalidToken(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken("invalid-token")

	_, err := server.Refresh(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRefreshToken))
}

func TestRefreshHashMismatch(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	session.session.RefreshTokenHash = token.Hash("other-token")
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	_, err := server.Refresh(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRefreshToken))
}

func TestRefreshExpiredSession(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	session.session.ExpiresAt = time.Now().Add(-time.Hour).UnixMilli()
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	_, err := server.Refresh(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorSessionExpired))
}

func TestRefreshRevokedSession(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	session.session.RevokedAt = time.Now().UnixMilli()
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	_, err := server.Refresh(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorSessionRevoked))
}

func TestLogout(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.LogoutRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	resp, err := server.Logout(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, int64(2001), store.revokedSessionID)
}

func TestLogoutInvalidToken(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.LogoutRequest)
	req.SetRefreshToken("invalid-token")

	_, err := server.Logout(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRefreshToken))
}

func TestVerifyAccessToken(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	sessionExpiresAt := time.Now().Add(time.Hour).UnixMilli()
	accessToken, err := tokens.IssueAccessToken(1001, 2001, time.Now())
	require.NoError(t, err)
	store.sessions[2001] = &model.Session{
		SessionID: 2001,
		UserID:    1001,
		ExpiresAt: sessionExpiresAt,
	}
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.VerifyAccessTokenRequest)
	req.SetAccessToken(accessToken.Raw)

	resp, err := server.VerifyAccessToken(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, int64(1001), resp.GetUserId())
	require.Equal(t, int64(2001), resp.GetSessionId())
}

func TestVerifyAccessTokenInvalidToken(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.VerifyAccessTokenRequest)
	req.SetAccessToken("invalid-token")

	_, err := server.VerifyAccessToken(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidAccessToken))
}
