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
	rds            *redis.Redis
	gatewayTTL     time.Duration
	routeTTL       time.Duration
	userSessionTTL time.Duration
	now            func() time.Time
}

func NewRedisStore(rds *redis.Redis, gatewayTTL, routeTTL, userSessionTTL time.Duration) *RedisStore {
	if gatewayTTL <= 0 {
		gatewayTTL = 30 * time.Second
	}
	if routeTTL <= 0 {
		routeTTL = 30 * time.Second
	}
	if userSessionTTL <= 0 {
		userSessionTTL = time.Minute
	}
	return &RedisStore{
		rds:            rds,
		gatewayTTL:     gatewayTTL,
		routeTTL:       routeTTL,
		userSessionTTL: userSessionTTL,
		now:            time.Now,
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

func (s *RedisStore) UpsertUserSession(ctx context.Context, session UserSession) (UserPresence, error) {
	return s.writeUserSession(ctx, normalizeUserSession(session))
}

func (s *RedisStore) UpdateUserSession(ctx context.Context, session UserSession) (UserPresence, error) {
	now := s.now().UnixMilli()
	current, ok, err := s.getLiveUserSession(ctx, session.UserID, session.SessionID, now)
	if err != nil {
		return UserPresence{}, err
	}
	if !ok {
		return s.resolveUserPresence(ctx, session.UserID, now)
	}
	if session.GatewayID == "" {
		session.GatewayID = current.GatewayID
	}
	if session.Generation == "" {
		session.Generation = current.Generation
	}
	if session.DeviceType == "" {
		session.DeviceType = current.DeviceType
	}
	return s.writeUserSession(ctx, normalizeUserSession(session))
}

func (s *RedisStore) RemoveUserSession(ctx context.Context, userID int64, sessionID string) error {
	err := s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, userSessionKey(sessionID))
		pipe.ZRem(ctx, userSessionsKey(userID), sessionID)
		return nil
	})
	return err
}

func (s *RedisStore) ResolveUsersPresence(ctx context.Context, userIDs []int64) ([]UserPresence, error) {
	now := s.now().UnixMilli()
	presences := make([]UserPresence, 0, len(userIDs))
	for _, userID := range userIDs {
		presence, err := s.resolveUserPresence(ctx, userID, now)
		if err != nil {
			return nil, err
		}
		presences = append(presences, presence)
	}
	return presences, nil
}

func (s *RedisStore) writeUserSession(ctx context.Context, session UserSession) (UserPresence, error) {
	now := s.now()
	session.LastSeenAt = now.UnixMilli()
	session.ExpiresAt = now.Add(s.userSessionTTL).UnixMilli()
	err := s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, userSessionKey(session.SessionID), map[string]any{
			"user_id":      strconv.FormatInt(session.UserID, 10),
			"gateway_id":   session.GatewayID,
			"generation":   session.Generation,
			"device_type":  session.DeviceType,
			"status":       strconv.FormatInt(int64(session.Status), 10),
			"client_state": strconv.FormatInt(int64(session.ClientState), 10),
			"last_seen_at": strconv.FormatInt(session.LastSeenAt, 10),
			"expires_at":   strconv.FormatInt(session.ExpiresAt, 10),
		})
		pipe.Expire(ctx, userSessionKey(session.SessionID), s.userSessionTTL)
		pipe.ZAdd(ctx, userSessionsKey(session.UserID), redis.Z{
			Score:  float64(session.ExpiresAt),
			Member: session.SessionID,
		})
		pipe.Expire(ctx, userSessionsKey(session.UserID), s.userSessionTTL+time.Minute)
		return nil
	})
	if err != nil {
		return UserPresence{}, err
	}
	return s.resolveUserPresence(ctx, session.UserID, session.LastSeenAt)
}

func (s *RedisStore) resolveUserPresence(ctx context.Context, userID int64, now int64) (UserPresence, error) {
	key := userSessionsKey(userID)
	if _, err := s.rds.ZremrangebyscoreCtx(ctx, key, 0, now); err != nil {
		return UserPresence{}, err
	}

	pairs, err := s.rds.ZrangebyscoreWithScoresCtx(ctx, key, now+1, math.MaxInt64)
	if err != nil {
		return UserPresence{}, err
	}

	sessions := make([]UserSession, 0, len(pairs))
	stale := make([]any, 0)
	for _, pair := range pairs {
		session, ok, err := s.getLiveUserSession(ctx, userID, pair.Key, now)
		if err != nil {
			return UserPresence{}, err
		}
		if !ok {
			stale = append(stale, pair.Key)
			continue
		}
		if session.Status != PresenceStatusInvisible {
			sessions = append(sessions, session)
		}
	}

	if len(stale) > 0 {
		_, _ = s.rds.ZremCtx(ctx, key, stale...)
	}

	return aggregateUserPresence(userID, sessions), nil
}

func (s *RedisStore) getLiveUserSession(ctx context.Context, userID int64, sessionID string, now int64) (UserSession, bool, error) {
	values, err := s.rds.HmgetCtx(ctx, userSessionKey(sessionID),
		"user_id",
		"gateway_id",
		"generation",
		"device_type",
		"status",
		"client_state",
		"last_seen_at",
		"expires_at",
	)
	if err != nil {
		return UserSession{}, false, err
	}
	if len(values) != 8 || values[0] == "" || values[1] == "" || values[2] == "" || values[7] == "" {
		return UserSession{}, false, nil
	}

	storedUserID, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || storedUserID != userID {
		return UserSession{}, false, nil
	}
	expiresAt, err := strconv.ParseInt(values[7], 10, 64)
	if err != nil || expiresAt <= now {
		return UserSession{}, false, nil
	}
	status, err := strconv.ParseInt(values[4], 10, 32)
	if err != nil {
		status = int64(PresenceStatusOnline)
	}
	clientState, err := strconv.ParseInt(values[5], 10, 32)
	if err != nil {
		clientState = int64(ClientStateForeground)
	}
	lastSeenAt, err := strconv.ParseInt(values[6], 10, 64)
	if err != nil {
		lastSeenAt = 0
	}

	return UserSession{
		UserID:      storedUserID,
		SessionID:   sessionID,
		GatewayID:   values[1],
		Generation:  values[2],
		DeviceType:  values[3],
		Status:      normalizePresenceStatus(PresenceStatus(status)),
		ClientState: normalizeClientState(ClientState(clientState)),
		LastSeenAt:  lastSeenAt,
		ExpiresAt:   expiresAt,
	}, true, nil
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

func userSessionKey(sessionID string) string {
	return fmt.Sprintf("presence:session:{%s}", sessionID)
}

func userSessionsKey(userID int64) string {
	return fmt.Sprintf("presence:user:{%d}:sessions", userID)
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

func normalizeUserSession(session UserSession) UserSession {
	session.Status = normalizePresenceStatus(session.Status)
	session.ClientState = normalizeClientState(session.ClientState)
	return session
}

func normalizePresenceStatus(status PresenceStatus) PresenceStatus {
	switch status {
	case PresenceStatusOnline, PresenceStatusIdle, PresenceStatusDND, PresenceStatusInvisible:
		return status
	default:
		return PresenceStatusOnline
	}
}

func normalizeClientState(state ClientState) ClientState {
	switch state {
	case ClientStateForeground, ClientStateBackground:
		return state
	default:
		return ClientStateForeground
	}
}

func aggregateUserPresence(userID int64, sessions []UserSession) UserPresence {
	presence := UserPresence{
		UserID:   userID,
		Status:   PresenceStatusOffline,
		Sessions: sessions,
	}
	for _, session := range sessions {
		if session.LastSeenAt > presence.LastSeenAt {
			presence.LastSeenAt = session.LastSeenAt
		}
		if session.Status == PresenceStatusDND {
			presence.Status = PresenceStatusDND
			continue
		}
		if presence.Status == PresenceStatusDND {
			continue
		}
		if session.Status == PresenceStatusOnline {
			presence.Status = PresenceStatusOnline
			continue
		}
		if presence.Status == PresenceStatusOffline && session.Status == PresenceStatusIdle {
			presence.Status = PresenceStatusIdle
		}
	}
	return presence
}
