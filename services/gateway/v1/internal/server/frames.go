package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
)

func (c *client) toGatewayFrame(msg envelope) (*sessionv1.ConnectRequest, error) {
	frame := new(sessionv1.ConnectRequest)
	frame.SetConnectionId(c.connectionID)
	frame.SetGatewayId(c.server.gatewayID)
	frame.SetGatewayGeneration(c.server.generation)
	switch msg.Op {
	case opIdentify:
		var data identifyData
		if err := json.Unmarshal(msg.D, &data); err != nil {
			return nil, err
		}
		identify := new(sessionv1.Identify)
		if (strings.TrimSpace(data.Token) == "") == (strings.TrimSpace(data.GatewayTicket) == "") {
			return nil, errors.New("exactly one gateway credential is required")
		}
		if data.Token != "" {
			identify.SetToken(data.Token)
		} else {
			identify.SetGatewayTicket(data.GatewayTicket)
		}
		identify.SetDeviceType(data.DeviceType)
		statusValue, hasStatus, err := parseOptionalString(data.Status, "presence_status_invalid", "status")
		if err != nil {
			return nil, err
		}
		if hasStatus {
			if err := validatePresenceStatus(statusValue); err != nil {
				return nil, err
			}
			identify.SetStatus(statusValue)
		}
		clientState, hasClientState, err := parseOptionalString(
			data.ClientState,
			"presence_client_state_invalid",
			"client_state",
		)
		if err != nil {
			return nil, err
		}
		if hasClientState {
			if err := validateClientState(clientState); err != nil {
				return nil, err
			}
			identify.SetClientState(clientState)
		}
		frame.SetIdentify(identify)
	case opResume:
		var data resumeData
		if err := json.Unmarshal(msg.D, &data); err != nil {
			return nil, err
		}
		resume := new(sessionv1.Resume)
		if (strings.TrimSpace(data.Token) == "") == (strings.TrimSpace(data.GatewayTicket) == "") {
			return nil, errors.New("exactly one gateway credential is required")
		}
		if data.Token != "" {
			resume.SetToken(data.Token)
		} else {
			resume.SetGatewayTicket(data.GatewayTicket)
		}
		resume.SetSessionId(data.SessionID)
		resume.SetSequence(data.Sequence)
		frame.SetResume(resume)
	case opHeartbeat:
		var sequence uint64
		if len(msg.D) > 0 && string(msg.D) != "null" {
			if err := json.Unmarshal(msg.D, &sequence); err != nil {
				return nil, errors.New("heartbeat sequence is invalid")
			}
		}
		heartbeat := new(sessionv1.Heartbeat)
		heartbeat.SetSequence(sequence)
		frame.SetHeartbeat(heartbeat)
	case opPresence:
		var data presenceData
		if err := json.Unmarshal(msg.D, &data); err != nil {
			return nil, err
		}
		statusValue, hasStatus, err := parseOptionalString(data.Status, "presence_status_invalid", "status")
		if err != nil {
			return nil, err
		}
		clientState, hasClientState, err := parseOptionalString(
			data.ClientState,
			"presence_client_state_invalid",
			"client_state",
		)
		if err != nil {
			return nil, err
		}
		if !hasStatus && !hasClientState {
			return nil, newGatewayError("presence_update_empty", "presence update must include status or client_state")
		}
		presence := new(sessionv1.PresenceUpdate)
		if hasStatus {
			if err := validatePresenceStatus(statusValue); err != nil {
				return nil, err
			}
			presence.SetStatus(statusValue)
		}
		if hasClientState {
			if err := validateClientState(clientState); err != nil {
				return nil, err
			}
			presence.SetClientState(clientState)
		}
		frame.SetPresence(presence)
	default:
		return nil, errors.New("unsupported gateway op")
	}
	return frame, nil
}

func parseOptionalString(raw json.RawMessage, code, field string) (string, bool, error) {
	if len(raw) == 0 {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false, newGatewayError(code, field+" is invalid")
	}
	return value, true, nil
}

func validatePresenceStatus(value string) error {
	switch value {
	case "online", "idle", "dnd", "invisible":
		return nil
	case "offline":
		return newGatewayError("presence_status_invalid", "status cannot be offline")
	default:
		return newGatewayError("presence_status_invalid", "status is invalid")
	}
}

func validateClientState(value string) error {
	switch value {
	case "foreground", "background":
		return nil
	default:
		return newGatewayError("presence_client_state_invalid", "client_state is invalid")
	}
}

func (c *client) write(ctx context.Context, msg envelope) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal websocket message: %w", err)
	}
	payload = append(payload, '\n')
	started := time.Now()
	err = c.ws.Write(ctx, websocket.MessageText, payload)
	result := "success"
	if err != nil {
		result = "failure"
		gatewayWebSocketWriteFailuresTotal.WithLabelValues(gatewayWebSocketFailureReason(err)).Inc()
	} else {
		observeGatewayFrame("websocket_out", len(payload))
	}
	gatewayWebSocketWriteDuration.WithLabelValues(result).Observe(time.Since(started).Seconds())
	return err
}

func (c *client) read(ctx context.Context, msg *envelope) error {
	_, payload, err := c.ws.Read(ctx)
	if err != nil {
		return err
	}
	observeGatewayFrame("websocket_in", len(payload))
	if err := json.Unmarshal(payload, msg); err != nil {
		_ = c.ws.Close(websocket.StatusInvalidFramePayloadData, "failed to unmarshal JSON")
		return fmt.Errorf("unmarshal websocket message: %w", err)
	}
	return nil
}

func (c *client) sendSessionFrame(frame *sessionv1.ConnectRequest) error {
	if err := c.stream.Send(frame); err != nil {
		return err
	}
	observeGatewayFrame("session_out", proto.Size(frame))
	return nil
}

func (c *client) receiveSessionFrame() (*sessionv1.ConnectResponse, error) {
	frame, err := c.stream.Recv()
	if err != nil {
		gatewaySessionReceiveFailuresTotal.WithLabelValues(gatewaySessionFailureReason(err)).Inc()
		return nil, err
	}
	observeGatewayFrame("session_in", proto.Size(frame))
	return frame, nil
}

func (c *client) writeSessionFrame(ctx context.Context, frame *sessionv1.ConnectResponse) error {
	payload := json.RawMessage(frame.GetJsonPayload())
	if len(payload) == 0 {
		payload = json.RawMessage(`null`)
	}
	if !json.Valid(payload) {
		return errors.New("session returned invalid json payload")
	}
	if err := c.write(ctx, envelope{
		Op: int(frame.GetOpcode()),
		S:  frame.GetSequence(),
		T:  frame.GetType(),
		D:  payload,
	}); err != nil {
		return err
	}
	c.recordBindingMetadata(frame)
	return nil
}

func (c *client) recordBindingMetadata(frame *sessionv1.ConnectResponse) {
	c.heartbeatMu.Lock()
	if frame.GetSequence() > c.highestSequence {
		c.highestSequence = frame.GetSequence()
	}
	if frame.GetSessionId() != "" {
		c.sessionID = frame.GetSessionId()
	}
	if frame.GetBindingEpoch() != 0 {
		c.bindingEpoch = frame.GetBindingEpoch()
	}
	c.heartbeatMu.Unlock()
}

func (c *client) close() {
	c.heartbeatMu.Lock()
	address, sessionID, bindingEpoch := c.sessionAddress, c.sessionID, c.bindingEpoch
	c.heartbeatMu.Unlock()
	if c.server.checkpoints != nil && address != "" && sessionID != "" {
		c.server.checkpoints.remove(address, sessionID, c.connectionID, bindingEpoch)
	}
	if c.stream != nil {
		_ = c.stream.CloseSend()
	}
	if c.streamConn != nil {
		_ = c.streamConn.Close()
	}
	_ = c.ws.Close(websocket.StatusNormalClosure, "")
}
