package store

import (
	"context"

	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/services/message/v1/internal/eventoutbox"
)

func (s *SQLStore) EnsureMessageStream(ctx context.Context, streamKey string, shardID int) error {
	return outbox.EnsureStream(ctx, s.q, eventoutbox.MessageStreams, streamKey, shardID)
}

func (s *SQLStore) ReserveMessageSequences(ctx context.Context, streamKey string, count int) (outbox.ReservedRange, error) {
	return outbox.ReserveSequences(ctx, s.q, eventoutbox.MessageStreams, streamKey, count)
}

func (s *SQLStore) InsertMessageOutbox(ctx context.Context, records []outbox.Record) error {
	return outbox.InsertBatch(ctx, s.q, eventoutbox.MessageEvents, records)
}

func (s *SQLStore) EnsureReadStateStream(ctx context.Context, streamKey string, shardID int) error {
	return outbox.EnsureStream(ctx, s.q, eventoutbox.ReadStateStreams, streamKey, shardID)
}

func (s *SQLStore) ReserveReadStateSequences(ctx context.Context, streamKey string, count int) (outbox.ReservedRange, error) {
	return outbox.ReserveSequences(ctx, s.q, eventoutbox.ReadStateStreams, streamKey, count)
}

func (s *SQLStore) InsertReadStateOutbox(ctx context.Context, records []outbox.Record) error {
	return outbox.InsertBatch(ctx, s.q, eventoutbox.ReadStateEvents, records)
}

func (s *SQLStore) NotifyOutbox(ctx context.Context, channel string) error {
	return outbox.Notify(ctx, s.q, channel)
}
