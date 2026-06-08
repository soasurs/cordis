package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func newTestStore(t *testing.T) (*SQLStore, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}

	sqlxDB := sqlx.NewDb(db, "postgres")
	return &SQLStore{db: sqlxDB, q: sqlxDB}, mock, func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet sql expectations: %v", err)
		}
		_ = sqlxDB.Close()
	}
}

func sqlPattern(query string) string {
	fields := strings.Fields(query)
	for i := range fields {
		fields[i] = regexp.QuoteMeta(fields[i])
	}
	return strings.Join(fields, `\s+`)
}

func createUserExecPattern() string {
	return sqlPattern(`
	INSERT INTO
		users (user_id, email, hashed_password, created_at, updated_at, deleted_at)
	VALUES
		($1, $2, $3, $4, $5, $6);
	`)
}

func createUserProfileExecPattern() string {
	return sqlPattern(`
	INSERT INTO
		user_profiles (user_id, name, avatar_uri, created_at, updated_at, deleted_at)
	VALUES
		($1, $2, $3, $4, $5, $6);
	`)
}

func TestCreateUser(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(createUserExecPattern()).
		WithArgs(int64(1001), "user@example.com", "hashed-password", sqlmock.AnyArg(), int64(0), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	user, err := store.CreateUser(context.Background(), 1001, "user@example.com", "hashed-password")
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	if user.UserID != 1001 || user.Email != "user@example.com" || user.HashedPassword != "hashed-password" {
		t.Fatalf("unexpected user: %+v", user)
	}
	if user.CreatedAt == 0 || user.UpdatedAt != 0 || user.DeletedAt != 0 {
		t.Fatalf("unexpected timestamps: %+v", user)
	}
}

func TestGetUser(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"user_id", "email", "hashed_password", "created_at", "updated_at", "deleted_at"}).
		AddRow(int64(1001), "user@example.com", "hashed-password", int64(10), int64(20), int64(0))

	mock.ExpectQuery(sqlPattern(GetUserQuery)).
		WithArgs(int64(1001), 0).
		WillReturnRows(rows)

	user, err := store.GetUser(context.Background(), 1001)
	if err != nil {
		t.Fatalf("GetUser returned error: %v", err)
	}
	if user.UserID != 1001 || user.Email != "user@example.com" || user.HashedPassword != "hashed-password" {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestCheckEmailAvailability(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"available"}).AddRow(true)

	mock.ExpectQuery(sqlPattern(CheckEmailAvailabilityQuery)).
		WithArgs("user@example.com", 0).
		WillReturnRows(rows)

	available, err := store.CheckEmailAvailability(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("CheckEmailAvailability returned error: %v", err)
	}
	if !available {
		t.Fatal("expected email to be available")
	}
}

func TestUpdateUserPassword(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(UpdateUserPasswordStatement)).
		WithArgs("new-hash", sqlmock.AnyArg(), int64(1001), 0).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.UpdateUserPassword(context.Background(), 1001, "new-hash"); err != nil {
		t.Fatalf("UpdateUserPassword returned error: %v", err)
	}
}

func TestUpdateUserPasswordNoRows(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(UpdateUserPasswordStatement)).
		WithArgs("new-hash", sqlmock.AnyArg(), int64(1001), 0).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := store.UpdateUserPassword(context.Background(), 1001, "new-hash")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestUpdateUserEmail(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"user_id", "email", "hashed_password", "created_at", "updated_at", "deleted_at"}).
		AddRow(int64(1001), "new@example.com", "hashed-password", int64(10), int64(30), int64(0))

	mock.ExpectQuery(sqlPattern(UpdateUserEmailQuery)).
		WithArgs("new@example.com", sqlmock.AnyArg(), int64(1001), 0).
		WillReturnRows(rows)

	user, err := store.UpdateUserEmail(context.Background(), 1001, "new@example.com")
	if err != nil {
		t.Fatalf("UpdateUserEmail returned error: %v", err)
	}
	if user.UserID != 1001 || user.Email != "new@example.com" || user.UpdatedAt != 30 {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestCreateUserProfile(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(createUserProfileExecPattern()).
		WithArgs(int64(1001), "display name", "avatar://1", sqlmock.AnyArg(), int64(0), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	profile, err := store.CreateUserProfile(context.Background(), 1001, "display name", "avatar://1")
	if err != nil {
		t.Fatalf("CreateUserProfile returned error: %v", err)
	}
	if profile.UserID != 1001 || profile.Name != "display name" || profile.AvatarURI != "avatar://1" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
}

func TestGetUserProfile(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"user_id", "name", "avatar_uri", "created_at", "updated_at", "deleted_at"}).
		AddRow(int64(1001), "display name", "avatar://1", int64(10), int64(20), int64(0))

	mock.ExpectQuery(sqlPattern(GetUserProfileQuery)).
		WithArgs(int64(1001), 0).
		WillReturnRows(rows)

	profile, err := store.GetUserProfile(context.Background(), 1001)
	if err != nil {
		t.Fatalf("GetUserProfile returned error: %v", err)
	}
	if profile.UserID != 1001 || profile.Name != "display name" || profile.AvatarURI != "avatar://1" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
}

func TestTransactCommit(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectExec(createUserProfileExecPattern()).
		WithArgs(int64(1001), "display name", "", sqlmock.AnyArg(), int64(0), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := store.Transact(context.Background(), func(txStore Store) error {
		_, err := txStore.CreateUserProfile(context.Background(), 1001, "display name", "")
		return err
	})
	if err != nil {
		t.Fatalf("Transact returned error: %v", err)
	}
}

func TestTransactRollback(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	errRollback := errors.New("rollback")

	mock.ExpectBegin()
	mock.ExpectRollback()

	err := store.Transact(context.Background(), func(txStore Store) error {
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("expected rollback error, got %v", err)
	}
}
