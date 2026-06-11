package server

import "encoding/json"

const (
	opDispatch     = 0
	opHeartbeat    = 1
	opIdentify     = 2
	opPresence     = 3
	opSubscribe    = 4
	opHello        = 10
	opHeartbeatAck = 11
	opError        = 4000
)

type envelope struct {
	Op int             `json:"op"`
	T  string          `json:"t,omitempty"`
	D  json.RawMessage `json:"d,omitempty"`
}

type helloData struct {
	HeartbeatIntervalMs int64  `json:"heartbeat_interval_ms"`
	GatewayID           string `json:"gateway_id"`
}

type identifyData struct {
	Token       string `json:"token"`
	DeviceType  string `json:"device_type,omitempty"`
	Status      string `json:"status,omitempty"`
	ClientState string `json:"client_state,omitempty"`
}

type readyData struct {
	UserID               int64  `json:"user_id"`
	AuthSessionID        int64  `json:"auth_session_id"`
	GatewaySessionID     string `json:"gateway_session_id"`
	GatewayID            string `json:"gateway_id"`
	HeartbeatIntervalMs  int64  `json:"heartbeat_interval_ms"`
	AccessTokenExpiresAt int64  `json:"access_token_expires_at"`
}

type heartbeatAckData struct {
	UserID           int64  `json:"user_id"`
	GatewaySessionID string `json:"gateway_session_id"`
}

type presenceData struct {
	Status      string `json:"status,omitempty"`
	ClientState string `json:"client_state,omitempty"`
}

type subscribeData struct {
	ChannelIDs []int64 `json:"channel_ids"`
}

type subscribedData struct {
	ChannelIDs []int64 `json:"channel_ids"`
}

type errorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func makeEnvelope(op int, event string, data any) envelope {
	raw, err := json.Marshal(data)
	if err != nil {
		raw = json.RawMessage(`null`)
	}
	return envelope{Op: op, T: event, D: raw}
}
