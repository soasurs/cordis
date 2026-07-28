//go:build integration

package gatewayticket

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/internal/testkit"
)

func TestRedisStoreRedeemsTicketOnce(t *testing.T) {
	container := testkit.StartRedis(t)
	rds, err := redis.NewRedis(redis.RedisConf{Host: container.Address, Type: redis.NodeType})
	require.NoError(t, err)
	store := NewRedisStore(rds, "test:gateway_ticket:")
	ticket := Ticket{UserID: 1001, SessionID: 2001, AccessTokenExpiresAt: time.Now().Add(time.Minute).UnixMilli()}

	require.NoError(t, store.Put(t.Context(), "hash", ticket, time.Minute))
	redeemed, err := store.Redeem(t.Context(), "hash")
	require.NoError(t, err)
	require.Equal(t, ticket, redeemed)
	_, err = store.Redeem(t.Context(), "hash")
	require.ErrorIs(t, err, ErrNotFound)
}
