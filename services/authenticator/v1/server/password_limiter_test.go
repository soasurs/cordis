package server

import (
	"context"
	"sync"
)

type recordingPasswordLimiter struct {
	mu       sync.Mutex
	calls    int
	releases int
	err      error
}

func (l *recordingPasswordLimiter) Acquire(_ context.Context, _ int64) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	l.calls++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.releases++
			l.mu.Unlock()
		})
	}, nil
}

func (l *recordingPasswordLimiter) snapshot() (int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls, l.releases
}
