// Package concurrencylimit provides named process-local weighted semaphores
// for protecting CPU- and database-intensive work.
package concurrencylimit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sync/semaphore"
)

var (
	concurrencyInUse = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cordis",
			Subsystem: "concurrency_limit",
			Name:      "in_use",
			Help:      "Current weight held by a named concurrency limiter.",
		},
		[]string{"limiter"},
	)
	concurrencyWait = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "cordis",
			Subsystem: "concurrency_limit",
			Name:      "wait_seconds",
			Help:      "Time spent waiting for a named concurrency limiter.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"limiter", "result"},
	)
)

// Limiter is a named weighted semaphore with usage and wait metrics.
type Limiter struct {
	name     string
	capacity int64
	sem      *semaphore.Weighted
	inUse    atomic.Int64
}

// New constructs a weighted concurrency limiter.
func New(name string, capacity int64) (*Limiter, error) {
	if name == "" {
		return nil, errors.New("concurrency limiter name is required")
	}
	if capacity <= 0 {
		return nil, errors.New("concurrency limiter capacity must be positive")
	}
	return &Limiter{
		name:     name,
		capacity: capacity,
		sem:      semaphore.NewWeighted(capacity),
	}, nil
}

// Acquire waits until weight is available or the context is canceled. The
// returned release function is idempotent.
func (l *Limiter) Acquire(ctx context.Context, weight int64) (func(), error) {
	if err := l.validateWeight(weight); err != nil {
		return nil, err
	}
	start := time.Now()
	if err := l.sem.Acquire(ctx, weight); err != nil {
		concurrencyWait.WithLabelValues(l.name, "canceled").Observe(time.Since(start).Seconds())
		return nil, err
	}
	concurrencyWait.WithLabelValues(l.name, "acquired").Observe(time.Since(start).Seconds())
	l.addUsage(weight)
	return l.releaseFunc(weight), nil
}

// TryAcquire acquires weight without waiting.
func (l *Limiter) TryAcquire(weight int64) (func(), bool) {
	if l.validateWeight(weight) != nil || !l.sem.TryAcquire(weight) {
		return nil, false
	}
	l.addUsage(weight)
	return l.releaseFunc(weight), true
}

// Capacity returns the maximum total weight held by the limiter.
func (l *Limiter) Capacity() int64 {
	return l.capacity
}

// InUse returns the currently held weight.
func (l *Limiter) InUse() int64 {
	return l.inUse.Load()
}

func (l *Limiter) validateWeight(weight int64) error {
	return validateWeight(l.capacity, weight)
}

func (l *Limiter) addUsage(weight int64) {
	l.inUse.Add(weight)
	concurrencyInUse.WithLabelValues(l.name).Add(float64(weight))
}

func (l *Limiter) releaseFunc(weight int64) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			l.sem.Release(weight)
			l.inUse.Add(-weight)
			concurrencyInUse.WithLabelValues(l.name).Sub(float64(weight))
		})
	}
}
