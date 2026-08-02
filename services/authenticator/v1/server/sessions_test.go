package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
)

func TestListSessions(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions[2001] = &model.Session{
		SessionID: 2001,
		UserID:    1001,
		UserAgent: "agent",
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.ListSessionsRequest)
	req.SetUserId(1001)
	resp, err := server.ListSessions(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetSessions(), 1)
	require.Equal(t, int64(2001), resp.GetSessions()[0].GetSessionId())
}

func TestRevokeUserSessionChecksOwner(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions[2001] = &model.Session{
		SessionID: 2001,
		UserID:    1002,
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.RevokeUserSessionRequest)
	req.SetUserId(1001)
	req.SetSessionId(2001)
	_, err := server.RevokeUserSession(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestRevokeOtherSessionsKeepsCurrent(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions[2001] = &model.Session{SessionID: 2001, UserID: 1001}
	store.sessions[2002] = &model.Session{SessionID: 2002, UserID: 1001}
	store.sessions[2003] = &model.Session{SessionID: 2003, UserID: 1002}
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.RevokeOtherSessionsRequest)
	req.SetUserId(1001)
	req.SetCurrentSessionId(2001)
	resp, err := server.RevokeOtherSessions(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetRevoked())
	require.Zero(t, store.sessions[2001].RevokedAt)
	require.NotZero(t, store.sessions[2002].RevokedAt)
	require.Zero(t, store.sessions[2003].RevokedAt)
}
