package server

import (
	"context"
	"database/sql"
	"errors"
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
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if userClient.createUserRequest.GetName() != "display name" ||
		userClient.createUserRequest.GetEmail() != "user@example.com" ||
		userClient.createUserRequest.GetPassword() != "password" {
		t.Fatalf("unexpected create user request: %v", userClient.createUserRequest)
	}
	result := resp.GetResult()
	if !result.GetOk() || result.GetUserId() != 1001 || result.GetAccessToken() == "" || result.GetRefreshToken() == "" {
		t.Fatalf("unexpected result: %v", result)
	}
	if store.createdSession == nil || store.createdSession.UserAgent != "test-agent" || store.createdSession.IP != "127.0.0.1" {
		t.Fatalf("unexpected created session: %+v", store.createdSession)
	}
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
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Register error = %v, want %v", err, expectedErr)
	}
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
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	result := resp.GetResult()
	if !result.GetOk() || result.GetUserId() != 1001 || result.GetSessionId() == 0 {
		t.Fatalf("unexpected result: %v", result)
	}
	if result.GetAccessToken() == "" || result.GetRefreshToken() == "" {
		t.Fatalf("expected tokens: %v", result)
	}
	if store.createdSession == nil || store.createdSession.UserAgent != "test-agent" || store.createdSession.IP != "127.0.0.1" {
		t.Fatalf("unexpected created session: %+v", store.createdSession)
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), &fakeUserClient{
		verifyPasswordResponse: verifyPasswordResponse(false, 0),
	})

	req := new(authenticatorv1.LoginRequest)
	req.SetEmail("user@example.com")
	req.SetPassword("wrong-password")

	_, err := server.Login(context.Background(), req)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Login code = %v, want %v: %v", status.Code(err), codes.Unauthenticated, err)
	}
	if !rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidCredentials) {
		t.Fatalf("expected invalid credentials reason: %v", err)
	}
}

func TestRefresh(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	resp, err := server.Refresh(context.Background(), req)
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	result := resp.GetResult()
	if !result.GetOk() || result.GetUserId() != 1001 || result.GetSessionId() != 2001 {
		t.Fatalf("unexpected result: %v", result)
	}
	if result.GetRefreshToken() == session.refreshToken.Raw {
		t.Fatal("expected rotated refresh token")
	}
	if store.rotatedOldHash != token.Hash(session.refreshToken.Raw) || store.rotatedNewHash != token.Hash(result.GetRefreshToken()) {
		t.Fatalf("unexpected rotation old=%q new=%q", store.rotatedOldHash, store.rotatedNewHash)
	}
}

func TestRefreshInvalidToken(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken("invalid-token")

	_, err := server.Refresh(context.Background(), req)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Refresh code = %v, want %v: %v", status.Code(err), codes.Unauthenticated, err)
	}
	if !rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRefreshToken) {
		t.Fatalf("expected invalid refresh token reason: %v", err)
	}
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
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Refresh code = %v, want %v: %v", status.Code(err), codes.Unauthenticated, err)
	}
	if !rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRefreshToken) {
		t.Fatalf("expected invalid refresh token reason: %v", err)
	}
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
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Refresh code = %v, want %v: %v", status.Code(err), codes.Unauthenticated, err)
	}
	if !rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorSessionExpired) {
		t.Fatalf("expected session expired reason: %v", err)
	}
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
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Refresh code = %v, want %v: %v", status.Code(err), codes.Unauthenticated, err)
	}
	if !rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorSessionRevoked) {
		t.Fatalf("expected session revoked reason: %v", err)
	}
}

func TestLogout(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.LogoutRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	resp, err := server.Logout(context.Background(), req)
	if err != nil {
		t.Fatalf("Logout returned error: %v", err)
	}
	if !resp.GetOk() {
		t.Fatalf("unexpected response: %v", resp)
	}
	if store.revokedSessionID != 2001 {
		t.Fatalf("revoked session id = %d, want 2001", store.revokedSessionID)
	}
}

func TestLogoutInvalidToken(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.LogoutRequest)
	req.SetRefreshToken("invalid-token")

	_, err := server.Logout(context.Background(), req)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Logout code = %v, want %v: %v", status.Code(err), codes.Unauthenticated, err)
	}
	if !rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRefreshToken) {
		t.Fatalf("expected invalid refresh token reason: %v", err)
	}
}

func TestVerifyAccessToken(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	sessionExpiresAt := time.Now().Add(time.Hour).UnixMilli()
	accessToken, err := tokens.IssueAccessToken(1001, 2001, time.Now())
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}
	store.sessions[2001] = &model.Session{
		SessionID: 2001,
		UserID:    1001,
		ExpiresAt: sessionExpiresAt,
	}
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.VerifyAccessTokenRequest)
	req.SetAccessToken(accessToken.Raw)

	resp, err := server.VerifyAccessToken(context.Background(), req)
	if err != nil {
		t.Fatalf("VerifyAccessToken returned error: %v", err)
	}
	if !resp.GetOk() || resp.GetUserId() != 1001 || resp.GetSessionId() != 2001 {
		t.Fatalf("unexpected response: %v", resp)
	}
}

func TestVerifyAccessTokenInvalidToken(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.VerifyAccessTokenRequest)
	req.SetAccessToken("invalid-token")

	_, err := server.VerifyAccessToken(context.Background(), req)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("VerifyAccessToken code = %v, want %v: %v", status.Code(err), codes.Unauthenticated, err)
	}
	if !rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidAccessToken) {
		t.Fatalf("expected invalid access token reason: %v", err)
	}
}

func newTestAuthenticatorServer(t *testing.T, store store.Store, tokens *token.Manager, userClient userv1.UserServiceClient) authenticatorv1.AuthenticatorServiceServer {
	t.Helper()

	node, err := snowflake.New()
	if err != nil {
		t.Fatalf("new snowflake node: %v", err)
	}

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
	if err != nil {
		t.Fatalf("new token manager: %v", err)
	}
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
	sessions         map[int64]*model.Session
	createdSession   *model.Session
	rotatedOldHash   string
	rotatedNewHash   string
	revokedSessionID int64
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

type refreshSession struct {
	session      *model.Session
	refreshToken token.Token
}

func createRefreshSession(t *testing.T, store *fakeSessionStore, tokens *token.Manager, userID, sessionID, sessionExpiresAt int64) refreshSession {
	t.Helper()

	refreshToken, err := tokens.IssueRefreshToken(userID, sessionID, sessionExpiresAt, time.Now())
	if err != nil {
		t.Fatalf("issue refresh token: %v", err)
	}

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
