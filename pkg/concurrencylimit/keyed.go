package concurrencylimit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"
)

// KeyedLimiter maintains an independent weighted semaphore for each active
// key. Entries exist only while holders or waiters reference them.
type KeyedLimiter struct {
	name     string
	capacity int64

	mu      sync.Mutex
	entries map[string]*keyedEntry
	inUse   atomic.Int64
}

type keyedEntry struct {
	sem  *semaphore.Weighted
	refs int
}

// NewKeyed constructs a process-local weighted limiter with capacity per key.
func NewKeyed(name string, capacity int64) (*KeyedLimiter, error) {
	if name == "" {
		return nil, errors.New("concurrency limiter name is required")
	}
	if capacity <= 0 {
		return nil, errors.New("concurrency limiter capacity must be positive")
	}
	return &KeyedLimiter{
		name:     name,
		capacity: capacity,
		entries:  make(map[string]*keyedEntry),
	}, nil
}

// Acquire waits for weight to become available for key or for ctx to be
// canceled. The returned release function is idempotent.
func (l *KeyedLimiter) Acquire(ctx context.Context, key string, weight int64) (func(), error) {
	if key == "" {
		return nil, errors.New("concurrency limiter key is required")
	}
	if err := validateWeight(l.capacity, weight); err != nil {
		return nil, err
	}

	entry := l.reference(key)
	start := time.Now()
	if err := entry.sem.Acquire(ctx, weight); err != nil {
		l.unreference(key, entry)
		concurrencyWait.WithLabelValues(l.name, "canceled").Observe(time.Since(start).Seconds())
		return nil, err
	}
	concurrencyWait.WithLabelValues(l.name, "acquired").Observe(time.Since(start).Seconds())
	l.inUse.Add(weight)
	concurrencyInUse.WithLabelValues(l.name).Add(float64(weight))

	var once sync.Once
	return func() {
		once.Do(func() {
			entry.sem.Release(weight)
			l.inUse.Add(-weight)
			concurrencyInUse.WithLabelValues(l.name).Sub(float64(weight))
			l.unreference(key, entry)
		})
	}, nil
}

// Capacity returns the maximum total weight held for each key.
func (l *KeyedLimiter) Capacity() int64 {
	return l.capacity
}

// InUse returns the aggregate weight currently held across all keys.
func (l *KeyedLimiter) InUse() int64 {
	return l.inUse.Load()
}

func (l *KeyedLimiter) reference(key string) *keyedEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[key]
	if entry == nil {
		entry = &keyedEntry{sem: semaphore.NewWeighted(l.capacity)}
		l.entries[key] = entry
	}
	entry.refs++
	return entry
}

func (l *KeyedLimiter) unreference(key string, entry *keyedEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry.refs--
	if entry.refs == 0 && l.entries[key] == entry {
		delete(l.entries, key)
	}
}

func validateWeight(capacity, weight int64) error {
	if weight <= 0 {
		return errors.New("concurrency limiter weight must be positive")
	}
	if weight > capacity {
		return errors.New("concurrency limiter weight exceeds capacity")
	}
	return nil
}
