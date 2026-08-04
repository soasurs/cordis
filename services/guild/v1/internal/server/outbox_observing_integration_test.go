//go:build integration

package server

import (
	"context"

	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

type outboxObservingStore struct {
	store.Store
	observe func([]outbox.Record)
}

func (s *outboxObservingStore) Transact(ctx context.Context, fn func(txStore store.Store) error) error {
	return s.Store.Transact(ctx, func(tx store.Store) error {
		return fn(&outboxObservingStore{Store: tx, observe: s.observe})
	})
}

func (s *outboxObservingStore) InsertGuildOutbox(ctx context.Context, records []outbox.Record) error {
	if err := s.Store.InsertGuildOutbox(ctx, records); err != nil {
		return err
	}
	if s.observe != nil {
		s.observe(records)
	}
	return nil
}
