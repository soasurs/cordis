package server

import (
	"context"
	"sync"
	"time"
)

const (
	watermarkShardCount = 256
	watermarkTTL        = time.Hour
	watermarkCleanup    = 10 * time.Minute
)

type watermarkKey struct {
	routeKind uint8
	routeID   int64
	channelID int64
}

type watermarkShard struct {
	mu     sync.Mutex
	values map[watermarkKey]watermarkEntry
}

type watermarkEntry struct {
	sequence  int64
	expiresAt int64
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
		store.shards[index].values = make(map[watermarkKey]watermarkEntry)
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
	shard := &s.shards[(uint64(kind)^uint64(routeID)^uint64(channelID))%watermarkShardCount]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	now := time.Now().UnixNano()
	if current, ok := shard.values[key]; ok && now < current.expiresAt && sequence <= current.sequence {
		return false
	}
	shard.values[key] = watermarkEntry{
		sequence:  sequence,
		expiresAt: now + int64(watermarkTTL),
	}
	return true
}

// start periodically removes expired entries so the store stays bounded.
func (s *watermarkStore) start(ctx context.Context) {
	ticker := time.NewTicker(watermarkCleanup)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

func (s *watermarkStore) cleanup() {
	now := time.Now().UnixNano()
	for index := range s.shards {
		shard := &s.shards[index]
		shard.mu.Lock()
		for key, entry := range shard.values {
			if now >= entry.expiresAt {
				delete(shard.values, key)
			}
		}
		shard.mu.Unlock()
	}
}
