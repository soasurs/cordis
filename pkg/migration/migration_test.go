package migration

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestApplySkipsDownMigrations(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	sqlxDB := sqlx.NewDb(db, "postgres")
	defer sqlxDB.Close()

	files := fstest.MapFS{
		"000001_create.sql":       {Data: []byte("CREATE TABLE first_table (id BIGINT)")},
		"000002_feature.down.sql": {Data: []byte("DROP TABLE first_table")},
		"000002_feature.up.sql":   {Data: []byte("CREATE TABLE second_table (id BIGINT)")},
	}

	mock.ExpectExec("CREATE TABLE first_table").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE second_table").WillReturnResult(sqlmock.NewResult(0, 0))

	require.NoError(t, Apply(t.Context(), sqlxDB, files))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyNamedRecordsAndSkipsMigrations(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	sqlxDB := sqlx.NewDb(db, "postgres")
	defer sqlxDB.Close()

	query := []byte("CREATE TABLE first_table (id BIGINT)")
	files := fstest.MapFS{"000001_create.sql": {Data: query}}
	checksum := fmt.Sprintf("%x", sha256.Sum256(query))

	mock.ExpectExec("SELECT pg_advisory_lock").
		WithArgs("cordis:migrations").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS cordis_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT checksum").
		WithArgs("user", "000001_create.sql").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE first_table").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO cordis_schema_migrations").
		WithArgs("user", "000001_create.sql", checksum).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectExec("SELECT pg_advisory_unlock").
		WithArgs("cordis:migrations").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, ApplyNamed(t.Context(), sqlxDB, "user", files))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyNamedRejectsChangedMigration(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	sqlxDB := sqlx.NewDb(db, "postgres")
	defer sqlxDB.Close()

	files := fstest.MapFS{"000001_create.sql": {Data: []byte("SELECT 1")}}

	mock.ExpectExec("SELECT pg_advisory_lock").
		WithArgs("cordis:migrations").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS cordis_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT checksum").
		WithArgs("user", "000001_create.sql").
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}).AddRow("old-checksum"))
	mock.ExpectExec("SELECT pg_advisory_unlock").
		WithArgs("cordis:migrations").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = ApplyNamed(t.Context(), sqlxDB, "user", files)
	require.ErrorContains(t, err, "checksum changed")
	require.NoError(t, mock.ExpectationsWereMet())
}
