package server

import "sync"

const watermarkShardCount = 256

type watermarkKey struct {
	routeKind uint8
	routeID   int64
	channelID int64
}

type watermarkShard struct {
	mu     sync.Mutex
	values map[watermarkKey]int64
}

// watermarkStore keeps the highest stream_sequence seen per route and channel.
// It is process-local: duplicates after a node restart may redeliver, which is
// accepted behavior covered by payload-level idempotency.
type watermarkStore struct {
	shards [watermarkShardCount]watermarkShard
}

func newWatermarkStore() *watermarkStore {
	store := &watermarkStore{}
	for index := range store.shards {
		store.shards[index].values = make(map[watermarkKey]int64)
	}
	return store
}

// accept reports whether sequence is newer than the stored watermark for the
// route/channel pair. Non-positive sequences are always accepted so legacy
// events without a stream sequence keep working.
func (s *watermarkStore) accept(kind uint8, routeID, channelID, sequence int64) bool {
	if sequence <= 0 {
		return true
	}
	key := watermarkKey{routeKind: kind, routeID: routeID, channelID: channelID}
	shard := &s.shards[uint64(kind)^uint64(routeID)^uint64(channelID)%watermarkShardCount]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if current, ok := shard.values[key]; ok && sequence <= current {
		return false
	}
	shard.values[key] = sequence
	return true
}
