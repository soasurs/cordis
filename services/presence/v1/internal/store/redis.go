package store

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

type RedisStore struct {
	rds        *redis.Redis
	gatewayTTL time.Duration
	routeTTL   time.Duration
	now        func() time.Time
}

func NewRedisStore(rds *redis.Redis, gatewayTTL, routeTTL time.Duration) *RedisStore {
	if gatewayTTL <= 0 {
		gatewayTTL = 30 * time.Second
	}
	if routeTTL <= 0 {
		routeTTL = 30 * time.Second
	}
	return &RedisStore{
		rds:        rds,
		gatewayTTL: gatewayTTL,
		routeTTL:   routeTTL,
		now:        time.Now,
	}
}

func (s *RedisStore) UpsertGateway(ctx context.Context, gateway Gateway) (Gateway, error) {
	now := s.now()
	gateway.ExpiresAt = now.Add(s.gatewayTTL).UnixMilli()
	key := gatewayKey(gateway.GatewayID)

	err := s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, key, map[string]any{
			"generation": gateway.Generation,
			"rpc_addr":   gateway.RPCAddr,
			"expires_at": strconv.FormatInt(gateway.ExpiresAt, 10),
			"updated_at": strconv.FormatInt(now.UnixMilli(), 10),
		})
		pipe.Expire(ctx, key, s.gatewayTTL)
		return nil
	})
	if err != nil {
		return Gateway{}, err
	}
	return gateway, nil
}

func (s *RedisStore) RefreshChannelRoutes(ctx context.Context, gatewayID, generation string, channelIDs []int64) (int, error) {
	if len(channelIDs) == 0 {
		return 0, nil
	}

	expiresAt := s.now().Add(s.routeTTL).UnixMilli()
	member := routeMember(gatewayID, generation)
	err := s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		for _, channelID := range channelIDs {
			key := channelGatewaysKey(channelID)
			pipe.ZAdd(ctx, key, redis.Z{Score: float64(expiresAt), Member: member})
			pipe.Expire(ctx, key, s.routeTTL+time.Minute)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(channelIDs), nil
}

func (s *RedisStore) DetachChannelRoute(ctx context.Context, gatewayID, generation string, channelID int64) error {
	_, err := s.rds.ZremCtx(ctx, channelGatewaysKey(channelID), routeMember(gatewayID, generation))
	return err
}

func (s *RedisStore) ResolveChannelGateways(ctx context.Context, channelID int64) ([]Gateway, error) {
	now := s.now().UnixMilli()
	key := channelGatewaysKey(channelID)

	if _, err := s.rds.ZremrangebyscoreCtx(ctx, key, 0, now); err != nil {
		return nil, err
	}

	pairs, err := s.rds.ZrangebyscoreWithScoresCtx(ctx, key, now+1, math.MaxInt64)
	if err != nil {
		return nil, err
	}

	gateways := make([]Gateway, 0, len(pairs))
	stale := make([]any, 0)
	for _, pair := range pairs {
		gatewayID, generation, ok := parseRouteMember(pair.Key)
		if !ok {
			stale = append(stale, pair.Key)
			continue
		}

		gateway, ok, err := s.getLiveGateway(ctx, gatewayID, generation, now)
		if err != nil {
			return nil, err
		}
		if !ok {
			stale = append(stale, pair.Key)
			continue
		}
		gateways = append(gateways, gateway)
	}

	if len(stale) > 0 {
		_, _ = s.rds.ZremCtx(ctx, key, stale...)
	}

	return gateways, nil
}

func (s *RedisStore) getLiveGateway(ctx context.Context, gatewayID, generation string, now int64) (Gateway, bool, error) {
	values, err := s.rds.HmgetCtx(ctx, gatewayKey(gatewayID), "generation", "rpc_addr", "expires_at")
	if err != nil {
		return Gateway{}, false, err
	}
	if len(values) != 3 || values[0] == "" || values[1] == "" || values[2] == "" {
		return Gateway{}, false, nil
	}
	if values[0] != generation {
		return Gateway{}, false, nil
	}

	expiresAt, err := strconv.ParseInt(values[2], 10, 64)
	if err != nil || expiresAt <= now {
		return Gateway{}, false, nil
	}

	return Gateway{
		GatewayID:  gatewayID,
		Generation: generation,
		RPCAddr:    values[1],
		ExpiresAt:  expiresAt,
	}, true, nil
}

func gatewayKey(gatewayID string) string {
	return fmt.Sprintf("presence:gateway:{%s}", gatewayID)
}

func channelGatewaysKey(channelID int64) string {
	return fmt.Sprintf("presence:channel:{%d}:gateways", channelID)
}

func routeMember(gatewayID, generation string) string {
	return gatewayID + routeMemberSeparator + generation
}

func parseRouteMember(member string) (string, string, bool) {
	parts := strings.SplitN(member, routeMemberSeparator, 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
