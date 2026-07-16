package server

import (
	"context"
	"database/sql"
	"errors"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *authenticatorServer) ListSessions(ctx context.Context, req *authenticatorv1.ListSessionsRequest) (*authenticatorv1.ListSessionsResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "user id is required")
	}
	sessions, err := s.svcCtx.Store.ListSessions(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}

	resp := new(authenticatorv1.ListSessionsResponse)
	pbSessions := make([]*authenticatorv1.Session, 0, len(sessions))
	for _, session := range sessions {
		pbSession := new(authenticatorv1.Session)
		pbSession.SetSessionId(session.SessionID)
		pbSession.SetUserId(session.UserID)
		pbSession.SetCreatedAt(session.CreatedAt)
		pbSession.SetUpdatedAt(session.UpdatedAt)
		pbSession.SetExpiresAt(session.ExpiresAt)
		pbSession.SetRevokedAt(session.RevokedAt)
		pbSession.SetUserAgent(session.UserAgent)
		pbSession.SetIp(session.IP)
		pbSessions = append(pbSessions, pbSession)
	}
	resp.SetSessions(pbSessions)
	return resp, nil
}

func (s *authenticatorServer) RevokeUserSession(ctx context.Context, req *authenticatorv1.RevokeUserSessionRequest) (*authenticatorv1.RevokeUserSessionResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "user id is required")
	}
	if req.GetSessionId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "session id is required")
	}
	if err := s.svcCtx.Store.RevokeUserSession(ctx, req.GetUserId(), req.GetSessionId()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "session not found")
		}
		return nil, err
	}

	resp := new(authenticatorv1.RevokeUserSessionResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *authenticatorServer) RevokeOtherSessions(ctx context.Context, req *authenticatorv1.RevokeOtherSessionsRequest) (*authenticatorv1.RevokeOtherSessionsResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "user id is required")
	}
	if req.GetCurrentSessionId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "current session id is required")
	}
	revoked, err := s.svcCtx.Store.RevokeOtherSessions(ctx, req.GetUserId(), req.GetCurrentSessionId())
	if err != nil {
		return nil, err
	}

	resp := new(authenticatorv1.RevokeOtherSessionsResponse)
	resp.SetRevoked(int32(revoked))
	return resp, nil
}
