//go:build integration

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/guild/v1/config"
	guildmigrations "github.com/soasurs/cordis/services/guild/v1/db/migrations"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
	"github.com/soasurs/cordis/services/guild/v1/internal/svc"
)

type failingOutboxStore struct {
	store.Store
}

func (s *failingOutboxStore) Transact(ctx context.Context, fn func(txStore store.Store) error) error {
	return s.Store.Transact(ctx, func(tx store.Store) error {
		return fn(&failingOutboxStore{Store: tx})
	})
}

func (s *failingOutboxStore) InsertGuildOutbox(_ context.Context, _ []outbox.Record) error {
	return errors.New("outbox unavailable")
}

func TestCreateGuildOutboxFailureRollsBackWithPostgres(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	migrationDB, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, migrationDB.Close()) })
	db, err := database.NewPostgresPool(t.Context(), database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, migration.Apply(t.Context(), migrationDB, guildmigrations.Files))

	node, err := snowflake.New()
	require.NoError(t, err)
	guildStore := &failingOutboxStore{Store: store.New(db)}
	service := New(svc.NewServiceContextWithDependencies(config.Config{}, svc.Dependencies{
		Store:       guildStore,
		Snowflake:   node,
		Cursors:     testCursorCodec(t),
		UserClient:  &fakeUserClient{},
		MediaClient: &unusedMediaClient{},
	}))

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")
	_, err = service.CreateGuild(t.Context(), req)
	require.Error(t, err)

	var count int64
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM guilds WHERE owner_id = $1`, 1001).Scan(&count))
	require.Zero(t, count, "guild creation must roll back when the outbox insert fails")
}
