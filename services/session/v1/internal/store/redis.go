package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

const routeMemberSeparator = "\x1f"

type RedisStore struct {
	rds *redis.Redis
	now func() time.Time
}

func NewRedisStore(rds *redis.Redis) *RedisStore {
	return &RedisStore{rds: rds, now: time.Now}
}

func (s *RedisStore) SetOwner(ctx context.Context, owner Owner, ttl time.Duration) error {
	now := s.now()
	owner.ExpiresAt = now.Add(ttl).UnixMilli()
	key := ownerKey(owner.SessionID)
	return s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, key, map[string]any{
			"node_id":    owner.NodeID,
			"generation": owner.Generation,
			"expires_at": strconv.FormatInt(owner.ExpiresAt, 10),
		})
		pipe.Expire(ctx, key, ttl)
		return nil
	})
}

func (s *RedisStore) DeleteOwner(ctx context.Context, sessionID, nodeID, generation string) error {
	values, err := s.rds.HmgetCtx(ctx, ownerKey(sessionID), "node_id", "generation")
	if err != nil || len(values) != 2 {
		return err
	}
	if values[0] != nodeID || values[1] != generation {
		return nil
	}
	_, err = s.rds.DelCtx(ctx, ownerKey(sessionID))
	return err
}

func (s *RedisStore) RefreshRoutes(ctx context.Context, nodeID, generation string, routes []Route, ttl time.Duration) error {
	if len(routes) == 0 {
		return nil
	}
	expiresAt := s.now().Add(ttl).UnixMilli()
	member := routeMember(nodeID, generation)
	return s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		for _, route := range routes {
			key := routeKey(route.Kind, route.ID)
			pipe.ZAdd(ctx, key, redis.Z{Score: float64(expiresAt), Member: member})
			pipe.Expire(ctx, key, ttl+time.Minute)
		}
		return nil
	})
}

func (s *RedisStore) DetachRoutes(ctx context.Context, nodeID, generation string, routes []Route) error {
	if len(routes) == 0 {
		return nil
	}
	member := routeMember(nodeID, generation)
	return s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		for _, route := range routes {
			pipe.ZRem(ctx, routeKey(route.Kind, route.ID), member)
		}
		return nil
	})
}

func ownerKey(sessionID string) string {
	return fmt.Sprintf("session:owners:{%s}", sessionID)
}

func routeKey(kind RouteKind, id int64) string {
	return fmt.Sprintf("gateway:routes:%s:{%d}:nodes", kind, id)
}

func routeMember(nodeID, generation string) string {
	return nodeID + routeMemberSeparator + generation
}
