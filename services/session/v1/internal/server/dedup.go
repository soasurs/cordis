package server

import (
	"context"
	"hash/fnv"
	"sync"
	"time"
)

const (
	dedupShardCount = 256
	dedupTTL        = 2 * time.Minute
	dedupNumGens    = 4
)

var dedupRotationInterval = dedupTTL / (dedupNumGens - 1)

const (
	routeKindGuild    uint8 = 1
	routeKindGuildMsg uint8 = 2
	routeKindUser     uint8 = 3
)

type dedupKey struct {
	routeKind uint8
	_         [7]uint8
	routeID   int64
	namespace uint64
	eventID   int64
}

type dedupGen struct {
	entries      map[dedupKey]int64
	maxExpiresAt int64
}

type dedupShard struct {
	mu   sync.Mutex
	gens [dedupNumGens]dedupGen
	head int
	len  int
}

type dedupStore struct {
	shards [dedupShardCount]dedupShard
}

func newDedupStore() *dedupStore {
	ds := &dedupStore{}
	for i := range ds.shards {
		ds.shards[i].gens[0] = dedupGen{entries: make(map[dedupKey]int64)}
		ds.shards[i].len = 1
	}
	return ds
}

func hashEventType(eventType string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(eventType))
	return h.Sum64()
}

func (ds *dedupStore) checkAndAdd(kind uint8, routeID, eventID int64, eventType string, ttl time.Duration) bool {
	if eventID == 0 {
		return true
	}
	key := dedupKey{
		routeKind: kind,
		routeID:   routeID,
		namespace: hashEventType(eventType),
		eventID:   eventID,
	}
	h := uint64(kind) ^ uint64(routeID) ^ key.namespace ^ uint64(eventID)
	shard := &ds.shards[h%dedupShardCount]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := time.Now().UnixNano()
	for i := 0; i < shard.len; i++ {
		gen := &shard.gens[(shard.head-i+dedupNumGens)%dedupNumGens]
		if exp, ok := gen.entries[key]; ok && now < exp {
			return false
		}
	}

	gen := &shard.gens[shard.head]
	expiresAt := now + int64(ttl)
	gen.entries[key] = expiresAt
	gen.maxExpiresAt = max(gen.maxExpiresAt, expiresAt)
	return true
}

func (ds *dedupStore) remove(kind uint8, routeID, eventID int64, eventType string) {
	if eventID == 0 {
		return
	}
	key := dedupKey{
		routeKind: kind,
		routeID:   routeID,
		namespace: hashEventType(eventType),
		eventID:   eventID,
	}
	h := uint64(kind) ^ uint64(routeID) ^ key.namespace ^ uint64(eventID)
	shard := &ds.shards[h%dedupShardCount]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	for i := 0; i < shard.len; i++ {
		gen := &shard.gens[(shard.head-i+dedupNumGens)%dedupNumGens]
		delete(gen.entries, key)
	}
}

func (ds *dedupStore) rotateAll() {
	now := time.Now().UnixNano()
	for i := range ds.shards {
		ds.shards[i].mu.Lock()

		shard := &ds.shards[i]
		next := (shard.head + 1) % dedupNumGens
		if shard.len == dedupNumGens && shard.gens[next].maxExpiresAt > now {
			shard.mu.Unlock()
			continue
		}

		shard.head = next
		if shard.len < dedupNumGens {
			shard.len++
		}
		shard.gens[shard.head] = dedupGen{
			entries: make(map[dedupKey]int64, 1024),
		}

		shard.mu.Unlock()
	}
}

func (ds *dedupStore) start(ctx context.Context) {
	interval := dedupRotationInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ds.rotateAll()
		}
	}
}
