package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/clientip"
	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/pkg/socketlimit"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
	gatewayratelimit "github.com/soasurs/cordis/services/gateway/v1/ratelimit"
)

const (
	eventHello        = realtime.GatewayEventHello
	eventReady        = realtime.GatewayEventReady
	eventResumed      = realtime.GatewayEventResumed
	eventHeartbeatAck = realtime.GatewayEventHeartbeatAck
	eventError        = realtime.GatewayEventError
)

type sessionStream interface {
	Send(*sessionv1.ConnectRequest) error
	Recv() (*sessionv1.ConnectResponse, error)
	CloseSend() error
}

type Server struct {
	svcCtx          *svc.ServiceContext
	gatewayID       string
	generation      string
	checkpoints     *checkpointManager
	checkpointClose io.Closer
	tracer          trace.Tracer

	connectionsMu sync.Mutex
	connections   map[*client]struct{}
	connectionsWG sync.WaitGroup
	draining      bool
	drainDone     chan struct{}
}

type client struct {
	server               *Server
	ws                   *websocket.Conn
	cancel               context.CancelFunc
	connectionID         string
	stream               sessionStream
	streamConn           io.Closer
	sourceScope          clientip.Scope
	socketLease          socketlimit.LeaseHandle
	eventWindow          time.Time
	eventCount           int
	writeMu              sync.Mutex
	heartbeatMu          sync.Mutex
	lastHeartbeat        time.Time
	highestSequence      uint64
	acknowledgedSequence uint64
	sessionID            string
	bindingEpoch         uint64
	sessionAddress       string
}

func New(svcCtx *svc.ServiceContext) *Server {
	return newServer(svcCtx, newGRPCCheckpointSender())
}

func newServer(svcCtx *svc.ServiceContext, sender checkpointSender) *Server {
	gatewayID, generation := randomID("gw"), randomID("gen")
	server := &Server{
		svcCtx: svcCtx, gatewayID: gatewayID, generation: generation,
		tracer:      otel.Tracer(observability.GatewayInstrumentationName),
		connections: make(map[*client]struct{}),
		drainDone:   make(chan struct{}),
	}
	server.checkpoints = newCheckpointManager(
		sender, gatewayID, generation,
		svcCtx.Cfg.Gateway.CheckpointInterval(), svcCtx.Cfg.Gateway.CheckpointLimit(),
	)
	if closer, ok := sender.(io.Closer); ok {
		server.checkpointClose = closer
	}
	return server
}

func (s *Server) StartBackground(ctx context.Context) {
	go s.checkpoints.run(ctx)
}

func (s *Server) Close() error {
	if s.checkpointClose != nil {
		return s.checkpointClose.Close()
	}
	return nil
}

// Drain rejects new WebSocket upgrades, asks active clients to reconnect, and
// waits for their handlers to release Session streams.

// Drain rejects new WebSocket upgrades, asks active clients to reconnect, and
// waits for their handlers to release Session streams.
func (s *Server) Drain(ctx context.Context) error {
	s.connectionsMu.Lock()
	if s.drainDone == nil {
		s.drainDone = make(chan struct{})
	}
	if s.draining {
		done := s.drainDone
		s.connectionsMu.Unlock()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.draining = true
	connections := make([]*client, 0, len(s.connections))
	for connection := range s.connections {
		connections = append(connections, connection)
	}
	done := s.drainDone
	s.connectionsMu.Unlock()

	for _, connection := range connections {
		go func() {
			_ = connection.ws.Close(websocket.StatusServiceRestart, "gateway draining")
		}()
	}
	go func() {
		s.connectionsWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		for _, connection := range connections {
			if connection.cancel != nil {
				connection.cancel()
			}
			_ = connection.ws.CloseNow()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		return ctx.Err()
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	route := s.svcCtx.Cfg.Gateway.WebSocketRoute()
	if route == "/" {
		route = "/{$}"
	}
	mux.HandleFunc(http.MethodGet+" "+route, s.handleWebSocket)
	return mux
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !s.beginConnection() {
		http.Error(w, "gateway is draining", http.StatusServiceUnavailable)
		return
	}
	defer s.connectionsWG.Done()
	clientAddr, err := s.svcCtx.ClientIPResolver.Resolve(r.RemoteAddr, r.Header)
	if err != nil {
		http.Error(w, "invalid client address", http.StatusBadRequest)
		return
	}
	sourceScope := clientip.SourceScope(clientAddr)
	if !s.takeHTTPRateLimit(w, r,
		gatewayratelimit.PolicyForFamily(gatewayratelimit.PolicyUpgradeIP, sourceScope.Family),
		sourceScope.Key(),
	) {
		return
	}
	var lease socketlimit.LeaseHandle
	if s.svcCtx.SocketLimiter != nil {
		scopeLimit := s.svcCtx.Cfg.Gateway.IPv6PendingHandshakeLimit()
		if sourceScope.Family == clientip.FamilyIPv4 {
			scopeLimit = s.svcCtx.Cfg.Gateway.IPv4PendingHandshakeLimit()
		}
		var allowed bool
		lease, allowed = s.svcCtx.SocketLimiter.Acquire(
			sourceScope.Key(),
			s.svcCtx.Cfg.Gateway.ConnectionLimit(),
			s.svcCtx.Cfg.Gateway.PendingHandshakeLimit(),
			scopeLimit,
		)
		if !allowed {
			http.Error(w, "websocket capacity exceeded", http.StatusTooManyRequests)
			return
		}
		defer lease.Release()
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
		OriginPatterns:  s.svcCtx.Cfg.Gateway.OriginPatterns,
	})
	if err != nil {
		return
	}
	ws.SetReadLimit(s.svcCtx.Cfg.Gateway.MessageLimit())

	requestCtx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	ctx, cancel := context.WithCancel(requestCtx)
	defer cancel()
	c := &client{
		server:       s,
		ws:           ws,
		cancel:       cancel,
		connectionID: randomID("conn"),
		sourceScope:  sourceScope,
		socketLease:  lease,
	}
	if !s.trackConnection(c) {
		_ = ws.Close(websocket.StatusServiceRestart, "gateway draining")
		return
	}
	defer s.untrackConnection(c)
	c.run(ctx)
}

func (s *Server) beginConnection() bool {
	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()
	if s.draining {
		return false
	}
	s.connectionsWG.Add(1)
	return true
}

func (s *Server) trackConnection(connection *client) bool {
	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()
	if s.draining {
		return false
	}
	if s.connections == nil {
		s.connections = make(map[*client]struct{})
	}
	s.connections[connection] = struct{}{}
	return true
}

func (s *Server) untrackConnection(connection *client) {
	s.connectionsMu.Lock()
	delete(s.connections, connection)
	s.connectionsMu.Unlock()
}

func (s *Server) takeHTTPRateLimit(w http.ResponseWriter, r *http.Request, policy, key string) bool {
	if s.svcCtx.RateLimiter == nil {
		return true
	}
	decision, err := s.svcCtx.RateLimiter.Take(r.Context(), policy, key, 1)
	if err != nil {
		http.Error(w, "rate limiter unavailable", http.StatusServiceUnavailable)
		return false
	}
	if decision.Allowed {
		return true
	}
	if decision.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(max(int64(decision.RetryAfter/time.Second), 1), 10))
	}
	http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
	return false
}

func randomID(prefix string) string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return prefix + "-" + time.Now().Format("20060102150405.000000000")
	}
	return prefix + "-" + hex.EncodeToString(value[:])
}
