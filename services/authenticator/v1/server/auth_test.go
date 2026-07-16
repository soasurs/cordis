package server

import (
	"context"
	"database/sql"
	"testing"
	"time"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/authenticator/v1/config"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
	"github.com/soasurs/cordis/services/authenticator/v1/svc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRegister(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	userClient := &fakeUserClient{
		createUserResponse: createUserResponse(1001, "user@example.com"),
	}
	server := newTestAuthenticatorServer(t, store, tokens, userClient)

	req := new(authenticatorv1.RegisterRequest)
	req.SetName("display name")
	req.SetEmail("user@example.com")
	req.SetPassword("password")
	req.SetUserAgent("test-agent")
	req.SetIp("127.0.0.1")

	resp, err := server.Register(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "display name", userClient.createUserRequest.GetName())
	require.Equal(t, "user@example.com", userClient.createUserRequest.GetEmail())
	require.Equal(t, "password", userClient.createUserRequest.GetPassword())
	result := resp.GetResult()
	require.True(t, result.GetOk())
	require.Equal(t, int64(1001), result.GetUserId())
	require.NotEmpty(t, result.GetAccessToken())
	require.NotEmpty(t, result.GetRefreshToken())
	require.NotNil(t, store.createdSession)
	require.Equal(t, "test-agent", store.createdSession.UserAgent)
	require.Equal(t, "127.0.0.1", store.createdSession.IP)
}

func TestRegisterUserError(t *testing.T) {
	expectedErr := rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists, "email already exists")
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), &fakeUserClient{
		createUserErr: expectedErr,
	})

	req := new(authenticatorv1.RegisterRequest)
	req.SetName("display name")
	req.SetEmail("user@example.com")
	req.SetPassword("password")

	_, err := server.Register(context.Background(), req)
	require.ErrorIs(t, err, expectedErr)
}

func TestLogin(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	server := newTestAuthenticatorServer(t, store, tokens, &fakeUserClient{
		verifyPasswordResponse: verifyPasswordResponse(true, 1001),
	})

	req := new(authenticatorv1.LoginRequest)
	req.SetEmail("user@example.com")
	req.SetPassword("password")
	req.SetUserAgent("test-agent")
	req.SetIp("127.0.0.1")

	resp, err := server.Login(context.Background(), req)
	require.NoError(t, err)
	result := resp.GetResult()
	require.True(t, result.GetOk())
	require.Equal(t, int64(1001), result.GetUserId())
	require.NotZero(t, result.GetSessionId())
	require.NotEmpty(t, result.GetAccessToken())
	require.NotEmpty(t, result.GetRefreshToken())
	require.NotNil(t, store.createdSession)
	require.Equal(t, "test-agent", store.createdSession.UserAgent)
	require.Equal(t, "127.0.0.1", store.createdSession.IP)
}

func TestLoginInvalidCredentials(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), &fakeUserClient{
		verifyPasswordResponse: verifyPasswordResponse(false, 0),
	})

	req := new(authenticatorv1.LoginRequest)
	req.SetEmail("user@example.com")
	req.SetPassword("wrong-password")

	_, err := server.Login(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidCredentials))
}

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

func newTestAuthenticatorServer(t *testing.T, store store.Store, tokens *token.Manager, userClient userv1.UserServiceClient) authenticatorv1.AuthenticatorServiceServer {
	t.Helper()

	node, err := snowflake.New()
	require.NoError(t, err)

	return New(&svc.ServiceContext{
		Cfg: config.Config{
			Sessions: config.SessionConfig{
				TTL: time.Hour,
			},
		},
		Store:      store,
		Tokens:     tokens,
		Snowflake:  node,
		UserClient: userClient,
	})
}

func newTestTokenManager(t *testing.T) *token.Manager {
	t.Helper()

	manager, err := token.NewManager(token.Config{
		Issuer:        "cordis.test",
		AccessSecret:  "test-access-secret-32-bytes-long",
		RefreshSecret: "test-refresh-secret-32-bytes-long",
		AccessTTL:     15 * time.Minute,
		RefreshTTL:    time.Hour,
	})
	require.NoError(t, err)
	return manager
}

func verifyPasswordResponse(ok bool, userID int64) *userv1.VerifyPasswordResponse {
	resp := new(userv1.VerifyPasswordResponse)
	resp.SetOk(ok)
	resp.SetUserId(userID)
	return resp
}

func createUserResponse(userID int64, email string) *userv1.CreateUserResponse {
	user := new(userv1.User)
	user.SetUserId(userID)
	user.SetEmail(email)

	resp := new(userv1.CreateUserResponse)
	resp.SetUser(user)
	return resp
}

type fakeUserClient struct {
	userv1.UserServiceClient
	createUserRequest      *userv1.CreateUserRequest
	createUserResponse     *userv1.CreateUserResponse
	createUserErr          error
	verifyPasswordResponse *userv1.VerifyPasswordResponse
	verifyPasswordErr      error
}

func (c *fakeUserClient) CreateUser(_ context.Context, req *userv1.CreateUserRequest, _ ...grpc.CallOption) (*userv1.CreateUserResponse, error) {
	c.createUserRequest = req
	if c.createUserErr != nil {
		return nil, c.createUserErr
	}
	return c.createUserResponse, nil
}

func (c *fakeUserClient) VerifyPassword(context.Context, *userv1.VerifyPasswordRequest, ...grpc.CallOption) (*userv1.VerifyPasswordResponse, error) {
	if c.verifyPasswordErr != nil {
		return nil, c.verifyPasswordErr
	}
	return c.verifyPasswordResponse, nil
}

type fakeSessionStore struct {
	sessions           map[int64]*model.Session
	createdSession     *model.Session
	rotatedOldHash     string
	rotatedNewHash     string
	revokedSessionID   int64
	revokedOtherUserID int64
	currentSessionID   int64
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{
		sessions: make(map[int64]*model.Session),
	}
}

func (s *fakeSessionStore) CreateSession(_ context.Context, sessionID, userID int64, refreshTokenHash, userAgent, ip string, expiresAt int64) (*model.Session, error) {
	session := &model.Session{
		SessionID:        sessionID,
		UserID:           userID,
		RefreshTokenHash: refreshTokenHash,
		UserAgent:        userAgent,
		IP:               ip,
		ExpiresAt:        expiresAt,
	}
	s.createdSession = session
	s.sessions[sessionID] = session
	return session, nil
}

func (s *fakeSessionStore) GetSession(_ context.Context, sessionID int64) (*model.Session, error) {
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return session, nil
}

func (s *fakeSessionStore) ListSessions(_ context.Context, userID int64) ([]*model.Session, error) {
	sessions := make([]*model.Session, 0)
	for _, session := range s.sessions {
		if session.UserID == userID && session.RevokedAt == 0 && session.ExpiresAt > time.Now().UnixMilli() {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (s *fakeSessionStore) RotateRefreshToken(_ context.Context, sessionID int64, oldRefreshTokenHash, newRefreshTokenHash string) error {
	session, ok := s.sessions[sessionID]
	if !ok || session.RefreshTokenHash != oldRefreshTokenHash {
		return sql.ErrNoRows
	}
	s.rotatedOldHash = oldRefreshTokenHash
	s.rotatedNewHash = newRefreshTokenHash
	session.RefreshTokenHash = newRefreshTokenHash
	return nil
}

func (s *fakeSessionStore) RevokeSession(_ context.Context, sessionID int64) error {
	session, ok := s.sessions[sessionID]
	if !ok {
		return sql.ErrNoRows
	}
	s.revokedSessionID = sessionID
	session.RevokedAt = time.Now().UnixMilli()
	return nil
}

func (s *fakeSessionStore) RevokeUserSession(_ context.Context, userID, sessionID int64) error {
	session, ok := s.sessions[sessionID]
	if !ok || session.UserID != userID || session.RevokedAt != 0 {
		return sql.ErrNoRows
	}
	s.revokedSessionID = sessionID
	session.RevokedAt = time.Now().UnixMilli()
	return nil
}

func (s *fakeSessionStore) RevokeOtherSessions(_ context.Context, userID, currentSessionID int64) (int64, error) {
	s.revokedOtherUserID = userID
	s.currentSessionID = currentSessionID
	var revoked int64
	for _, session := range s.sessions {
		if session.UserID == userID && session.SessionID != currentSessionID && session.RevokedAt == 0 {
			session.RevokedAt = time.Now().UnixMilli()
			revoked++
		}
	}
	return revoked, nil
}

type refreshSession struct {
	session      *model.Session
	refreshToken token.Token
}

func createRefreshSession(t *testing.T, store *fakeSessionStore, tokens *token.Manager, userID, sessionID, sessionExpiresAt int64) refreshSession {
	t.Helper()

	refreshToken, err := tokens.IssueRefreshToken(userID, sessionID, sessionExpiresAt, time.Now())
	require.NoError(t, err)

	session := &model.Session{
		SessionID:        sessionID,
		UserID:           userID,
		RefreshTokenHash: token.Hash(refreshToken.Raw),
		ExpiresAt:        sessionExpiresAt,
	}
	store.sessions[sessionID] = session
	return refreshSession{
		session:      session,
		refreshToken: refreshToken,
	}
}
