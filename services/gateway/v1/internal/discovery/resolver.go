package discovery

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

type Resolver interface {
	ResolveNode(ctx context.Context) (string, error)
	ResolveSession(ctx context.Context, sessionID string) (string, error)
}

type RedisResolver struct {
	rds *redis.Redis
	now func() time.Time
}

func NewRedisResolver(rds *redis.Redis) *RedisResolver {
	return &RedisResolver{rds: rds, now: time.Now}
}

func (r *RedisResolver) ResolveNode(ctx context.Context) (string, error) {
	now := r.now().UnixMilli()
	if _, err := r.rds.ZremrangebyscoreCtx(ctx, "session:nodes", 0, now); err != nil {
		return "", err
	}
	pairs, err := r.rds.ZrangebyscoreWithScoresCtx(ctx, "session:nodes", now+1, math.MaxInt64)
	if err != nil {
		return "", err
	}
	for _, pair := range pairs {
		nodeID, generation, ok := strings.Cut(pair.Key, "\x1f")
		if !ok {
			continue
		}
		node, err := r.rds.HmgetCtx(ctx, nodeKey(nodeID), "generation", "rpc_addr", "status", "expires_at")
		if err != nil {
			return "", err
		}
		if len(node) == 4 && node[0] == generation && node[1] != "" &&
			node[2] == "ready" && !expired(node[3], r.now()) {
			return node[1], nil
		}
	}
	return "", errors.New("ready session node not found")
}

func (r *RedisResolver) ResolveSession(ctx context.Context, sessionID string) (string, error) {
	owner, err := r.rds.HmgetCtx(ctx, ownerKey(sessionID), "node_id", "generation", "expires_at")
	if err != nil {
		return "", err
	}
	if len(owner) != 3 || owner[0] == "" || owner[1] == "" || expired(owner[2], r.now()) {
		return "", errors.New("session owner not found")
	}
	node, err := r.rds.HmgetCtx(ctx, nodeKey(owner[0]), "generation", "rpc_addr", "status", "expires_at")
	if err != nil {
		return "", err
	}
	if len(node) != 4 || node[0] != owner[1] || node[1] == "" || node[2] == "draining" || expired(node[3], r.now()) {
		return "", errors.New("session node not found")
	}
	return node[1], nil
}

func expired(value string, now time.Time) bool {
	expiresAt, err := strconv.ParseInt(value, 10, 64)
	return err != nil || expiresAt <= now.UnixMilli()
}

func ownerKey(sessionID string) string {
	return fmt.Sprintf("session:owners:{%s}", sessionID)
}

func nodeKey(nodeID string) string {
	return fmt.Sprintf("session:nodes:{%s}", nodeID)
}
