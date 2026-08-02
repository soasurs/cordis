package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToGatewayFramePresenceUpdate(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1", generation: "gen-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opPresence,
		D:  json.RawMessage(`{"status":"online","client_state":"background"}`),
	})
	require.NoError(t, err)
	require.Equal(t, "conn-1", frame.GetConnectionId())
	require.Equal(t, "gw-1", frame.GetGatewayId())
	require.Equal(t, "gen-1", frame.GetGatewayGeneration())
	require.Equal(t, "online", frame.GetPresence().GetStatus())
	require.Equal(t, "background", frame.GetPresence().GetClientState())
	require.True(t, frame.GetPresence().HasStatus())
	require.True(t, frame.GetPresence().HasClientState())
}

func TestToGatewayFramePresenceUpdatePreservesFieldPresence(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1", generation: "gen-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opPresence,
		D:  json.RawMessage(`{"status":"idle"}`),
	})
	require.NoError(t, err)
	require.True(t, frame.GetPresence().HasStatus())
	require.False(t, frame.GetPresence().HasClientState())
}

func TestToGatewayFrameRejectsInvalidPresenceValues(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1", generation: "gen-1"}}
	tests := []struct {
		name string
		data string
		code string
	}{
		{name: "empty update", data: `{}`, code: "presence_update_empty"},
		{name: "empty status", data: `{"status":""}`, code: "presence_status_invalid"},
		{name: "null status", data: `{"status":null}`, code: "presence_status_invalid"},
		{name: "non-string status", data: `{"status":1}`, code: "presence_status_invalid"},
		{name: "offline", data: `{"status":"offline"}`, code: "presence_status_invalid"},
		{name: "unknown status", data: `{"status":"away"}`, code: "presence_status_invalid"},
		{name: "empty client state", data: `{"client_state":""}`, code: "presence_client_state_invalid"},
		{name: "null client state", data: `{"client_state":null}`, code: "presence_client_state_invalid"},
		{name: "non-string client state", data: `{"client_state":true}`, code: "presence_client_state_invalid"},
		{name: "unknown client state", data: `{"client_state":"mobile"}`, code: "presence_client_state_invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.toGatewayFrame(envelope{Op: opPresence, D: json.RawMessage(tt.data)})
			require.Error(t, err)
			require.Equal(t, tt.code, operationErrorData(err).Code)
		})
	}
}

func TestToGatewayFrameIdentifyPresenceDefaultsRemainAbsent(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1", generation: "gen-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opIdentify,
		D:  json.RawMessage(`{"token":"access-token"}`),
	})
	require.NoError(t, err)
	require.False(t, frame.GetIdentify().HasStatus())
	require.False(t, frame.GetIdentify().HasClientState())
}

func TestToGatewayFrameRejectsInvalidIdentifyPresence(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1", generation: "gen-1"}}
	tests := []struct {
		name string
		data string
		code string
	}{
		{name: "empty status", data: `{"token":"access-token","status":""}`, code: "presence_status_invalid"},
		{name: "null status", data: `{"token":"access-token","status":null}`, code: "presence_status_invalid"},
		{name: "non-string status", data: `{"token":"access-token","status":1}`, code: "presence_status_invalid"},
		{name: "offline", data: `{"token":"access-token","status":"offline"}`, code: "presence_status_invalid"},
		{name: "unknown status", data: `{"token":"access-token","status":"away"}`, code: "presence_status_invalid"},
		{name: "empty client state", data: `{"token":"access-token","client_state":""}`, code: "presence_client_state_invalid"},
		{name: "null client state", data: `{"token":"access-token","client_state":null}`, code: "presence_client_state_invalid"},
		{name: "non-string client state", data: `{"token":"access-token","client_state":true}`, code: "presence_client_state_invalid"},
		{name: "unknown client state", data: `{"token":"access-token","client_state":"mobile"}`, code: "presence_client_state_invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.toGatewayFrame(envelope{Op: opIdentify, D: json.RawMessage(tt.data)})
			require.Error(t, err)
			var gatewayErr *gatewayError
			require.ErrorAs(t, err, &gatewayErr)
			require.Equal(t, tt.code, gatewayErr.code)
		})
	}
}

func TestToGatewayFrameResume(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1", generation: "gen-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opResume,
		D:  json.RawMessage(`{"token":"access-token","session_id":"sess-1","seq":42}`),
	})
	require.NoError(t, err)
	require.Equal(t, "access-token", frame.GetResume().GetToken())
	require.Equal(t, "sess-1", frame.GetResume().GetSessionId())
	require.Equal(t, uint64(42), frame.GetResume().GetSequence())
}

func TestToGatewayFrameGatewayTicket(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1", generation: "gen-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opIdentify,
		D:  json.RawMessage(`{"gateway_ticket":"ticket"}`),
	})
	require.NoError(t, err)
	require.Equal(t, "ticket", frame.GetIdentify().GetGatewayTicket())
	require.Empty(t, frame.GetIdentify().GetToken())
}

func TestToGatewayFrameRejectsAmbiguousCredential(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opIdentify,
		D:  json.RawMessage(`{"token":"access","gateway_ticket":"ticket"}`),
	})
	require.Error(t, err)
}

func TestToGatewayFrameResumeInvalidJSON(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opResume,
		D:  json.RawMessage(`invalid`),
	})
	require.Error(t, err)
}

func TestToGatewayFrameIdentifyInvalidJSON(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opIdentify,
		D:  json.RawMessage(`invalid`),
	})
	require.Error(t, err)
}

func TestToGatewayFrameHeartbeatNullD(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opHeartbeat,
		D:  json.RawMessage(`null`),
	})
	require.NoError(t, err)
	require.NotNil(t, frame.GetHeartbeat())
	require.Equal(t, uint64(0), frame.GetHeartbeat().GetSequence())
}

func TestToGatewayFrameHeartbeatEmptyD(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opHeartbeat,
	})
	require.NoError(t, err)
	require.NotNil(t, frame.GetHeartbeat())
	require.Equal(t, uint64(0), frame.GetHeartbeat().GetSequence())
}

func TestToGatewayFrameHeartbeatWithSequence(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opHeartbeat,
		D:  json.RawMessage(`42`),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(42), frame.GetHeartbeat().GetSequence())
}

func TestToGatewayFrameHeartbeatInvalidD(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opHeartbeat,
		D:  json.RawMessage(`{"x":1}`),
	})
	require.Error(t, err)
}

func TestToGatewayFrameUnknownOpcode(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: 99,
		D:  json.RawMessage(`{}`),
	})
	require.Error(t, err)
}

func TestToGatewayFrameRejectsRemovedSubscribeOpcode(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: 4,
		D:  json.RawMessage(`{"channel_ids":["42"]}`),
	})
	require.Error(t, err)
}

func TestToGatewayFramePresenceInvalidJSON(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opPresence,
		D:  json.RawMessage(`invalid`),
	})
	require.Error(t, err)
}
