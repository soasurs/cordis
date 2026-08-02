//go:build integration

package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/internal/testkit"
)

func TestDispatcherIntegration(t *testing.T) {
	kafka := testkit.StartKafka(t)
	redisContainer := testkit.StartRedis(t)
	etcd := testkit.StartEtcd(t)
	rds, err := redis.NewRedis(redis.RedisConf{Host: redisContainer.Address, Type: redis.NodeType})
	require.NoError(t, err)
	env := &dispatcherEnv{kafkaAddress: kafka.Address, rds: rds, etcdHosts: []string{etcd.Address}}

	t.Run("guild message route", func(t *testing.T) { testGuildMessageRoute(t, env) })
	t.Run("guild route merges user route nodes", func(t *testing.T) { testGuildRouteMergesUserNodes(t, env) })
	t.Run("retry preserves uncommitted offset", func(t *testing.T) { testRetryPreservesUncommittedOffset(t, env) })
	t.Run("topic retry isolation", func(t *testing.T) { testTopicRetryIsolation(t, env) })
	t.Run("partition retry isolation", func(t *testing.T) { testPartitionRetryIsolation(t, env) })
	t.Run("retry survives rebalance", func(t *testing.T) { testRetrySurvivesRebalance(t, env) })
	t.Run("queued records replay after rebalance", func(t *testing.T) { testQueuedRecordsReplayAfterRebalance(t, env) })
	t.Run("shutdown flushes completed offsets", func(t *testing.T) { testShutdownFlushesCompletedOffsets(t, env) })
	t.Run("partition queue preserves order", func(t *testing.T) { testPartitionQueuePreservesOrder(t, env) })
	t.Run("poison pill does not block partition", func(t *testing.T) { testPoisonPillDoesNotBlockPartition(t, env) })
	t.Run("user route", func(t *testing.T) { testUserRoute(t, env) })
	t.Run("presence fan-out", func(t *testing.T) { testPresenceFanOut(t, env) })
	t.Run("presence friend lookup retry", func(t *testing.T) { testPresenceFriendLookupRetry(t, env) })
	t.Run("profile fan-out", func(t *testing.T) { testProfileFanOut(t, env) })
}
