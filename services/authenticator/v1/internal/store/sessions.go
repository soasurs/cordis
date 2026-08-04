package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
)

type sessionRow struct {
	SessionID                      int64  `db:"session_id"`
	UserID                         int64  `db:"user_id"`
	RefreshTokenHash               string `db:"refresh_token_hash"`
	RefreshTokenID                 string `db:"refresh_token_id"`
	RefreshTokenIssuedAt           int64  `db:"refresh_token_issued_at"`
	RefreshTokenExpiresAt          int64  `db:"refresh_token_expires_at"`
	PreviousRefreshTokenHash       string `db:"previous_refresh_token_hash"`
	PreviousRefreshTokenValidUntil int64  `db:"previous_refresh_token_valid_until"`
	UserAgent                      string `db:"user_agent"`
	IP                             string `db:"ip"`
	CreatedAt                      int64  `db:"created_at"`
	UpdatedAt                      int64  `db:"updated_at"`
	ExpiresAt                      int64  `db:"expires_at"`
	AbsoluteExpiresAt              int64  `db:"absolute_expires_at"`
	RevokedAt                      int64  `db:"revoked_at"`
}

func (s *SQLStore) CreateSession(ctx context.Context, params CreateSessionParams) (*model.Session, error) {
	row := &sessionRow{
		SessionID:             params.SessionID,
		UserID:                params.UserID,
		RefreshTokenHash:      params.RefreshTokenHash,
		RefreshTokenID:        params.RefreshTokenID,
		RefreshTokenIssuedAt:  params.RefreshTokenIssuedAt,
		RefreshTokenExpiresAt: params.RefreshTokenExpiresAt,
		UserAgent:             params.UserAgent,
		IP:                    params.IP,
		CreatedAt:             time.Now().UnixMilli(),
		UpdatedAt:             0,
		ExpiresAt:             params.ExpiresAt,
		AbsoluteExpiresAt:     params.AbsoluteExpiresAt,
		RevokedAt:             0,
	}

	_, err := s.q.Exec(
		ctx,
		CreateSessionStatement,
		row.SessionID,
		row.UserID,
		row.RefreshTokenHash,
		row.RefreshTokenID,
		row.RefreshTokenIssuedAt,
		row.RefreshTokenExpiresAt,
		row.PreviousRefreshTokenHash,
		row.PreviousRefreshTokenValidUntil,
		row.UserAgent,
		row.IP,
		row.CreatedAt,
		row.UpdatedAt,
		row.ExpiresAt,
		row.AbsoluteExpiresAt,
		row.RevokedAt,
	)
	if err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (s *SQLStore) GetSession(ctx context.Context, sessionID int64) (*model.Session, error) {
	row, err := scanOne(ctx, s.q, GetSessionQuery, pgx.RowToStructByName[sessionRow], sessionID)
	if err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (s *SQLStore) ListSessions(ctx context.Context, userID int64) ([]*model.Session, error) {
	rows, err := scanMany(ctx, s.q, ListSessionsQuery, pgx.RowToStructByName[sessionRow], userID, 0, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	sessions := make([]*model.Session, 0, len(rows))
	for i := range rows {
		sessions = append(sessions, rows[i].toModel())
	}
	return sessions, nil
}

func (s *SQLStore) RotateRefreshToken(ctx context.Context, params RotateRefreshTokenParams) error {
	tag, err := s.q.Exec(ctx, RotateRefreshTokenStatement,
		params.NewRefreshTokenHash, params.NewRefreshTokenID, params.NewRefreshTokenIssuedAt,
		params.NewRefreshTokenExpiresAt, params.PreviousRefreshTokenValidUntil, params.ExpiresAt,
		params.SessionID, 0, params.OldRefreshTokenHash, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	return checkRowsAffected(tag)
}

func (s *SQLStore) RevokeSession(ctx context.Context, sessionID int64) error {
	now := time.Now().UnixMilli()
	tag, err := s.q.Exec(ctx, RevokeSessionStatement, now, sessionID, 0)
	if err != nil {
		return err
	}
	return checkRowsAffected(tag)
}

func (s *SQLStore) RevokeUserSession(ctx context.Context, userID, sessionID int64) error {
	now := time.Now().UnixMilli()
	tag, err := s.q.Exec(ctx, RevokeUserSessionStatement, now, userID, sessionID, 0)
	if err != nil {
		return err
	}
	return checkRowsAffected(tag)
}

func (s *SQLStore) RevokeOtherSessions(ctx context.Context, userID, currentSessionID int64) (int64, error) {
	now := time.Now().UnixMilli()
	tag, err := s.q.Exec(ctx, RevokeOtherSessionsStatement, now, userID, currentSessionID, 0)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *sessionRow) toModel() *model.Session {
	return &model.Session{
		SessionID:                      r.SessionID,
		UserID:                         r.UserID,
		RefreshTokenHash:               r.RefreshTokenHash,
		RefreshTokenID:                 r.RefreshTokenID,
		RefreshTokenIssuedAt:           r.RefreshTokenIssuedAt,
		RefreshTokenExpiresAt:          r.RefreshTokenExpiresAt,
		PreviousRefreshTokenHash:       r.PreviousRefreshTokenHash,
		PreviousRefreshTokenValidUntil: r.PreviousRefreshTokenValidUntil,
		UserAgent:                      r.UserAgent,
		IP:                             r.IP,
		CreatedAt:                      r.CreatedAt,
		UpdatedAt:                      r.UpdatedAt,
		ExpiresAt:                      r.ExpiresAt,
		AbsoluteExpiresAt:              r.AbsoluteExpiresAt,
		RevokedAt:                      r.RevokedAt,
	}
}
