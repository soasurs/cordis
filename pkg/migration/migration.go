package migration

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
)

func Apply(ctx context.Context, db *sqlx.DB, migrations fs.FS) error {
	entries, err := fs.ReadDir(migrations, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() ||
			!strings.HasSuffix(name, ".sql") ||
			strings.HasSuffix(name, ".down.sql") {
			continue
		}

		query, err := fs.ReadFile(migrations, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := db.ExecContext(ctx, string(query)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}
