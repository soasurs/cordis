package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
)

const (
	eventHello        = realtime.GatewayEventHello
	eventReady        = realtime.GatewayEventReady
	eventResumed      = realtime.GatewayEventResumed
	eventSubscribed   = realtime.GatewayEventSubscribed
	eventHeartbeatAck = realtime.GatewayEventHeartbeatAck
	eventError        = realtime.GatewayEventError
)

type sessionStream interface {
	Send(*sessionv1.ConnectRequest) error
	Recv() (*sessionv1.ConnectResponse, error)
	CloseSend() error
}

type Server struct {
	svcCtx     *svc.ServiceContext
	gatewayID  string
	generation string
}

type client struct {
	server       *Server
	ws           *websocket.Conn
	connectionID string
	stream       sessionStream
	streamConn   io.Closer
	writeMu      sync.Mutex
}

func New(svcCtx *svc.ServiceContext) *Server {
	return &Server{
		svcCtx: svcCtx, gatewayID: randomID("gw"), generation: randomID("gen"),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc(s.svcCtx.Cfg.Gateway.WebSocketRoute(), s.handleWebSocket)
	return mux
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	ws.SetReadLimit(s.svcCtx.Cfg.Gateway.MessageLimit())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &client{
		server:       s,
		ws:           ws,
		connectionID: randomID("conn"),
	}
	c.run(ctx)
}

func (c *client) run(ctx context.Context) {
	defer c.close()
	if err := c.write(ctx, makeEnvelope(opHello, eventHello, helloData{
		HeartbeatIntervalMs: c.server.svcCtx.Cfg.Gateway.HeartbeatInterval().Milliseconds(),
		GatewayID:           c.server.gatewayID,
	})); err != nil {
		return
	}

	readCtx, cancel := context.WithTimeout(ctx, c.server.svcCtx.Cfg.Gateway.IdentifyTimeout())
	var first envelope
	err := wsjson.Read(readCtx, c.ws, &first)
	cancel()
	if err != nil {
		return
	}
	if first.Op != opIdentify && first.Op != opResume {
		_ = c.write(ctx, makeEnvelope(opError, eventError, errorData{
			Code: "handshake_required", Message: "first websocket message must be IDENTIFY or RESUME",
		}))
		return
	}

	stream, conn, err := c.connect(ctx, first)
	if err != nil {
		c.writeConnectError(ctx, first.Op, err)
		return
	}
	c.stream = stream
	c.streamConn = conn
	initial, err := c.stream.Recv()
	if err != nil {
		c.writeConnectError(ctx, first.Op, err)
		return
	}
	if err := c.writeSessionFrame(ctx, initial); err != nil {
		return
	}

	recvErr := make(chan error, 1)
	go func() {
		err := c.receiveSessionFrames(ctx)
		recvErr <- err
		// Unblock the websocket reader when the Session stream terminates.
		_ = c.ws.Close(websocket.StatusInternalError, "session stream closed")
	}()

	for {
		var msg envelope
		if err := wsjson.Read(ctx, c.ws, &msg); err != nil {
			_ = c.sendDetach(true)
			return
		}
		frame, err := c.toGatewayFrame(msg)
		if err != nil {
			_ = c.write(ctx, makeEnvelope(opError, eventError, errorData{
				Code: "operation_failed", Message: err.Error(),
			}))
			continue
		}
		if err := c.stream.Send(frame); err != nil {
			return
		}
		select {
		case err := <-recvErr:
			if err != nil {
				logx.WithContext(ctx).Errorw("receive session stream", logx.Field("error", err))
			}
			return
		default:
		}
	}
}

func (c *client) connect(ctx context.Context, first envelope) (sessionStream, io.Closer, error) {
	var (
		address string
	)
	if first.Op == opResume {
		var data resumeData
		if err := json.Unmarshal(first.D, &data); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(data.SessionID) == "" {
			return nil, nil, errors.New("session id is required")
		}
		var err error
		address, err = c.server.svcCtx.Resolver.ResolveSession(ctx, data.SessionID)
		if err != nil {
			return nil, nil, err
		}
	} else {
		var err error
		address, err = c.server.svcCtx.Resolver.ResolveNode(ctx)
		if err != nil {
			return nil, nil, err
		}
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	client := sessionv1.NewSessionServiceClient(conn)

	stream, err := client.Connect(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	frame, err := c.toGatewayFrame(first)
	if err != nil {
		_ = stream.CloseSend()
		_ = conn.Close()
		return nil, nil, err
	}
	if err := stream.Send(frame); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return stream, conn, nil
}

func (c *client) receiveSessionFrames(ctx context.Context) error {
	for {
		frame, err := c.stream.Recv()
		if err != nil {
			return err
		}
		if err := c.writeSessionFrame(ctx, frame); err != nil {
			return err
		}
		if frame.GetCloseCode() != 0 {
			return c.ws.Close(websocket.StatusCode(frame.GetCloseCode()), frame.GetCloseReason())
		}
	}
}

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
		identify.SetToken(data.Token)
		identify.SetDeviceType(data.DeviceType)
		identify.SetStatus(data.Status)
		identify.SetClientState(data.ClientState)
		frame.SetIdentify(identify)
	case opResume:
		var data resumeData
		if err := json.Unmarshal(msg.D, &data); err != nil {
			return nil, err
		}
		resume := new(sessionv1.Resume)
		resume.SetToken(data.Token)
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
		presence := new(sessionv1.PresenceUpdate)
		presence.SetStatus(data.Status)
		presence.SetClientState(data.ClientState)
		frame.SetPresence(presence)
	case opSubscribe:
		var data subscribeData
		if err := json.Unmarshal(msg.D, &data); err != nil {
			return nil, err
		}
		channelIDs := make([]int64, len(data.ChannelIDs))
		for i, value := range data.ChannelIDs {
			channelID, err := strconv.ParseInt(value, 10, 64)
			if err != nil || channelID <= 0 {
				return nil, errors.New("channel id is invalid")
			}
			channelIDs[i] = channelID
		}
		subscribe := new(sessionv1.SubscribeChannels)
		subscribe.SetChannelIds(channelIDs)
		frame.SetSubscribe(subscribe)
	default:
		return nil, errors.New("unsupported gateway op")
	}
	return frame, nil
}

func (c *client) sendDetach(resumable bool) error {
	if c.stream == nil {
		return nil
	}
	frame := new(sessionv1.ConnectRequest)
	frame.SetConnectionId(c.connectionID)
	frame.SetGatewayId(c.server.gatewayID)
	frame.SetGatewayGeneration(c.server.generation)
	detach := new(sessionv1.Detach)
	detach.SetResumable(resumable)
	frame.SetDetach(detach)
	return c.stream.Send(frame)
}

func (c *client) writeConnectError(ctx context.Context, opcode int, err error) {
	if opcode == opResume {
		_ = c.write(ctx, envelope{Op: opInvalid, D: json.RawMessage(`false`)})
		return
	}
	message := err.Error()
	if rpcStatus, ok := status.FromError(err); ok {
		message = rpcStatus.Message()
	}
	_ = c.write(ctx, makeEnvelope(opError, eventError, errorData{
		Code: "identify_failed", Message: message,
	}))
}

func (c *client) write(ctx context.Context, msg envelope) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return wsjson.Write(ctx, c.ws, msg)
}

func (c *client) writeSessionFrame(ctx context.Context, frame *sessionv1.ConnectResponse) error {
	payload := json.RawMessage(frame.GetJsonPayload())
	if len(payload) == 0 {
		payload = json.RawMessage(`null`)
	}
	if !json.Valid(payload) {
		return errors.New("session returned invalid json payload")
	}
	return c.write(ctx, envelope{
		Op: int(frame.GetOpcode()),
		S:  frame.GetSequence(),
		T:  frame.GetType(),
		D:  payload,
	})
}

func (c *client) close() {
	if c.stream != nil {
		_ = c.stream.CloseSend()
	}
	if c.streamConn != nil {
		_ = c.streamConn.Close()
	}
	_ = c.ws.Close(websocket.StatusNormalClosure, "")
}

func randomID(prefix string) string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return prefix + "-" + time.Now().Format("20060102150405.000000000")
	}
	return prefix + "-" + hex.EncodeToString(value[:])
}
