package server

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
)

func TestReplayWindowKeepsLatestEvents(t *testing.T) {
	server := newTestServer()
	server.svcCtx.Cfg.Node.MaxReplayEvents = 3
	session := &logicalSession{
		guilds: make(map[int64]struct{}),
	}
	for range 5 {
		server.appendDispatchLocked(session, realtime.EventMessageCreated, []byte(`{}`))
	}
	require.Equal(t, uint64(5), session.sequence)
	require.Equal(t, uint64(2), session.replayFloor)
	require.Len(t, session.replay, 3)
	require.Equal(t, uint64(3), session.replay[0].sequence)
}

func TestRefreshSessionLeasesBatchesStoreAndPresence(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-a", "gateway-a", "gen-a", identify)
	require.NoError(t, err)
	fakeStore := server.svcCtx.Store.(*fakeStore)
	presence := new(batchPresence)
	server.svcCtx.PresenceClient = presence

	server.refreshSessionLeases(t.Context())

	require.Equal(t, []store.Owner{{SessionID: session.id, NodeID: server.nodeID, Generation: server.generation}}, fakeStore.batchOwners)
	require.Equal(t, 120*time.Second, fakeStore.batchOwnerTTL)
	require.Len(t, presence.requests, 1)
	require.Len(t, presence.requests[0].GetSessions(), 1)
	require.Equal(t, session.id, presence.requests[0].GetSessions()[0].GetSessionId())
}

func TestJitterDurationStaysWithinLeaseWindow(t *testing.T) {
	base := time.Minute
	for range 100 {
		got := jitterDuration(base)
		require.GreaterOrEqual(t, got, 48*time.Second)
		require.LessOrEqual(t, got, 72*time.Second)
	}
}

func TestBatchRefreshOffsetStaysWithinAssignedSlot(t *testing.T) {
	const batchCount = 100
	spread := 5 * time.Second
	slot := spread / batchCount
	for batch := range batchCount {
		offset := batchRefreshOffset(batch, batchCount, spread)
		require.GreaterOrEqual(t, offset, time.Duration(batch)*slot)
		require.Less(t, offset, time.Duration(batch+1)*slot)
	}
	require.Zero(t, batchRefreshOffset(0, 1, spread))
}

func TestRefreshSessionLeasesUsesBoundedBatches(t *testing.T) {
	server := newTestServer()
	fakeStore := server.svcCtx.Store.(*fakeStore)
	presence := new(batchPresence)
	server.svcCtx.PresenceClient = presence
	for i := range 1001 {
		session := &logicalSession{
			id: "session-" + strconv.Itoa(i), userID: int64(i + 1), guilds: make(map[int64]struct{}),
		}
		server.sessions[session.id] = session
	}

	server.refreshSessionLeasesWithSpread(t.Context(), 0)

	require.Len(t, fakeStore.ownerBatches, 3)
	require.Len(t, fakeStore.ownerBatches[0], 500)
	require.Len(t, fakeStore.ownerBatches[1], 500)
	require.Len(t, fakeStore.ownerBatches[2], 1)
	require.Len(t, presence.requests, 3)
	require.Len(t, presence.requests[0].GetSessions(), 500)
	require.Len(t, presence.requests[1].GetSessions(), 500)
	require.Len(t, presence.requests[2].GetSessions(), 1)
}

func TestResumeExpandsBindingQueueForReplay(t *testing.T) {
	server := newTestServer()
	server.svcCtx.Cfg.Node.BindingQueueSize = 1
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-a", "gateway-a", "gen-a", identify)
	require.NoError(t, err)

	session.mu.Lock()
	firstBinding := session.binding
	server.appendDispatchLocked(session, realtime.EventMessageCreated, []byte(`{"id":"1"}`))
	server.appendDispatchLocked(session, realtime.EventMessageUpdated, []byte(`{"id":"1"}`))
	session.mu.Unlock()
	server.detach(session, firstBinding, true)

	resume := new(sessionv1.Resume)
	resume.SetToken("token")
	resume.SetSessionId(session.id)
	resume.SetSequence(1)
	resumed, err := server.resume(t.Context(), "conn-b", "gateway-b", "gen-b", resume)
	require.NoError(t, err)

	resumed.mu.Lock()
	binding := resumed.binding
	resumed.mu.Unlock()
	require.Equal(t, 3, len(binding.send))
	require.Equal(t, 3, cap(binding.send))
}
