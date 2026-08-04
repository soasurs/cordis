package server

import (
	"context"
	"time"

	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
)

// enqueueUserEvents reserves a contiguous sequence range per recipient stream
// and inserts all drafted records in one outbox batch per stream.
func (s *userServer) enqueueUserEvents(
	ctx context.Context,
	txStore store.Store,
	drafts []userEvent,
	topic string,
	shardCount int,
	notifyChannel string,
) error {
	if len(drafts) == 0 {
		return nil
	}
	byStream := make(map[string][]userEvent, len(drafts))
	streamOrder := make([]string, 0, len(drafts))
	for _, draft := range drafts {
		if _, ok := byStream[draft.StreamKey]; !ok {
			streamOrder = append(streamOrder, draft.StreamKey)
		}
		byStream[draft.StreamKey] = append(byStream[draft.StreamKey], draft)
	}
	for _, streamKey := range streamOrder {
		group := byStream[streamKey]
		shardID := outbox.ShardID(streamKey, shardCount)
		if err := txStore.EnsureUserStream(ctx, streamKey, shardID); err != nil {
			return err
		}
		rangeValue, err := txStore.ReserveUserSequences(ctx, streamKey, len(group))
		if err != nil {
			return err
		}
		if err := s.insertUserOutbox(ctx, txStore, group, rangeValue, topic); err != nil {
			return err
		}
	}
	return txStore.NotifyOutbox(ctx, notifyChannel)
}

func (s *userServer) insertUserOutbox(
	ctx context.Context,
	txStore store.Store,
	drafts []userEvent,
	rangeValue outbox.ReservedRange,
	topic string,
) error {
	records := make([]outbox.Record, 0, len(drafts))
	now := time.Now().UnixMilli()
	for index, draft := range drafts {
		streamSequence := rangeValue.FirstSequence + int64(index)
		payload, err := finalizeEvent(draft.Payload, streamSequence, draft.DeliveryIndex)
		if err != nil {
			return err
		}
		records = append(records, outbox.Record{
			OutboxID:       s.svcCtx.Snowflake.Generate().Int64(),
			EventID:        draft.EventID,
			DeliveryIndex:  draft.DeliveryIndex,
			StreamKey:      draft.StreamKey,
			RelayShardID:   rangeValue.ShardID,
			StreamSequence: streamSequence,
			Topic:          topic,
			EventType:      draft.EventType,
			Key:            draft.Key,
			Payload:        payload,
			TraceContext:   kafka.MarshalTraceContext(ctx),
			CreatedAt:      now,
		})
	}
	return txStore.InsertUserOutbox(ctx, records)
}
