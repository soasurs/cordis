//go:build integration

package server

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/services/session/v1/config"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
	"github.com/soasurs/cordis/services/session/v1/internal/svc"
)

func TestAuthSessionClaimTakeoverAfterNodeGenerationExpires(t *testing.T) {
	redisContainer := testkit.StartRedis(t)
	etcdContainer := testkit.StartEtcd(t)
	rds, err := redis.NewRedis(redis.RedisConf{Host: redisContainer.Address, Type: redis.NodeType})
	require.NoError(t, err)
	sessionStore := store.NewRedisStore(rds)

	registryConfig := sessionregistry.Config{
		Hosts: []string{etcdContainer.Address}, Prefix: "/cordis/auth-claim/" + strconv.FormatInt(time.Now().UnixNano(), 10),
	}
	oldRegistry, err := sessionregistry.New(registryConfig)
	require.NoError(t, err)
	readRegistry, err := sessionregistry.New(registryConfig)
	require.NoError(t, err)
	t.Cleanup(func() { _ = readRegistry.Close() })

	require.NoError(t, oldRegistry.Register(t.Context(), sessionregistry.Node{
		ID: "session-old", Generation: "generation-old", RPCAddress: "127.0.0.1:3006", Status: sessionregistry.StatusReady,
	}, time.Minute))
	oldClaim := store.AuthSessionClaim{
		AuthSessionID: 1001, LogicalSessionID: "logical-old", NodeID: "session-old", Generation: "generation-old",
	}
	result, err := sessionStore.ClaimAuthSession(t.Context(), oldClaim, time.Minute)
	require.NoError(t, err)
	require.True(t, result.Claimed)

	server := &Server{
		nodeID: "session-new", generation: "generation-new",
		svcCtx: &svc.ServiceContext{
			Cfg:   config.Config{Node: config.NodeConfig{NodeTTLSeconds: 30}},
			Store: sessionStore, SessionRegistry: readRegistry,
		},
	}
	newClaim := store.AuthSessionClaim{
		AuthSessionID: 1001, LogicalSessionID: "logical-new", NodeID: server.nodeID, Generation: server.generation,
	}
	claimed, err := server.claimAuthSession(t.Context(), newClaim)
	require.NoError(t, err)
	require.False(t, claimed)

	require.NoError(t, oldRegistry.Close())
	require.Eventually(t, func() bool {
		_, err := readRegistry.Resolve(t.Context(), oldClaim.NodeID, oldClaim.Generation)
		return errors.Is(err, sessionregistry.ErrNodeNotFound)
	}, 5*time.Second, 20*time.Millisecond)

	claimed, err = server.claimAuthSession(t.Context(), newClaim)
	require.NoError(t, err)
	require.True(t, claimed)
	result, err = sessionStore.ClaimAuthSession(t.Context(), oldClaim, time.Minute)
	require.NoError(t, err)
	require.False(t, result.Claimed)
	require.Equal(t, newClaim, result.Existing)
}
