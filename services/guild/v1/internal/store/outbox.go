package store

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/services/guild/v1/internal/eventoutbox"
)

func (s *SQLStore) EnsureGuildStream(ctx context.Context, streamKey string, shardID int) error {
	return outbox.EnsureStream(ctx, s.q, eventoutbox.GuildStreams, streamKey, shardID)
}

func (s *SQLStore) ReserveGuildSequences(ctx context.Context, streamKey string, count int) (outbox.ReservedRange, error) {
	return outbox.ReserveSequences(ctx, s.q, eventoutbox.GuildStreams, streamKey, count)
}

func (s *SQLStore) InsertGuildOutbox(ctx context.Context, records []outbox.Record) error {
	return outbox.InsertBatch(ctx, s.q, eventoutbox.GuildEvents, records)
}

func (s *SQLStore) NotifyOutbox(ctx context.Context, channel string) error {
	if err := outbox.Notify(ctx, s.q, channel); err != nil {
		// Wakeups are a latency optimization; a notification failure must not
		// roll back the business transaction. The relay's fallback poll
		// guarantees eventual delivery.
		logx.WithContext(ctx).Errorw("notify outbox relay",
			logx.Field("channel", channel),
			logx.Field("error", err),
		)
	}
	return nil
}
