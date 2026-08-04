package server

import (
	"context"
	"time"

	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/services/guild/v1/internal/eventoutbox"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func (s *guildServer) enqueueEvents(ctx context.Context, txStore store.Store, events []guildEvent) error {
	return s.enqueueGuildEvents(
		ctx,
		txStore,
		events,
		s.svcCtx.Cfg.Kafka.EventTopic(),
		s.svcCtx.Cfg.Outbox.Shards(),
		eventoutbox.GuildNotifyChannel,
	)
}

// enqueueGuildEvents reserves a contiguous sequence range per guild stream
// and inserts all drafted records in one outbox batch per stream.
func (s *guildServer) enqueueGuildEvents(
	ctx context.Context,
	txStore store.Store,
	events []guildEvent,
	topic string,
	shardCount int,
	notifyChannel string,
) error {
	if len(events) == 0 {
		return nil
	}
	byStream := make(map[string][]guildEvent, len(events))
	streamOrder := make([]string, 0, len(events))
	for _, event := range events {
		if _, ok := byStream[event.StreamKey]; !ok {
			streamOrder = append(streamOrder, event.StreamKey)
		}
		byStream[event.StreamKey] = append(byStream[event.StreamKey], event)
	}
	for _, streamKey := range streamOrder {
		group := byStream[streamKey]
		shardID := outbox.ShardID(streamKey, shardCount)
		if err := txStore.EnsureGuildStream(ctx, streamKey, shardID); err != nil {
			return err
		}
		rangeValue, err := txStore.ReserveGuildSequences(ctx, streamKey, len(group))
		if err != nil {
			return err
		}
		if err := s.insertGuildOutbox(ctx, txStore, group, rangeValue, topic); err != nil {
			return err
		}
	}
	return txStore.NotifyOutbox(ctx, notifyChannel)
}

func (s *guildServer) insertGuildOutbox(
	ctx context.Context,
	txStore store.Store,
	events []guildEvent,
	rangeValue outbox.ReservedRange,
	topic string,
) error {
	records := make([]outbox.Record, 0, len(events))
	now := time.Now().UnixMilli()
	accessRevisions := make(map[int64]int64, len(events))
	for index, event := range events {
		streamSequence := rangeValue.FirstSequence + int64(index)
		payload := event.Payload
		if event.Type != EventTypeGuildDeleted {
			accessRevision, ok := accessRevisions[event.GuildID]
			if !ok {
				guild, err := txStore.GetGuild(ctx, event.GuildID)
				if err != nil {
					return err
				}
				accessRevision = guild.AccessRevision
				accessRevisions[event.GuildID] = accessRevision
			}
			var err error
			payload, err = addEventAccessRevision(payload, accessRevision)
			if err != nil {
				return err
			}
		}
		payload, err := finalizeEvent(payload, streamSequence, event.DeliveryIndex)
		if err != nil {
			return err
		}
		records = append(records, outbox.Record{
			OutboxID:       s.svcCtx.Snowflake.Generate().Int64(),
			EventID:        event.EventID,
			DeliveryIndex:  event.DeliveryIndex,
			StreamKey:      event.StreamKey,
			RelayShardID:   rangeValue.ShardID,
			StreamSequence: streamSequence,
			Topic:          topic,
			EventType:      event.Type,
			Key:            event.Key,
			Payload:        payload,
			TraceContext:   kafka.MarshalTraceContext(ctx),
			CreatedAt:      now,
		})
	}
	return txStore.InsertGuildOutbox(ctx, records)
}
