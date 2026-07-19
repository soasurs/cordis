package server

import "encoding/json"

const (
	opDispatch     = 0
	opHeartbeat    = 1
	opIdentify     = 2
	opPresence     = 3
	opResume       = 6
	opReconnect    = 7
	opInvalid      = 9
	opHello        = 10
	opHeartbeatAck = 11
	opError        = 4000
)

type envelope struct {
	Op int             `json:"op"`
	S  uint64          `json:"s,omitempty"`
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

type resumeData struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Sequence  uint64 `json:"seq"`
}

type presenceData struct {
	Status      string `json:"status,omitempty"`
	ClientState string `json:"client_state,omitempty"`
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
