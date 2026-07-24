package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
)

const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS cordis_schema_migrations (
	service TEXT NOT NULL,
	version TEXT NOT NULL,
	checksum TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (service, version)
)`

func Apply(ctx context.Context, db *sqlx.DB, migrations fs.FS) error {
	entries, err := migrationEntries(migrations)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		query, err := fs.ReadFile(migrations, entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		if _, err := db.ExecContext(ctx, string(query)); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// ApplyNamed applies each migration once for service and records its checksum.
// A PostgreSQL advisory lock serializes migrators sharing the database.
func ApplyNamed(ctx context.Context, db *sqlx.DB, service string, migrations fs.FS) (err error) {
	service = strings.TrimSpace(service)
	if service == "" {
		return fmt.Errorf("migration service is required")
	}

	entries, err := migrationEntries(migrations)
	if err != nil {
		return err
	}
	conn, err := db.Connx(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Close()

	lockName := "cordis:migrations"
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`, lockName); err != nil {
		return fmt.Errorf("lock migrations for %s: %w", service, err)
	}
	defer func() {
		if _, unlockErr := conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, lockName); err == nil && unlockErr != nil {
			err = fmt.Errorf("unlock migrations for %s: %w", service, unlockErr)
		}
	}()

	if _, err := conn.ExecContext(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		query, err := fs.ReadFile(migrations, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(query))

		var recordedChecksum string
		err = conn.QueryRowxContext(ctx, `
SELECT checksum
FROM cordis_schema_migrations
WHERE service = $1 AND version = $2`, service, name).Scan(&recordedChecksum)
		switch {
		case err == nil:
			if recordedChecksum != checksum {
				return fmt.Errorf("migration %s/%s checksum changed", service, name)
			}
			continue
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("read migration %s/%s: %w", service, name, err)
		}

		tx, err := conn.BeginTxx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s/%s: %w", service, name, err)
		}
		if _, err := tx.ExecContext(ctx, string(query)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s/%s: %w", service, name, err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO cordis_schema_migrations (service, version, checksum)
VALUES ($1, $2, $3)`, service, name, checksum); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s/%s: %w", service, name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s/%s: %w", service, name, err)
		}
	}
	return nil
}

func migrationEntries(migrations fs.FS) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(migrations, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	filtered := entries[:0]
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() ||
			!strings.HasSuffix(name, ".sql") ||
			strings.HasSuffix(name, ".down.sql") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered, nil
}
