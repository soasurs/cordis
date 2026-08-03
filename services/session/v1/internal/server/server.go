package server

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
	"github.com/soasurs/cordis/services/session/v1/internal/svc"
)

const (
	opDispatch     = 0
	opHeartbeatAck = 11
	opInvalid      = 9
	leaseJitter    = 0.2
	leaseBatchSize = 500
)

type replayEntry struct {
	sequence uint64
	frame    *sessionv1.ConnectResponse
}

type pendingDispatch struct {
	eventType string
	payload   []byte
}

type leaseRefreshOutcome struct {
	ownerFailures       int
	presenceFailures    int
	ownerErrorType      string
	presenceErrorType   string
	completedBatchCount int
}

type binding struct {
	id    string
	epoch uint64
	send  chan *sessionv1.ConnectResponse
	done  chan struct{}
	once  sync.Once
}

func (b *binding) close() {
	b.once.Do(func() { close(b.done) })
}

type logicalSession struct {
	mu sync.Mutex

	id                      string
	userID                  int64
	authSessionID           int64
	gatewayID               string
	gatewayGeneration       string
	deviceType              string
	initialStatus           presencev1.PresenceStatus
	hasInitialStatus        bool
	clientState             presencev1.ClientState
	sequence                uint64
	ackedSequence           uint64
	replay                  []replayEntry
	replayFloor             uint64
	guilds                  map[int64]struct{}
	binding                 *binding
	bindingEpoch            uint64
	detachedAt              time.Time
	presenceWindow          time.Time
	presenceUpdates         int
	initializing            bool
	pendingDispatches       []pendingDispatch
	pendingDispatchBytes    int64
	pendingDispatchOverflow bool
}

// Server implements SessionService: it owns logical gateway sessions, presence
// fan-out, guild visibility snapshots, and node routing.
type Server struct {
	sessionv1.UnimplementedSessionServiceServer

	svcCtx     *svc.ServiceContext
	nodeID     string
	generation string
	rpcAddress string

	mu       sync.RWMutex
	sessions map[string]*logicalSession
	users    map[int64]map[*logicalSession]struct{}
	guilds   map[int64]map[*logicalSession]struct{}
	draining atomic.Bool

	visibilityMu        sync.RWMutex
	visibilityUsers     map[int64]*userVisibilityState
	visibilityReloads   singleflight.Group
	visibilityReloadSem *semaphore.Weighted

	routeMu         sync.Mutex
	publishedRoutes map[store.Route]struct{}
	tracer          trace.Tracer

	dedup      *dedupStore
	watermarks *watermarkStore
}

// New creates a Session Server from the service context, falling back to the
// hostname and listen address when node identity is unset.
func New(svcCtx *svc.ServiceContext) *Server {
	nodeID := strings.TrimSpace(svcCtx.Cfg.Node.ID)
	if nodeID == "" {
		nodeID, _ = os.Hostname()
	}
	if nodeID == "" {
		nodeID = randomID("session-node")
	}
	rpcAddress := strings.TrimSpace(svcCtx.Cfg.Node.AdvertiseAddress)
	if rpcAddress == "" {
		rpcAddress = svcCtx.Cfg.ListenOn
	}
	return &Server{
		svcCtx:              svcCtx,
		nodeID:              nodeID,
		generation:          randomID("gen"),
		rpcAddress:          rpcAddress,
		sessions:            make(map[string]*logicalSession),
		users:               make(map[int64]map[*logicalSession]struct{}),
		guilds:              make(map[int64]map[*logicalSession]struct{}),
		visibilityUsers:     make(map[int64]*userVisibilityState),
		visibilityReloadSem: semaphore.NewWeighted(svcCtx.Cfg.Node.SnapshotReloadLimit()),
		publishedRoutes:     make(map[store.Route]struct{}),
		dedup:               newDedupStore(),
		watermarks:          newWatermarkStore(),
	}
}

// StartBackground runs node registration, route publication, lease renewal,
// detached-session cleanup, and dedup GC until ctx is canceled.
func (s *Server) StartBackground(ctx context.Context) {
	go s.refreshNode(ctx)
	go s.refreshRoutes(ctx)
	go s.refreshSessionLeaseLoop(ctx)
	go s.cleanupDetached(ctx)
	go s.dedup.start(ctx)
	go s.watermarks.start(ctx)
}

// Drain marks the node as draining, asks every bound session to reconnect, and
// returns when ctx is canceled.
func (s *Server) Drain(ctx context.Context) {
	if !s.draining.CompareAndSwap(false, true) {
		return
	}
	_ = s.registerNode(ctx, sessionregistry.StatusDraining)

	s.mu.RLock()
	sessions := make([]*logicalSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.mu.RUnlock()

	var interval time.Duration
	if len(sessions) > 0 {
		interval = s.svcCtx.Cfg.Node.DrainWindow() / time.Duration(len(sessions))
	}
	for _, session := range sessions {
		session.mu.Lock()
		if session.binding != nil {
			frame := new(sessionv1.ConnectResponse)
			frame.SetOpcode(opInvalid)
			frame.SetJsonPayload(`false`)
			frame.SetCloseCode(1012)
			frame.SetCloseReason("session node draining")
			_ = enqueue(session.binding, frame)
		}
		session.mu.Unlock()
		if interval > 0 {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

// SyncGatewayConnections applies gateway connection checkpoints that still
// match the current binding and returns the number applied.
func (s *Server) SyncGatewayConnections(
	_ context.Context,
	req *sessionv1.SyncGatewayConnectionsRequest,
) (*sessionv1.SyncGatewayConnectionsResponse, error) {
	if strings.TrimSpace(req.GetGatewayId()) == "" || strings.TrimSpace(req.GetGatewayGeneration()) == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway id and generation are required")
	}

	var applied int32
	for _, checkpoint := range req.GetCheckpoints() {
		s.mu.RLock()
		session := s.sessions[checkpoint.GetSessionId()]
		s.mu.RUnlock()
		if session == nil {
			continue
		}

		session.mu.Lock()
		binding := session.binding
		if binding == nil ||
			binding.id != checkpoint.GetConnectionId() ||
			binding.epoch != checkpoint.GetBindingEpoch() ||
			session.gatewayID != req.GetGatewayId() ||
			session.gatewayGeneration != req.GetGatewayGeneration() ||
			checkpoint.GetAcknowledgedSequence() > session.sequence {
			session.mu.Unlock()
			continue
		}
		acknowledgeLocked(session, checkpoint.GetAcknowledgedSequence())
		session.mu.Unlock()
		applied++
	}

	resp := new(sessionv1.SyncGatewayConnectionsResponse)
	resp.SetApplied(applied)
	return resp, nil
}
