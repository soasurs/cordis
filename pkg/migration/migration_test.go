package migration

import (
	"testing"
	"testing/fstest"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestApplySkipsDownMigrations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	sqlxDB := sqlx.NewDb(db, "postgres")
	defer sqlxDB.Close()

	files := fstest.MapFS{
		"000001_create.sql":       {Data: []byte("CREATE TABLE first_table (id BIGINT)")},
		"000002_feature.down.sql": {Data: []byte("DROP TABLE first_table")},
		"000002_feature.up.sql":   {Data: []byte("CREATE TABLE second_table (id BIGINT)")},
	}

	mock.ExpectExec("CREATE TABLE first_table").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE second_table").WillReturnResult(sqlmock.NewResult(0, 0))

	if err := Apply(t.Context(), sqlxDB, files); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected migration execution: %v", err)
	}
}
