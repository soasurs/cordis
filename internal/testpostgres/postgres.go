//go:build integration

package testpostgres

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
)

const dataSourceEnv = "CORDIS_TEST_POSTGRES_DSN"

func New(t *testing.T, migrations fs.FS) *sqlx.DB {
	t.Helper()

	dataSource := os.Getenv(dataSourceEnv)
	if dataSource == "" {
		t.Skipf("%s is not set", dataSourceEnv)
	}

	adminDB, err := database.NewPostgres(database.Config{
		DataSource:   dataSource,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("open integration postgres: %v", err)
	}

	schema := fmt.Sprintf("cordis_test_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(context.Background(), "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create test schema: %v", err)
	}

	testDataSource, err := withSearchPath(dataSource, schema)
	if err != nil {
		dropSchema(adminDB, schema)
		_ = adminDB.Close()
		t.Fatalf("configure test schema: %v", err)
	}

	db, err := database.NewPostgres(database.Config{
		DataSource:   testDataSource,
		MaxOpenConns: 4,
		MaxIdleConns: 4,
	})
	if err != nil {
		dropSchema(adminDB, schema)
		_ = adminDB.Close()
		t.Fatalf("open test schema: %v", err)
	}

	if err := migration.Apply(context.Background(), db, migrations); err != nil {
		_ = db.Close()
		dropSchema(adminDB, schema)
		_ = adminDB.Close()
		t.Fatalf("apply migrations: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
		dropSchema(adminDB, schema)
		_ = adminDB.Close()
	})
	return db
}

func withSearchPath(dataSource, schema string) (string, error) {
	if strings.Contains(dataSource, "://") {
		parsed, err := url.Parse(dataSource)
		if err != nil {
			return "", err
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String(), nil
	}
	return dataSource + " search_path=" + schema, nil
}

func dropSchema(db *sqlx.DB, schema string) {
	_, _ = db.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+pq.QuoteIdentifier(schema)+" CASCADE")
}
