package store

import (
	"context"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
)

type registrationInviteRow struct {
	ID             int64  `db:"id"`
	CodeHash       string `db:"code_hash"`
	BoundEmail     string `db:"bound_email"`
	ReservedEmail  string `db:"reserved_email"`
	ReservedUntil  int64  `db:"reserved_until"`
	RedeemedUserID int64  `db:"redeemed_user_id"`
	RedeemedAt     int64  `db:"redeemed_at"`
	ExpiresAt      int64  `db:"expires_at"`
	RevokedAt      int64  `db:"revoked_at"`
	Label          string `db:"label"`
	CreatedAt      int64  `db:"created_at"`
}

func (s *SQLStore) CreateRegistrationInvite(ctx context.Context, invite *model.RegistrationInvite) error {
	_, err := s.q.ExecContext(
		ctx,
		CreateRegistrationInviteStatement,
		invite.ID,
		invite.CodeHash,
		invite.BoundEmail,
		invite.ExpiresAt,
		invite.Label,
		invite.CreatedAt,
	)
	return err
}

func (s *SQLStore) ReserveRegistrationInvite(
	ctx context.Context,
	codeHash, email string,
	now, reservedUntil int64,
) (*model.RegistrationInvite, error) {
	row := new(registrationInviteRow)
	if err := sqlx.GetContext(
		ctx,
		s.q,
		row,
		ReserveRegistrationInviteQuery,
		email,
		reservedUntil,
		codeHash,
		now,
	); err != nil {
		return nil, err
	}
	return registrationInviteFromRow(row), nil
}

func (s *SQLStore) RedeemRegistrationInvite(
	ctx context.Context,
	inviteID int64,
	email string,
	userID, redeemedAt int64,
) error {
	res, err := s.q.ExecContext(
		ctx,
		RedeemRegistrationInviteStatement,
		userID,
		redeemedAt,
		inviteID,
		email,
	)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

func (s *SQLStore) ReleaseRegistrationInvite(ctx context.Context, inviteID int64, email string) error {
	res, err := s.q.ExecContext(ctx, ReleaseRegistrationInviteStatement, inviteID, email)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

func (s *SQLStore) ListRegistrationInvites(
	ctx context.Context,
	beforeID int64,
	limit int,
) ([]*model.RegistrationInvite, error) {
	var rows []registrationInviteRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, ListRegistrationInvitesQuery, beforeID, limit); err != nil {
		return nil, err
	}
	invites := make([]*model.RegistrationInvite, 0, len(rows))
	for i := range rows {
		invites = append(invites, registrationInviteFromRow(&rows[i]))
	}
	return invites, nil
}

func (s *SQLStore) RevokeRegistrationInvite(ctx context.Context, inviteID, revokedAt int64) error {
	res, err := s.q.ExecContext(ctx, RevokeRegistrationInviteStatement, revokedAt, inviteID)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

func registrationInviteFromRow(row *registrationInviteRow) *model.RegistrationInvite {
	return &model.RegistrationInvite{
		ID:             row.ID,
		CodeHash:       row.CodeHash,
		BoundEmail:     row.BoundEmail,
		ReservedEmail:  row.ReservedEmail,
		ReservedUntil:  row.ReservedUntil,
		RedeemedUserID: row.RedeemedUserID,
		RedeemedAt:     row.RedeemedAt,
		ExpiresAt:      row.ExpiresAt,
		RevokedAt:      row.RevokedAt,
		Label:          row.Label,
		CreatedAt:      row.CreatedAt,
	}
}
