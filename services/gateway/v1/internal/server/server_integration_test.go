//go:build integration

package server

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/discovery"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
)

func TestGatewayIntegration(t *testing.T) {
	redisContainer := testkit.StartRedis(t)
	etcd := testkit.StartEtcd(t)
	rds, err := redis.NewRedis(redis.RedisConf{Host: redisContainer.Address, Type: redis.NodeType})
	require.NoError(t, err)

	prefix := "/cordis/gateway-server-test/" + strconv.FormatInt(time.Now().UnixNano(), 10)
	directory, err := sessionregistry.New(sessionregistry.Config{
		Hosts:              []string{etcd.Address},
		Prefix:             prefix,
		DialTimeoutSeconds: 5,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, directory.Close()) })

	t.Run("identify fails when no ready nodes", func(t *testing.T) {
		resolver := discovery.New(rds, directory)
		gateway := New(svc.NewServiceContextWithDependencies(config.Config{
			Name:     "gateway.test",
			ListenOn: "127.0.0.1:8081",
			Gateway: config.GatewayConfig{
				WebSocketPath:          "/ws",
				HeartbeatIntervalMs:    50,
				IdentifyTimeoutSeconds: 5,
			},
		}, svc.Dependencies{Resolver: resolver}))

		conn, reader := connectWebSocket(t, gateway, "/ws")
		defer conn.Close()

		_ = readEnvelope(t, reader)
		writeClientText(t, conn, `{"op":2,"d":{"token":"access-token"}}`)
		failure := readEnvelope(t, reader)
		require.Equal(t, opError, failure.Op)
		require.Equal(t, eventError, failure.T)
	})

	t.Run("identify connects to registered ready node", func(t *testing.T) {
		sessionAddr := startFakeSessionServer(t)
		host, portStr, err := net.SplitHostPort(sessionAddr)
		require.NoError(t, err)
		port, err := strconv.Atoi(portStr)
		require.NoError(t, err)
		_ = host
		_ = port

		reg, err := sessionregistry.New(sessionregistry.Config{
			Hosts:              []string{etcd.Address},
			Prefix:             prefix,
			DialTimeoutSeconds: 5,
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = reg.Close() })
		require.NoError(t, reg.Register(t.Context(), sessionregistry.Node{
			ID:         "test-node",
			Generation: "gen-1",
			RPCAddress: sessionAddr,
			Status:     sessionregistry.StatusReady,
		}, time.Minute))

		resolver := discovery.New(rds, directory)
		gateway := New(svc.NewServiceContextWithDependencies(config.Config{
			Name:     "gateway.test",
			ListenOn: "127.0.0.1:8081",
			Gateway: config.GatewayConfig{
				WebSocketPath:          "/ws",
				HeartbeatIntervalMs:    50,
				IdentifyTimeoutSeconds: 5,
			},
		}, svc.Dependencies{Resolver: resolver}))

		conn, reader := connectWebSocket(t, gateway, "/ws")
		defer conn.Close()

		_ = readEnvelope(t, reader)
		writeClientText(t, conn, `{"op":2,"d":{"token":"access-token"}}`)
		ready := readEnvelope(t, reader)
		require.Equal(t, opDispatch, ready.Op)
		require.Equal(t, eventReady, ready.T)
	})

	t.Run("resume connects to owned session node", func(t *testing.T) {
		sessionAddr := startFakeSessionServerName(t, "resume-node")
		sessionID := "sess-resume-test-1"

		reg, err := sessionregistry.New(sessionregistry.Config{
			Hosts:              []string{etcd.Address},
			Prefix:             prefix,
			DialTimeoutSeconds: 5,
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = reg.Close() })
		require.NoError(t, reg.Register(t.Context(), sessionregistry.Node{
			ID:         "resume-node",
			Generation: "gen-resume-1",
			RPCAddress: sessionAddr,
			Status:     sessionregistry.StatusReady,
		}, time.Minute))

		ownerKey := sessionOwnerKey(sessionID)
		ctx := t.Context()
		require.NoError(t, rds.HsetCtx(ctx, ownerKey, "node_id", "resume-node"))
		require.NoError(t, rds.HsetCtx(ctx, ownerKey, "generation", "gen-resume-1"))
		require.NoError(t, rds.HsetCtx(ctx, ownerKey, "expires_at", strconv.FormatInt(time.Now().Add(time.Minute).UnixMilli(), 10)))
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = rds.DelCtx(cleanupCtx, ownerKey)
		})

		resolver := discovery.New(rds, directory)
		gateway := New(svc.NewServiceContextWithDependencies(config.Config{
			Name:     "gateway.test",
			ListenOn: "127.0.0.1:8081",
			Gateway: config.GatewayConfig{
				WebSocketPath:          "/ws",
				HeartbeatIntervalMs:    50,
				IdentifyTimeoutSeconds: 5,
			},
		}, svc.Dependencies{Resolver: resolver}))

		conn, reader := connectWebSocket(t, gateway, "/ws")
		defer conn.Close()

		_ = readEnvelope(t, reader)
		payload := fmt.Sprintf(`{"op":6,"d":{"token":"access-token","session_id":"%s","seq":42}}`, sessionID)
		writeClientText(t, conn, payload)
		resumed := readEnvelope(t, reader)
		require.Equal(t, opDispatch, resumed.Op)
		require.Equal(t, eventResumed, resumed.T)
	})
}

func startFakeSessionServerName(t *testing.T, name string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	sessionv1.RegisterSessionServiceServer(server, fakeSessionServer{name: name})
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

func sessionOwnerKey(sessionID string) string {
	return "session:owners:{" + sessionID + "}"
}
