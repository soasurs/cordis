package server

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
)

func TestCheckpointManagerCoalescesAndBatches(t *testing.T) {
	sender := new(recordingCheckpointSender)
	manager := newCheckpointManager(sender, "gw-1", "gen-1", time.Second, 2)
	manager.record(connectionCheckpoint{
		address: "node-a", sessionID: "session-1", connectionID: "conn-1", bindingEpoch: 1, sequence: 3,
	})
	manager.record(connectionCheckpoint{
		address: "node-a", sessionID: "session-1", connectionID: "conn-1", bindingEpoch: 1, sequence: 7,
	})
	manager.record(connectionCheckpoint{
		address: "node-a", sessionID: "session-2", connectionID: "conn-2", bindingEpoch: 1, sequence: 4,
	})
	manager.record(connectionCheckpoint{
		address: "node-a", sessionID: "session-3", connectionID: "conn-3", bindingEpoch: 2, sequence: 9,
	})

	manager.flush(t.Context())

	require.Len(t, sender.requests, 2)
	var checkpoints []*sessionv1.GatewayConnectionCheckpoint
	for _, request := range sender.requests {
		require.Equal(t, "gw-1", request.GetGatewayId())
		require.Equal(t, "gen-1", request.GetGatewayGeneration())
		require.LessOrEqual(t, len(request.GetCheckpoints()), 2)
		checkpoints = append(checkpoints, request.GetCheckpoints()...)
	}
	require.Len(t, checkpoints, 3)
	require.Contains(t, checkpointSequences(checkpoints), "session-1:7")

	manager.mu.Lock()
	require.Empty(t, manager.pending)
	manager.mu.Unlock()
}

func TestCheckpointManagerRetainsFailedBatch(t *testing.T) {
	sender := &recordingCheckpointSender{err: errors.New("unavailable")}
	manager := newCheckpointManager(sender, "gw-1", "gen-1", time.Second, 500)
	manager.record(connectionCheckpoint{
		address: "node-a", sessionID: "session-1", connectionID: "conn-1", bindingEpoch: 1, sequence: 7,
	})

	manager.flush(t.Context())

	manager.mu.Lock()
	require.Len(t, manager.pending, 1)
	manager.mu.Unlock()
}

func checkpointSequences(checkpoints []*sessionv1.GatewayConnectionCheckpoint) []string {
	values := make([]string, 0, len(checkpoints))
	for _, checkpoint := range checkpoints {
		values = append(values, checkpoint.GetSessionId()+":"+
			strconv.FormatUint(checkpoint.GetAcknowledgedSequence(), 10))
	}
	return values
}

type recordingCheckpointSender struct {
	mu       sync.Mutex
	requests []*sessionv1.SyncGatewayConnectionsRequest
	err      error
}

func (s *recordingCheckpointSender) Sync(
	_ context.Context,
	_ string,
	request *sessionv1.SyncGatewayConnectionsRequest,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, request)
	return s.err
}
