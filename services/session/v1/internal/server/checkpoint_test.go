package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
)

func TestSyncGatewayConnectionsAppliesCurrentBinding(t *testing.T) {
	binding := newBinding("conn-1", 3, 1)
	session := &logicalSession{
		id: "session-1", gatewayID: "gw-1", gatewayGeneration: "gen-1",
		sequence: 8, binding: binding, bindingEpoch: 3,
		replay: []replayEntry{{sequence: 4}, {sequence: 7}, {sequence: 8}},
	}
	server := &Server{sessions: map[string]*logicalSession{"session-1": session}}
	req := new(sessionv1.SyncGatewayConnectionsRequest)
	req.SetGatewayId("gw-1")
	req.SetGatewayGeneration("gen-1")
	req.SetCheckpoints([]*sessionv1.GatewayConnectionCheckpoint{
		newCheckpoint("session-1", "conn-1", 3, 7),
		newCheckpoint("session-1", "stale-conn", 2, 8),
	})

	resp, err := server.SyncGatewayConnections(t.Context(), req)

	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetApplied())
	require.Equal(t, uint64(7), session.ackedSequence)
	require.Len(t, session.replay, 1)
	require.Equal(t, uint64(8), session.replay[0].sequence)
}

func TestSyncGatewayConnectionsRejectsSequenceAhead(t *testing.T) {
	binding := newBinding("conn-1", 1, 1)
	session := &logicalSession{
		id: "session-1", gatewayID: "gw-1", gatewayGeneration: "gen-1",
		sequence: 8, binding: binding, bindingEpoch: 1,
	}
	server := &Server{sessions: map[string]*logicalSession{"session-1": session}}
	req := new(sessionv1.SyncGatewayConnectionsRequest)
	req.SetGatewayId("gw-1")
	req.SetGatewayGeneration("gen-1")
	req.SetCheckpoints([]*sessionv1.GatewayConnectionCheckpoint{
		newCheckpoint("session-1", "conn-1", 1, 9),
	})

	resp, err := server.SyncGatewayConnections(t.Context(), req)

	require.NoError(t, err)
	require.Zero(t, resp.GetApplied())
	require.Zero(t, session.ackedSequence)
}

func newCheckpoint(sessionID, connectionID string, epoch, sequence uint64) *sessionv1.GatewayConnectionCheckpoint {
	checkpoint := new(sessionv1.GatewayConnectionCheckpoint)
	checkpoint.SetSessionId(sessionID)
	checkpoint.SetConnectionId(connectionID)
	checkpoint.SetBindingEpoch(epoch)
	checkpoint.SetAcknowledgedSequence(sequence)
	return checkpoint
}
