package server

import (
	"context"
	"time"

	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

// enqueueMessageEvents reserves a contiguous sequence range for one channel
// stream and inserts all drafted records in one outbox batch.
func (s *messageServer) enqueueMessageEvents(
	ctx context.Context,
	txStore store.Store,
	drafts []messageEvent,
	topic string,
	shardCount int,
	notifyChannel string,
) error {
	if len(drafts) == 0 {
		return nil
	}
	streamKey := drafts[0].StreamKey
	shardID := outbox.ShardID(streamKey, shardCount)
	if err := txStore.EnsureMessageStream(ctx, streamKey, shardID); err != nil {
		return err
	}
	rangeValue, err := txStore.ReserveMessageSequences(ctx, streamKey, len(drafts))
	if err != nil {
		return err
	}
	return s.insertMessageOutbox(ctx, txStore, drafts, rangeValue, topic, notifyChannel)
}

// enqueueReadStateEvent reserves one sequence for a read-state stream and
// inserts the drafted record.
func (s *messageServer) enqueueReadStateEvent(
	ctx context.Context,
	txStore store.Store,
	draft messageEvent,
	topic string,
	shardCount int,
	notifyChannel string,
) error {
	shardID := outbox.ShardID(draft.StreamKey, shardCount)
	if err := txStore.EnsureReadStateStream(ctx, draft.StreamKey, shardID); err != nil {
		return err
	}
	rangeValue, err := txStore.ReserveReadStateSequences(ctx, draft.StreamKey, 1)
	if err != nil {
		return err
	}
	return s.insertReadStateOutbox(ctx, txStore, []messageEvent{draft}, rangeValue, topic, notifyChannel)
}

func (s *messageServer) insertMessageOutbox(
	ctx context.Context,
	txStore store.Store,
	drafts []messageEvent,
	rangeValue outbox.ReservedRange,
	topic, notifyChannel string,
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
			CreatedAt:      now,
		})
	}
	if err := txStore.InsertMessageOutbox(ctx, records); err != nil {
		return err
	}
	return txStore.NotifyOutbox(ctx, notifyChannel)
}

func (s *messageServer) insertReadStateOutbox(
	ctx context.Context,
	txStore store.Store,
	drafts []messageEvent,
	rangeValue outbox.ReservedRange,
	topic, notifyChannel string,
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
			CreatedAt:      now,
		})
	}
	if err := txStore.InsertReadStateOutbox(ctx, records); err != nil {
		return err
	}
	return txStore.NotifyOutbox(ctx, notifyChannel)
}
