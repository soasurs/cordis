package discovery

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/pkg/sessionregistry"
)

const routeMemberSeparator = "\x1f"

type RouteKind string

const (
	RouteUser  RouteKind = "users"
	RouteGuild RouteKind = "guilds"
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
	rds      *redis.Redis
	registry sessionregistry.Directory
	now      func() time.Time
}

func NewRedisResolver(rds *redis.Redis, registry sessionregistry.Directory) *RedisResolver {
	return &RedisResolver{rds: rds, registry: registry, now: time.Now}
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
		node, err := r.registry.Resolve(ctx, nodeID, generation)
		if errors.Is(err, sessionregistry.ErrNodeNotFound) ||
			errors.Is(err, sessionregistry.ErrNodeNotReady) {
			stale = append(stale, pair.Key)
			continue
		}
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, Node{ID: nodeID, Generation: generation, RPCAddress: node.RPCAddress})
	}
	if len(stale) > 0 {
		_, _ = r.rds.ZremCtx(ctx, key, stale...)
	}
	return nodes, nil
}

func routeKey(kind RouteKind, id int64) string {
	return fmt.Sprintf("gateway:routes:%s:{%d}:nodes", kind, id)
}

func parseMember(value string) (string, string, bool) {
	nodeID, generation, ok := strings.Cut(value, routeMemberSeparator)
	return nodeID, generation, ok && nodeID != "" && generation != ""
}
