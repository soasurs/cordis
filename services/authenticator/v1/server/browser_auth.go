package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/gatewayticket"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
)

func (s *authenticatorServer) AuthenticateCookie(ctx context.Context, req *authenticatorv1.AuthenticateCookieRequest) (*authenticatorv1.AuthenticateCookieResponse, error) {
	if req.GetAccessToken() != "" {
		accessToken, expired, err := s.svcCtx.Tokens.ParseAccessTokenAllowExpired(req.GetAccessToken())
		if err != nil {
			return nil, invalidAccessTokenError()
		}
		minimumExpiresAt := time.Now().Add(time.Duration(req.GetMinimumAccessTtlMs()) * time.Millisecond).UnixMilli()
		if !expired && accessToken.ExpiresAt > minimumExpiresAt {
			auth, err := s.verifyParsedAccessToken(ctx, accessToken)
			if err != nil {
				return nil, err
			}
			return cookieAuthenticationResponse(auth, nil), nil
		}
		if req.GetRefreshToken() == "" {
			return nil, invalidAccessTokenError()
		}
		refreshToken, err := s.svcCtx.Tokens.ParseRefreshToken(req.GetRefreshToken())
		if err != nil || refreshToken.SessionID != accessToken.SessionID || refreshToken.UserID != accessToken.UserID {
			return nil, invalidRefreshTokenError()
		}
	} else if req.GetRefreshToken() == "" {
		return nil, invalidAccessTokenError()
	}

	rotated, err := s.rotateRefreshToken(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, err
	}
	auth := new(authenticatorv1.VerifyAccessTokenResponse)
	auth.SetOk(true)
	auth.SetUserId(rotated.GetUserId())
	auth.SetSessionId(rotated.GetSessionId())
	auth.SetExpiresAt(rotated.GetAccessTokenExpiresAt())
	return cookieAuthenticationResponse(auth, rotated), nil
}

func (s *authenticatorServer) verifyParsedAccessToken(ctx context.Context, accessToken token.Token) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	session, err := s.svcCtx.Store.GetSession(ctx, accessToken.SessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, invalidAccessTokenError()
		}
		return nil, err
	}
	if err := checkSession(session, time.Now().UnixMilli()); err != nil {
		return nil, err
	}
	if session.UserID != accessToken.UserID {
		return nil, invalidAccessTokenError()
	}
	resp := new(authenticatorv1.VerifyAccessTokenResponse)
	resp.SetOk(true)
	resp.SetUserId(accessToken.UserID)
	resp.SetSessionId(accessToken.SessionID)
	resp.SetExpiresAt(accessToken.ExpiresAt)
	return resp, nil
}

func cookieAuthenticationResponse(auth *authenticatorv1.VerifyAccessTokenResponse, rotated *authenticatorv1.AuthenticationResult) *authenticatorv1.AuthenticateCookieResponse {
	resp := new(authenticatorv1.AuthenticateCookieResponse)
	resp.SetOk(auth.GetOk())
	resp.SetUserId(auth.GetUserId())
	resp.SetSessionId(auth.GetSessionId())
	resp.SetExpiresAt(auth.GetExpiresAt())
	if rotated != nil {
		resp.SetRotated(rotated)
	}
	return resp
}

func (s *authenticatorServer) CreateGatewayTicket(ctx context.Context, req *authenticatorv1.CreateGatewayTicketRequest) (*authenticatorv1.CreateGatewayTicketResponse, error) {
	session, err := s.svcCtx.Store.GetSession(ctx, req.GetSessionId())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, invalidAccessTokenError()
		}
		return nil, err
	}
	if session.UserID != req.GetUserId() {
		return nil, invalidAccessTokenError()
	}
	if err := checkSession(session, time.Now().UnixMilli()); err != nil {
		return nil, err
	}
	if req.GetAccessTokenExpiresAt() <= time.Now().UnixMilli() {
		return nil, invalidAccessTokenError()
	}
	raw, err := token.GenerateOpaqueToken()
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().Add(s.svcCtx.Cfg.GatewayTickets.TTL).UnixMilli()
	if err := s.svcCtx.GatewayTickets.Put(ctx, token.Hash(raw), gatewayticket.Ticket{
		UserID: req.GetUserId(), SessionID: req.GetSessionId(),
		AccessTokenExpiresAt: req.GetAccessTokenExpiresAt(),
	}, s.svcCtx.Cfg.GatewayTickets.TTL); err != nil {
		return nil, err
	}
	resp := new(authenticatorv1.CreateGatewayTicketResponse)
	resp.SetGatewayTicket(raw)
	resp.SetExpiresAt(expiresAt)
	return resp, nil
}

func (s *authenticatorServer) RedeemGatewayTicket(ctx context.Context, req *authenticatorv1.RedeemGatewayTicketRequest) (*authenticatorv1.RedeemGatewayTicketResponse, error) {
	if req.GetGatewayTicket() == "" {
		return nil, invalidAccessTokenError()
	}
	ticket, err := s.svcCtx.GatewayTickets.Redeem(ctx, token.Hash(req.GetGatewayTicket()))
	if err != nil {
		if errors.Is(err, gatewayticket.ErrNotFound) {
			return nil, invalidAccessTokenError()
		}
		return nil, err
	}
	session, err := s.svcCtx.Store.GetSession(ctx, ticket.SessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, invalidAccessTokenError()
		}
		return nil, err
	}
	if session.UserID != ticket.UserID || ticket.AccessTokenExpiresAt <= time.Now().UnixMilli() {
		return nil, invalidAccessTokenError()
	}
	if err := checkSession(session, time.Now().UnixMilli()); err != nil {
		return nil, err
	}
	resp := new(authenticatorv1.RedeemGatewayTicketResponse)
	resp.SetOk(true)
	resp.SetUserId(ticket.UserID)
	resp.SetSessionId(ticket.SessionID)
	resp.SetAccessTokenExpiresAt(ticket.AccessTokenExpiresAt)
	return resp, nil
}
