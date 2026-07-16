package store

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
)

type sessionRow struct {
	SessionID        int64  `db:"session_id"`
	UserID           int64  `db:"user_id"`
	RefreshTokenHash string `db:"refresh_token_hash"`
	UserAgent        string `db:"user_agent"`
	IP               string `db:"ip"`
	CreatedAt        int64  `db:"created_at"`
	UpdatedAt        int64  `db:"updated_at"`
	ExpiresAt        int64  `db:"expires_at"`
	RevokedAt        int64  `db:"revoked_at"`
}

func (s *SQLStore) CreateSession(ctx context.Context, sessionID, userID int64, refreshTokenHash, userAgent, ip string, expiresAt int64) (*model.Session, error) {
	row := &sessionRow{
		SessionID:        sessionID,
		UserID:           userID,
		RefreshTokenHash: refreshTokenHash,
		UserAgent:        userAgent,
		IP:               ip,
		CreatedAt:        time.Now().UnixMilli(),
		UpdatedAt:        0,
		ExpiresAt:        expiresAt,
		RevokedAt:        0,
	}

	_, err := sqlx.NamedExecContext(ctx, s.q, CreateSessionStatement, row)
	if err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (s *SQLStore) GetSession(ctx context.Context, sessionID int64) (*model.Session, error) {
	row := new(sessionRow)
	err := sqlx.GetContext(ctx, s.q, row, GetSessionQuery, sessionID)
	if err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (s *SQLStore) ListSessions(ctx context.Context, userID int64) ([]*model.Session, error) {
	rows := make([]sessionRow, 0)
	if err := sqlx.SelectContext(ctx, s.q, &rows, ListSessionsQuery, userID, 0, time.Now().UnixMilli()); err != nil {
		return nil, err
	}
	sessions := make([]*model.Session, 0, len(rows))
	for i := range rows {
		sessions = append(sessions, rows[i].toModel())
	}
	return sessions, nil
}

func (s *SQLStore) RotateRefreshToken(ctx context.Context, sessionID int64, oldRefreshTokenHash, newRefreshTokenHash string) error {
	res, err := s.q.ExecContext(ctx, RotateRefreshTokenStatement, newRefreshTokenHash, time.Now().UnixMilli(), sessionID, 0, oldRefreshTokenHash)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

func (s *SQLStore) RevokeSession(ctx context.Context, sessionID int64) error {
	now := time.Now().UnixMilli()
	res, err := s.q.ExecContext(ctx, RevokeSessionStatement, now, sessionID, 0)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

func (s *SQLStore) RevokeUserSession(ctx context.Context, userID, sessionID int64) error {
	now := time.Now().UnixMilli()
	res, err := s.q.ExecContext(ctx, RevokeUserSessionStatement, now, userID, sessionID, 0)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

func (s *SQLStore) RevokeOtherSessions(ctx context.Context, userID, currentSessionID int64) (int64, error) {
	now := time.Now().UnixMilli()
	res, err := s.q.ExecContext(ctx, RevokeOtherSessionsStatement, now, userID, currentSessionID, 0)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *sessionRow) toModel() *model.Session {
	return &model.Session{
		SessionID:        r.SessionID,
		UserID:           r.UserID,
		RefreshTokenHash: r.RefreshTokenHash,
		UserAgent:        r.UserAgent,
		IP:               r.IP,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
		ExpiresAt:        r.ExpiresAt,
		RevokedAt:        r.RevokedAt,
	}
}
