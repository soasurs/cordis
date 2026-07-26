//go:build integration

package migration

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
)

func TestMigrationsWithPostgres(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	t.Run("apply skips down migrations", func(t *testing.T) {
		files := fstest.MapFS{
			"000001_create.sql": {
				Data: []byte("CREATE TABLE migration_apply_first (id BIGINT)"),
			},
			"000002_feature.down.sql": {
				Data: []byte("DROP TABLE migration_apply_first"),
			},
			"000002_feature.up.sql": {
				Data: []byte("CREATE TABLE migration_apply_second (id BIGINT)"),
			},
		}

		require.NoError(t, Apply(t.Context(), db, files))
		requireTableExists(t, db, "migration_apply_first")
		requireTableExists(t, db, "migration_apply_second")
	})

	t.Run("apply named records and skips migrations", func(t *testing.T) {
		query := []byte("CREATE TABLE migration_named_first (id BIGINT)")
		files := fstest.MapFS{"000001_create.sql": {Data: query}}

		require.NoError(t, ApplyNamed(t.Context(), db, "named-skip", files))
		require.NoError(t, ApplyNamed(t.Context(), db, "named-skip", files))

		var checksum string
		require.NoError(t, db.GetContext(t.Context(), &checksum, `
SELECT checksum
FROM cordis_schema_migrations
WHERE service = $1 AND version = $2`, "named-skip", "000001_create.sql"))
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(query)), checksum)
	})

	t.Run("apply named rejects changed migration", func(t *testing.T) {
		const name = "000001_create.sql"
		files := fstest.MapFS{
			name: {Data: []byte("CREATE TABLE migration_named_changed (id BIGINT)")},
		}
		require.NoError(t, ApplyNamed(t.Context(), db, "named-changed", files))

		files[name] = &fstest.MapFile{
			Data: []byte("CREATE TABLE migration_named_replacement (id BIGINT)"),
		}
		err := ApplyNamed(t.Context(), db, "named-changed", files)
		require.ErrorContains(t, err, "checksum changed")
	})
}

func requireTableExists(t *testing.T, db *sqlx.DB, table string) {
	t.Helper()

	var exists bool
	require.NoError(t, db.GetContext(t.Context(), &exists, `
SELECT to_regclass($1) IS NOT NULL`, "public."+table))
	require.True(t, exists)
}
