package discovery

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/pkg/sessionregistry"
)

type Resolver interface {
	ResolveNode(ctx context.Context) (string, error)
	ResolveSession(ctx context.Context, sessionID string) (string, error)
}

type SessionResolver struct {
	rds      *redis.Redis
	registry sessionregistry.Directory
	now      func() time.Time
}

func New(rds *redis.Redis, registry sessionregistry.Directory) *SessionResolver {
	return &SessionResolver{rds: rds, registry: registry, now: time.Now}
}

func (r *SessionResolver) ResolveNode(ctx context.Context) (string, error) {
	nodes, err := r.registry.Ready(ctx)
	if err != nil {
		return "", err
	}
	if len(nodes) == 0 {
		return "", errors.New("ready session node not found")
	}
	return nodes[rand.IntN(len(nodes))].RPCAddress, nil
}

func (r *SessionResolver) ResolveSession(ctx context.Context, sessionID string) (string, error) {
	owner, err := r.rds.HmgetCtx(ctx, ownerKey(sessionID), "node_id", "generation", "expires_at")
	if err != nil {
		return "", err
	}
	if len(owner) != 3 || owner[0] == "" || owner[1] == "" || expired(owner[2], r.now()) {
		return "", errors.New("session owner not found")
	}
	node, err := r.registry.Resolve(ctx, owner[0], owner[1])
	if err != nil {
		return "", err
	}
	return node.RPCAddress, nil
}

func expired(value string, now time.Time) bool {
	expiresAt, err := strconv.ParseInt(value, 10, 64)
	return err != nil || expiresAt <= now.UnixMilli()
}

func ownerKey(sessionID string) string {
	return fmt.Sprintf("session:owners:{%s}", sessionID)
}
