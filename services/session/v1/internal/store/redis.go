package store

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

//go:embed refresh_auth_session.lua
var refreshAuthSessionLua string

//go:embed delete_auth_session.lua
var deleteAuthSessionLua string

var (
	refreshAuthSessionScript = redis.NewScript(refreshAuthSessionLua)
	deleteAuthSessionScript  = redis.NewScript(deleteAuthSessionLua)
)

const routeMemberSeparator = "\x1f"

type RedisStore struct {
	rds *redis.Redis
	now func() time.Time
}

func NewRedisStore(rds *redis.Redis) *RedisStore {
	return &RedisStore{rds: rds, now: time.Now}
}

func (s *RedisStore) ClaimAuthSession(
	ctx context.Context,
	authSessionID int64,
	logicalSessionID string,
	ttl time.Duration,
) (bool, error) {
	seconds := max(int((ttl+time.Second-1)/time.Second), 1)
	return s.rds.SetnxExCtx(ctx, authSessionKey(authSessionID), logicalSessionID, seconds)
}

func (s *RedisStore) RefreshAuthSession(
	ctx context.Context,
	authSessionID int64,
	logicalSessionID string,
	ttl time.Duration,
) (bool, error) {
	value, err := s.rds.ScriptRunCtx(ctx, refreshAuthSessionScript, []string{authSessionKey(authSessionID)},
		logicalSessionID, max(ttl.Milliseconds(), int64(1)),
	)
	if err != nil {
		return false, err
	}
	result, ok := value.(int64)
	if !ok {
		return false, fmt.Errorf("decode auth session refresh result: %T", value)
	}
	return result == 1, nil
}

func (s *RedisStore) RefreshAuthSessions(ctx context.Context, leases []AuthSessionLease, ttl time.Duration) ([]string, error) {
	if len(leases) == 0 {
		return nil, nil
	}
	results := make([]*goredis.Cmd, len(leases))
	if err := s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		for i, lease := range leases {
			results[i] = pipe.Eval(ctx, refreshAuthSessionLua, []string{authSessionKey(lease.AuthSessionID)},
				lease.LogicalSessionID, max(ttl.Milliseconds(), int64(1)))
		}
		return nil
	}); err != nil {
		return nil, err
	}
	lost := make([]string, 0)
	for i, result := range results {
		value, err := result.Int64()
		if err != nil {
			return nil, err
		}
		if value != 1 {
			lost = append(lost, leases[i].LogicalSessionID)
		}
	}
	return lost, nil
}

func (s *RedisStore) DeleteAuthSession(ctx context.Context, authSessionID int64, logicalSessionID string) error {
	_, err := s.rds.ScriptRunCtx(ctx, deleteAuthSessionScript, []string{authSessionKey(authSessionID)}, logicalSessionID)
	return err
}

func (s *RedisStore) SetOwner(ctx context.Context, owner Owner, ttl time.Duration) error {
	return s.SetOwners(ctx, []Owner{owner}, ttl)
}

func (s *RedisStore) SetOwners(ctx context.Context, owners []Owner, ttl time.Duration) error {
	if len(owners) == 0 {
		return nil
	}
	now := s.now()
	return s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		for _, owner := range owners {
			owner.ExpiresAt = now.Add(ttl).UnixMilli()
			key := ownerKey(owner.SessionID)
			pipe.HSet(ctx, key, map[string]any{
				"node_id":    owner.NodeID,
				"generation": owner.Generation,
				"expires_at": strconv.FormatInt(owner.ExpiresAt, 10),
			})
			pipe.Expire(ctx, key, ttl)
		}
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

func authSessionKey(authSessionID int64) string {
	return fmt.Sprintf("session:auth_sessions:{%d}:logical", authSessionID)
}

func routeKey(kind RouteKind, id int64) string {
	return fmt.Sprintf("gateway:routes:%s:{%d}:nodes", kind, id)
}

func routeMember(nodeID, generation string) string {
	return nodeID + routeMemberSeparator + generation
}
