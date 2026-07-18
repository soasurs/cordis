// Package socketlimit provides process-local WebSocket capacity limits.
package socketlimit

import "sync"

// LeaseHandle counts one accepted socket and its pending handshake state.
type LeaseHandle interface {
	MarkReady()
	Release()
}

// Limiter reserves local socket and pending-handshake capacity.
type Limiter interface {
	Acquire(scope string, maxConnections, maxPending, maxPendingForScope int64) (LeaseHandle, bool)
}

// Manager maintains exact process-local counts. Active source keys are removed
// as soon as their final pending handshake becomes ready or disconnects.
type Manager struct {
	mu          sync.Mutex
	connections int64
	pending     int64
	scopes      map[string]int64
}

// NewManager constructs a local socket capacity manager.
func NewManager() *Manager {
	return &Manager{scopes: make(map[string]int64)}
}

// Acquire reserves one total-connection slot and one pending-handshake slot.
func (m *Manager) Acquire(
	scope string,
	maxConnections, maxPending, maxPendingForScope int64,
) (LeaseHandle, bool) {
	if scope == "" || maxConnections <= 0 || maxPending <= 0 || maxPendingForScope <= 0 {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connections >= maxConnections ||
		m.pending >= maxPending ||
		m.scopes[scope] >= maxPendingForScope {
		return nil, false
	}
	m.connections++
	m.pending++
	m.scopes[scope]++
	return &Lease{manager: m, scope: scope, pending: true}, true
}

// Lease represents one accepted local socket.
type Lease struct {
	manager  *Manager
	scope    string
	pending  bool
	released bool
}

// MarkReady removes the socket from pending-handshake limits while retaining
// its total connection slot.
func (l *Lease) MarkReady() {
	l.manager.mu.Lock()
	defer l.manager.mu.Unlock()
	if l.released || !l.pending {
		return
	}
	l.pending = false
	l.manager.releasePending(l.scope)
}

// Release frees the socket and any pending-handshake slot exactly once.
func (l *Lease) Release() {
	l.manager.mu.Lock()
	defer l.manager.mu.Unlock()
	if l.released {
		return
	}
	l.released = true
	l.manager.connections--
	if l.pending {
		l.pending = false
		l.manager.releasePending(l.scope)
	}
}

func (m *Manager) releasePending(scope string) {
	m.pending--
	m.scopes[scope]--
	if m.scopes[scope] == 0 {
		delete(m.scopes, scope)
	}
}
