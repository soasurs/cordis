package realtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayEventTypes(t *testing.T) {
	require.Equal(t, "hello", GatewayEventHello)
	require.Equal(t, "identify", GatewayEventIdentify)
	require.Equal(t, "ready", GatewayEventReady)
	require.Equal(t, "resume", GatewayEventResume)
	require.Equal(t, "resumed", GatewayEventResumed)
	require.Equal(t, "heartbeat", GatewayEventHeartbeat)
	require.Equal(t, "heartbeat.ack", GatewayEventHeartbeatAck)
	require.Equal(t, "error", GatewayEventError)
	require.Equal(t, "session.reconcile", GatewayEventReconcile)
}
