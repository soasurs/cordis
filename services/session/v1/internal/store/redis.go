package store

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

//go:embed refresh_auth_session.lua
var refreshAuthSessionLua string

//go:embed delete_auth_session.lua
var deleteAuthSessionLua string

//go:embed claim_auth_session.lua
var claimAuthSessionLua string

//go:embed takeover_auth_session.lua
var takeoverAuthSessionLua string

var (
	claimAuthSessionScript    = redis.NewScript(claimAuthSessionLua)
	takeoverAuthSessionScript = redis.NewScript(takeoverAuthSessionLua)
	refreshAuthSessionScript  = redis.NewScript(refreshAuthSessionLua)
	deleteAuthSessionScript   = redis.NewScript(deleteAuthSessionLua)
)

const (
	authSessionClaimSeparator = "\x1f"
	routeMemberSeparator      = "\x1f"
)

type RedisStore struct {
	rds *redis.Redis
	now func() time.Time
}

func NewRedisStore(rds *redis.Redis) *RedisStore {
	return &RedisStore{rds: rds, now: time.Now}
}

func (s *RedisStore) ClaimAuthSession(
	ctx context.Context,
	claim AuthSessionClaim,
	ttl time.Duration,
) (AuthSessionClaimResult, error) {
	encoded, err := encodeAuthSessionClaim(claim)
	if err != nil {
		return AuthSessionClaimResult{}, err
	}
	value, err := s.rds.ScriptRunCtx(ctx, claimAuthSessionScript, []string{authSessionKey(claim.AuthSessionID)},
		encoded, max(ttl.Milliseconds(), int64(1)),
	)
	if err != nil {
		return AuthSessionClaimResult{}, err
	}
	values, ok := value.([]any)
	if !ok || len(values) != 2 {
		return AuthSessionClaimResult{}, fmt.Errorf("decode auth session claim result: %T", value)
	}
	claimed, ok := values[0].(int64)
	if !ok {
		return AuthSessionClaimResult{}, fmt.Errorf("decode auth session claim status: %T", values[0])
	}
	if claimed == 1 {
		return AuthSessionClaimResult{Claimed: true}, nil
	}
	current, ok := values[1].(string)
	if !ok {
		return AuthSessionClaimResult{}, fmt.Errorf("decode existing auth session claim: %T", values[1])
	}
	existing, err := decodeAuthSessionClaim(claim.AuthSessionID, current)
	if err != nil {
		return AuthSessionClaimResult{}, err
	}
	return AuthSessionClaimResult{Existing: existing}, nil
}

func (s *RedisStore) TakeoverAuthSession(
	ctx context.Context,
	expected, replacement AuthSessionClaim,
	ttl time.Duration,
) (bool, error) {
	if expected.AuthSessionID != replacement.AuthSessionID {
		return false, fmt.Errorf("take over auth session: auth session ids differ")
	}
	expectedValue, err := encodeAuthSessionClaim(expected)
	if err != nil {
		return false, err
	}
	replacementValue, err := encodeAuthSessionClaim(replacement)
	if err != nil {
		return false, err
	}
	value, err := s.rds.ScriptRunCtx(ctx, takeoverAuthSessionScript, []string{authSessionKey(replacement.AuthSessionID)},
		expectedValue, replacementValue, max(ttl.Milliseconds(), int64(1)),
	)
	if err != nil {
		return false, err
	}
	result, ok := value.(int64)
	if !ok {
		return false, fmt.Errorf("decode auth session takeover result: %T", value)
	}
	return result == 1, nil
}

func (s *RedisStore) RefreshAuthSession(
	ctx context.Context,
	claim AuthSessionClaim,
	ttl time.Duration,
) (bool, error) {
	encoded, err := encodeAuthSessionClaim(claim)
	if err != nil {
		return false, err
	}
	value, err := s.rds.ScriptRunCtx(ctx, refreshAuthSessionScript, []string{authSessionKey(claim.AuthSessionID)},
		encoded, max(ttl.Milliseconds(), int64(1)),
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

func (s *RedisStore) RefreshAuthSessions(ctx context.Context, claims []AuthSessionClaim, ttl time.Duration) ([]string, error) {
	if len(claims) == 0 {
		return nil, nil
	}
	encoded := make([]string, len(claims))
	for i, claim := range claims {
		var err error
		encoded[i], err = encodeAuthSessionClaim(claim)
		if err != nil {
			return nil, err
		}
	}
	results := make([]*goredis.Cmd, len(claims))
	if err := s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		for i, claim := range claims {
			results[i] = pipe.Eval(ctx, refreshAuthSessionLua, []string{authSessionKey(claim.AuthSessionID)},
				encoded[i], max(ttl.Milliseconds(), int64(1)))
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
			lost = append(lost, claims[i].LogicalSessionID)
		}
	}
	return lost, nil
}

func (s *RedisStore) DeleteAuthSession(ctx context.Context, claim AuthSessionClaim) error {
	encoded, err := encodeAuthSessionClaim(claim)
	if err != nil {
		return err
	}
	_, err = s.rds.ScriptRunCtx(ctx, deleteAuthSessionScript, []string{authSessionKey(claim.AuthSessionID)}, encoded)
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
	return fmt.Sprintf("session:auth_sessions:{%d}", authSessionID)
}

func encodeAuthSessionClaim(claim AuthSessionClaim) (string, error) {
	if claim.AuthSessionID <= 0 {
		return "", fmt.Errorf("encode auth session claim: auth session id is required")
	}
	values := []string{claim.LogicalSessionID, claim.NodeID, claim.Generation}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || strings.Contains(value, authSessionClaimSeparator) {
			return "", fmt.Errorf("encode auth session claim: claim fields are invalid")
		}
	}
	return strings.Join(values, authSessionClaimSeparator), nil
}

func decodeAuthSessionClaim(authSessionID int64, value string) (AuthSessionClaim, error) {
	parts := strings.Split(value, authSessionClaimSeparator)
	if authSessionID <= 0 || len(parts) != 3 {
		return AuthSessionClaim{}, fmt.Errorf("decode auth session claim: value is invalid")
	}
	claim := AuthSessionClaim{
		AuthSessionID: authSessionID, LogicalSessionID: parts[0], NodeID: parts[1], Generation: parts[2],
	}
	if _, err := encodeAuthSessionClaim(claim); err != nil {
		return AuthSessionClaim{}, fmt.Errorf("decode auth session claim: value is invalid")
	}
	return claim, nil
}

func routeKey(kind RouteKind, id int64) string {
	return fmt.Sprintf("gateway:routes:%s:{%d}:nodes", kind, id)
}

func routeMember(nodeID, generation string) string {
	return nodeID + routeMemberSeparator + generation
}
