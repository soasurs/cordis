package server

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/observability"
)

type connectionCheckpoint struct {
	address      string
	sessionID    string
	connectionID string
	bindingEpoch uint64
	sequence     uint64
}

type checkpointSender interface {
	Sync(context.Context, string, *sessionv1.SyncGatewayConnectionsRequest) error
}

type checkpointManager struct {
	mu         sync.Mutex
	pending    map[string]connectionCheckpoint
	sender     checkpointSender
	gatewayID  string
	generation string
	interval   time.Duration
	batchSize  int
}

func newCheckpointManager(
	sender checkpointSender,
	gatewayID, generation string,
	interval time.Duration,
	batchSize int,
) *checkpointManager {
	return &checkpointManager{
		pending: make(map[string]connectionCheckpoint), sender: sender,
		gatewayID: gatewayID, generation: generation, interval: interval, batchSize: batchSize,
	}
}

func (m *checkpointManager) record(checkpoint connectionCheckpoint) {
	m.mu.Lock()
	key := checkpoint.address + "\x00" + checkpoint.sessionID
	current, ok := m.pending[key]
	if !ok || current.connectionID != checkpoint.connectionID ||
		current.bindingEpoch != checkpoint.bindingEpoch || checkpoint.sequence > current.sequence {
		m.pending[key] = checkpoint
	}
	m.mu.Unlock()
}

func (m *checkpointManager) remove(address, sessionID, connectionID string, bindingEpoch uint64) {
	m.mu.Lock()
	key := address + "\x00" + sessionID
	current, ok := m.pending[key]
	if ok && current.connectionID == connectionID && current.bindingEpoch == bindingEpoch {
		delete(m.pending, key)
	}
	m.mu.Unlock()
}

func (m *checkpointManager) run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.flush(ctx)
		}
	}
}

func (m *checkpointManager) flush(ctx context.Context) {
	m.mu.Lock()
	byAddress := make(map[string][]connectionCheckpoint)
	for _, checkpoint := range m.pending {
		byAddress[checkpoint.address] = append(byAddress[checkpoint.address], checkpoint)
	}
	m.mu.Unlock()

	for address, checkpoints := range byAddress {
		for start := 0; start < len(checkpoints); start += m.batchSize {
			end := min(start+m.batchSize, len(checkpoints))
			batch := checkpoints[start:end]
			req := new(sessionv1.SyncGatewayConnectionsRequest)
			req.SetGatewayId(m.gatewayID)
			req.SetGatewayGeneration(m.generation)
			items := make([]*sessionv1.GatewayConnectionCheckpoint, 0, len(batch))
			for _, value := range batch {
				item := new(sessionv1.GatewayConnectionCheckpoint)
				item.SetSessionId(value.sessionID)
				item.SetConnectionId(value.connectionID)
				item.SetBindingEpoch(value.bindingEpoch)
				item.SetAcknowledgedSequence(value.sequence)
				items = append(items, item)
			}
			req.SetCheckpoints(items)
			syncCtx, cancel := context.WithTimeout(ctx, m.interval)
			err := m.sender.Sync(syncCtx, address, req)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					logx.WithContext(ctx).Errorw("sync gateway connection checkpoints",
						logx.Field("session_address", address),
						logx.Field("checkpoint_count", len(batch)),
						logx.Field("error", err),
					)
				}
				continue
			}
			m.deleteSynced(batch)
		}
	}
}

func (m *checkpointManager) deleteSynced(checkpoints []connectionCheckpoint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, checkpoint := range checkpoints {
		key := checkpoint.address + "\x00" + checkpoint.sessionID
		current, ok := m.pending[key]
		if ok && current.connectionID == checkpoint.connectionID &&
			current.bindingEpoch == checkpoint.bindingEpoch && current.sequence == checkpoint.sequence {
			delete(m.pending, key)
		}
	}
}

type grpcCheckpointSender struct {
	mu      sync.Mutex
	clients map[string]sessionv1.SessionServiceClient
	closers map[string]io.Closer
}

func newGRPCCheckpointSender() *grpcCheckpointSender {
	return &grpcCheckpointSender{
		clients: make(map[string]sessionv1.SessionServiceClient),
		closers: make(map[string]io.Closer),
	}
}

func (s *grpcCheckpointSender) Sync(
	ctx context.Context,
	address string,
	req *sessionv1.SyncGatewayConnectionsRequest,
) error {
	client, err := s.client(address)
	if err != nil {
		return err
	}
	_, err = client.SyncGatewayConnections(ctx, req)
	return err
}

func (s *grpcCheckpointSender) client(address string) (sessionv1.SessionServiceClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if client := s.clients[address]; client != nil {
		return client, nil
	}
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler(otelgrpc.WithFilter(
			observability.ExcludeGRPCMethods(sessionv1.SessionService_Connect_FullMethodName),
		))),
	)
	if err != nil {
		return nil, err
	}
	client := sessionv1.NewSessionServiceClient(conn)
	s.clients[address] = client
	s.closers[address] = conn
	return client, nil
}

func (s *grpcCheckpointSender) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for address, closer := range s.closers {
		_ = closer.Close()
		delete(s.closers, address)
		delete(s.clients, address)
	}
	return nil
}
