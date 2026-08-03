package kafka

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestPublishBatchWithResultsPreservesInputOrder(t *testing.T) {
	producer := &reorderingProducer{reversed: true, failIndex: -1}
	publisher := &Publisher{producer: producer, topic: "events"}

	results := publisher.PublishBatchWithResults(t.Context(), []Record{
		{ID: 100, Key: []byte("a"), Payload: []byte("1")},
		{ID: 200, Key: []byte("b"), Payload: []byte("2")},
		{ID: 300, Key: []byte("c"), Payload: []byte("3")},
	})

	require.Len(t, results, 3)
	require.Equal(t, int64(100), results[0].ID)
	require.Equal(t, int32(2), results[0].Partition)
	require.Equal(t, int64(12), results[0].Offset)
	require.NoError(t, results[0].Err)

	require.Equal(t, int64(200), results[1].ID)
	require.Equal(t, int32(1), results[1].Partition)
	require.Equal(t, int64(11), results[1].Offset)
	require.NoError(t, results[1].Err)

	require.Equal(t, int64(300), results[2].ID)
	require.Equal(t, int32(0), results[2].Partition)
	require.Equal(t, int64(10), results[2].Offset)
	require.NoError(t, results[2].Err)
}

func TestPublishBatchWithResultsReturnsPerRecordErrors(t *testing.T) {
	producer := &reorderingProducer{reversed: true, failIndex: 1}
	publisher := &Publisher{producer: producer, topic: "events"}

	results := publisher.PublishBatchWithResults(t.Context(), []Record{
		{ID: 100, Key: []byte("a")},
		{ID: 200, Key: []byte("b")},
	})

	require.Len(t, results, 2)
	require.EqualError(t, results[0].Err, "kafka unavailable")
	require.NoError(t, results[1].Err)
}

func TestPublishBatchReturnsFirstError(t *testing.T) {
	producer := &reorderingProducer{reversed: true, failIndex: 1}
	publisher := &Publisher{producer: producer, topic: "events"}

	err := publisher.PublishBatch(t.Context(), []Record{
		{ID: 100, Key: []byte("a")},
		{ID: 200, Key: []byte("b")},
	})

	require.EqualError(t, err, "kafka unavailable")
}

type reorderingProducer struct {
	reversed  bool
	failIndex int
}

func (p *reorderingProducer) ProduceSync(_ context.Context, records ...*kgo.Record) kgo.ProduceResults {
	ordered := append([]*kgo.Record(nil), records...)
	if p.reversed {
		for left, right := 0, len(ordered)-1; left < right; left, right = left+1, right-1 {
			ordered[left], ordered[right] = ordered[right], ordered[left]
		}
	}
	results := make(kgo.ProduceResults, 0, len(ordered))
	for index, record := range ordered {
		record.Partition = int32(index)
		record.Offset = int64(index + 10)
		result := kgo.ProduceResult{Record: record}
		if p.failIndex >= 0 && index == p.failIndex {
			result.Err = errors.New("kafka unavailable")
		}
		results = append(results, result)
	}
	return results
}
