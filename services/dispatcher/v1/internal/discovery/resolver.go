package discovery

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

const routeMemberSeparator = "\x1f"

type RouteKind string

const (
	RouteUser    RouteKind = "users"
	RouteGuild   RouteKind = "guilds"
	RouteChannel RouteKind = "channels"
)

type Node struct {
	ID         string
	Generation string
	RPCAddress string
}

type Resolver interface {
	Resolve(ctx context.Context, kind RouteKind, id int64) ([]Node, error)
}

type RedisResolver struct {
	rds *redis.Redis
	now func() time.Time
}

func NewRedisResolver(rds *redis.Redis) *RedisResolver {
	return &RedisResolver{rds: rds, now: time.Now}
}

func (r *RedisResolver) Resolve(ctx context.Context, kind RouteKind, id int64) ([]Node, error) {
	now := r.now().UnixMilli()
	key := routeKey(kind, id)
	if _, err := r.rds.ZremrangebyscoreCtx(ctx, key, 0, now); err != nil {
		return nil, err
	}
	pairs, err := r.rds.ZrangebyscoreWithScoresCtx(ctx, key, now+1, math.MaxInt64)
	if err != nil {
		return nil, err
	}

	nodes := make([]Node, 0, len(pairs))
	stale := make([]any, 0)
	for _, pair := range pairs {
		nodeID, generation, ok := parseMember(pair.Key)
		if !ok {
			stale = append(stale, pair.Key)
			continue
		}
		values, err := r.rds.HmgetCtx(ctx, nodeKey(nodeID), "generation", "rpc_addr", "status", "expires_at")
		if err != nil {
			return nil, err
		}
		if len(values) != 4 || values[0] != generation || values[1] == "" ||
			values[2] == "draining" || expired(values[3], now) {
			stale = append(stale, pair.Key)
			continue
		}
		nodes = append(nodes, Node{ID: nodeID, Generation: generation, RPCAddress: values[1]})
	}
	if len(stale) > 0 {
		_, _ = r.rds.ZremCtx(ctx, key, stale...)
	}
	return nodes, nil
}

func routeKey(kind RouteKind, id int64) string {
	return fmt.Sprintf("gateway:routes:%s:{%d}:nodes", kind, id)
}

func nodeKey(nodeID string) string {
	return fmt.Sprintf("session:nodes:{%s}", nodeID)
}

func parseMember(value string) (string, string, bool) {
	nodeID, generation, ok := strings.Cut(value, routeMemberSeparator)
	return nodeID, generation, ok && nodeID != "" && generation != ""
}

func expired(value string, now int64) bool {
	expiresAt, err := strconv.ParseInt(value, 10, 64)
	return err != nil || expiresAt <= now
}
