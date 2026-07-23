package store

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestCreateAssetWithQuotaLocksCountAndInsert(t *testing.T) {
	db, mock := newSQLMock(t)
	assetStore := New(db)
	asset := testAsset()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(lockUploadQuotaScopeStatement)).
		WithArgs("cordis:media:upload-quota:1001").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM assets`).
		WithArgs(int64(1001)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))
	mock.ExpectExec(`INSERT INTO assets`).
		WithArgs(
			asset.ID,
			asset.CreatedByUserID,
			asset.SubjectID,
			asset.Kind,
			asset.Status,
			asset.StorageBackend,
			asset.StagingKey,
			asset.PublishedKey,
			asset.ExpectedSize,
			asset.ContentType,
			asset.ExpiresAt,
			asset.CreatedAt,
			asset.UpdatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, assetStore.CreateAssetWithQuota(t.Context(), asset, 5))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateAssetWithQuotaRejectsAtLimit(t *testing.T) {
	db, mock := newSQLMock(t)
	assetStore := New(db)
	asset := testAsset()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(lockUploadQuotaScopeStatement)).
		WithArgs("cordis:media:upload-quota:1001").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM assets`).
		WithArgs(int64(1001)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectRollback()

	require.ErrorIs(t, assetStore.CreateAssetWithQuota(t.Context(), asset, 5), ErrActiveUploadLimit)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAcquireAssetLockUsesSessionAdvisoryLock(t *testing.T) {
	db, mock := newSQLMock(t)
	assetStore := New(db)

	mock.ExpectExec(regexp.QuoteMeta(lockAssetStatement)).
		WithArgs("cordis:media:asset:123").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(unlockAssetStatement)).
		WithArgs("cordis:media:asset:123").
		WillReturnResult(sqlmock.NewResult(0, 1))

	lockedStore, unlock, err := assetStore.AcquireAssetLock(t.Context(), 123)
	require.NoError(t, err)
	require.NotNil(t, lockedStore)
	unlock()
	require.NoError(t, mock.ExpectationsWereMet())
}

func newSQLMock(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock) {
	t.Helper()
	rawDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawDB.Close() })
	return sqlx.NewDb(rawDB, "sqlmock"), mock
}

func testAsset() *Asset {
	return &Asset{
		ID:              123,
		CreatedByUserID: 1001,
		SubjectID:       1001,
		Kind:            KindUserAvatar,
		Status:          StatusCreated,
		StorageBackend:  "r2",
		StagingKey:      "staging/123",
		ExpectedSize:    1024,
		ContentType:     "image/png",
		ExpiresAt:       2000,
		CreatedAt:       1000,
		UpdatedAt:       1000,
	}
}
