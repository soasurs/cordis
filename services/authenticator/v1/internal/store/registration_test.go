package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
)

var registrationInviteColumns = []string{
	"id", "code_hash", "bound_email", "reserved_email", "reserved_until",
	"redeemed_user_id", "redeemed_at", "expires_at", "revoked_at", "label", "created_at",
}

func TestCreateRegistrationInvite(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(CreateRegistrationInviteStatement)).
		WithArgs(int64(1001), "code-hash", "user@example.com", int64(3000), "beta", int64(1000)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.CreateRegistrationInvite(context.Background(), &model.RegistrationInvite{
		ID: 1001, CodeHash: "code-hash", BoundEmail: "user@example.com",
		ExpiresAt: 3000, Label: "beta", CreatedAt: 1000,
	})
	require.NoError(t, err)
}

func TestReserveRegistrationInvite(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows(registrationInviteColumns).
		AddRow(int64(1001), "code-hash", "", "user@example.com", int64(2500), int64(0), int64(0), int64(3000), int64(0), "beta", int64(1000))
	mock.ExpectQuery(sqlPattern(ReserveRegistrationInviteQuery)).
		WithArgs("user@example.com", int64(2500), "code-hash", int64(2000)).
		WillReturnRows(rows)

	invite, err := store.ReserveRegistrationInvite(context.Background(), "code-hash", "user@example.com", 2000, 2500)
	require.NoError(t, err)
	require.Equal(t, int64(1001), invite.ID)
	require.Equal(t, "user@example.com", invite.ReservedEmail)
	require.Equal(t, int64(2500), invite.ReservedUntil)
}

func TestRedeemReleaseAndRevokeRegistrationInvite(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(RedeemRegistrationInviteStatement)).
		WithArgs(int64(2001), int64(2500), int64(1001), "user@example.com").
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, store.RedeemRegistrationInvite(context.Background(), 1001, "user@example.com", 2001, 2500))

	mock.ExpectExec(sqlPattern(ReleaseRegistrationInviteStatement)).
		WithArgs(int64(1002), "other@example.com").
		WillReturnResult(sqlmock.NewResult(0, 0))
	require.ErrorIs(t, store.ReleaseRegistrationInvite(context.Background(), 1002, "other@example.com"), sql.ErrNoRows)

	mock.ExpectExec(sqlPattern(RevokeRegistrationInviteStatement)).
		WithArgs(int64(2600), int64(1003)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, store.RevokeRegistrationInvite(context.Background(), 1003, 2600))
}

func TestListRegistrationInvites(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows(registrationInviteColumns).
		AddRow(int64(1002), "hash-b", "", "", int64(0), int64(0), int64(0), int64(0), int64(0), "b", int64(1002)).
		AddRow(int64(1001), "hash-a", "user@example.com", "", int64(0), int64(0), int64(0), int64(3000), int64(0), "a", int64(1001))
	mock.ExpectQuery(sqlPattern(ListRegistrationInvitesQuery)).
		WithArgs(int64(0), 10).
		WillReturnRows(rows)

	invites, err := store.ListRegistrationInvites(context.Background(), 0, 10)
	require.NoError(t, err)
	require.Len(t, invites, 2)
	require.Equal(t, []int64{1002, 1001}, []int64{invites[0].ID, invites[1].ID})
	require.Equal(t, "user@example.com", invites[1].BoundEmail)
}
