package store

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	minUserMutationLockTTL = 10 * time.Second
	userMutationLockGrace  = 5 * time.Second
	userMutationLockRetry  = 25 * time.Millisecond
)

const refreshUserSessionLua = `
local values = redis.call('HMGET', KEYS[1], 'user_id', 'gateway_id', 'generation')
if values[1] ~= ARGV[1] or values[2] ~= ARGV[2] or values[3] ~= ARGV[3] then
  return 0
end
redis.call('HSET', KEYS[1], 'last_seen_at', ARGV[4], 'expires_at', ARGV[5])
redis.call('PEXPIRE', KEYS[1], ARGV[6])
return 1
`

type RedisStore struct {
	rds             *redis.Redis
	userSessionTTL  time.Duration
	mutationLockTTL time.Duration
	now             func() time.Time
}

func NewRedisStore(rds *redis.Redis, userSessionTTL, publishTimeout time.Duration) *RedisStore {
	if userSessionTTL <= 0 {
		userSessionTTL = time.Minute
	}
	if publishTimeout <= 0 {
		publishTimeout = time.Second
	}
	return &RedisStore{
		rds:             rds,
		userSessionTTL:  userSessionTTL,
		mutationLockTTL: max(minUserMutationLockTTL, publishTimeout+userMutationLockGrace),
		now:             time.Now,
	}
}

// WithUserMutation serializes a user's presence mutation and transition
// publication across Presence instances.
func (s *RedisStore) WithUserMutation(ctx context.Context, userID int64, fn func(context.Context) error) (err error) {
	lock := redis.NewRedisLock(s.rds, userMutationLockKey(userID))
	lock.SetExpire(int((s.mutationLockTTL + time.Second - 1) / time.Second))
	for {
		acquired, err := lock.AcquireCtx(ctx)
		if err != nil {
			return err
		}
		if acquired {
			break
		}
		timer := time.NewTimer(userMutationLockRetry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		released, releaseErr := lock.ReleaseCtx(releaseCtx)
		if err == nil && releaseErr != nil {
			err = fmt.Errorf("release user mutation lock: %w", releaseErr)
		} else if err == nil && !released {
			err = fmt.Errorf("release user mutation lock: lock expired")
		}
	}()
	return fn(ctx)
}

func (s *RedisStore) UpsertUserSession(ctx context.Context, session UserSession) (UserPresence, error) {
	return s.writeUserSession(ctx, normalizeUserSession(session))
}

func (s *RedisStore) RefreshUserSessions(ctx context.Context, sessions []UserSession) ([]string, error) {
	if len(sessions) == 0 {
		return nil, nil
	}
	results := make([]*goredis.Cmd, len(sessions))
	now := s.now()
	expiresAt := now.Add(s.userSessionTTL).UnixMilli()
	if err := s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		for i, session := range sessions {
			results[i] = pipe.Eval(ctx, refreshUserSessionLua, []string{userSessionKey(session.SessionID)},
				strconv.FormatInt(session.UserID, 10), session.GatewayID, session.Generation,
				strconv.FormatInt(now.UnixMilli(), 10), strconv.FormatInt(expiresAt, 10),
				max(s.userSessionTTL.Milliseconds(), int64(1)))
		}
		return nil
	}); err != nil {
		return nil, err
	}
	live := make([]UserSession, 0, len(sessions))
	missing := make([]string, 0)
	for i, session := range sessions {
		refreshed, err := results[i].Int64()
		if err != nil {
			return nil, err
		}
		if refreshed != 1 {
			missing = append(missing, session.SessionID)
			continue
		}
		live = append(live, session)
	}
	if err := s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		for _, session := range live {
			userKey := userSessionsKey(session.UserID)
			pipe.ZAdd(ctx, userKey, redis.Z{Score: float64(expiresAt), Member: session.SessionID})
			pipe.Expire(ctx, userKey, s.userSessionTTL+time.Minute)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return missing, nil
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
		sessions = append(sessions, session)
	}

	if len(stale) > 0 {
		_, _ = s.rds.ZremCtx(ctx, key, stale...)
	}

	preference, known, err := s.GetUserPresencePreference(ctx, userID)
	if err != nil {
		return UserPresence{}, err
	}
	if !known {
		preference = UserPresencePreference{UserID: userID, Status: PresenceStatusOnline}
	}
	return aggregateUserPresence(userID, preference, sessions), nil
}

func (s *RedisStore) getLiveUserSession(ctx context.Context, userID int64, sessionID string, now int64) (UserSession, bool, error) {
	values, err := s.rds.HmgetCtx(ctx, userSessionKey(sessionID),
		"user_id",
		"gateway_id",
		"generation",
		"device_type",
		"client_state",
		"last_seen_at",
		"expires_at",
	)
	if err != nil {
		return UserSession{}, false, err
	}
	if len(values) != 7 || values[0] == "" || values[1] == "" || values[2] == "" || values[6] == "" {
		return UserSession{}, false, nil
	}

	storedUserID, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || storedUserID != userID {
		return UserSession{}, false, nil
	}
	expiresAt, err := strconv.ParseInt(values[6], 10, 64)
	if err != nil || expiresAt <= now {
		return UserSession{}, false, nil
	}
	clientState, err := strconv.ParseInt(values[4], 10, 32)
	if err != nil {
		clientState = int64(ClientStateForeground)
	}
	lastSeenAt, err := strconv.ParseInt(values[5], 10, 64)
	if err != nil {
		lastSeenAt = 0
	}

	return UserSession{
		UserID:      storedUserID,
		SessionID:   sessionID,
		GatewayID:   values[1],
		Generation:  values[2],
		DeviceType:  values[3],
		ClientState: normalizeClientState(ClientState(clientState)),
		LastSeenAt:  lastSeenAt,
		ExpiresAt:   expiresAt,
	}, true, nil
}

func userSessionKey(sessionID string) string {
	return fmt.Sprintf("presence:session:{%s}", sessionID)
}

func userSessionsKey(userID int64) string {
	return fmt.Sprintf("presence:user:{%d}:sessions", userID)
}

func userMutationLockKey(userID int64) string {
	return fmt.Sprintf("presence:user:{%d}:mutation-lock", userID)
}

func userPresenceSnapshotKey(userID int64) string {
	return fmt.Sprintf("presence:user:{%d}:snapshot", userID)
}

func userPresencePreferenceKey(userID int64) string {
	return fmt.Sprintf("presence:user:{%d}:preference", userID)
}

func (s *RedisStore) GetUserPresencePreference(
	ctx context.Context,
	userID int64,
) (UserPresencePreference, bool, error) {
	values, err := s.rds.HmgetCtx(ctx, userPresencePreferenceKey(userID), "status", "version")
	if err != nil {
		return UserPresencePreference{}, false, err
	}
	if len(values) != 2 || values[0] == "" || values[1] == "" {
		return UserPresencePreference{}, false, nil
	}
	statusValue, err := strconv.ParseInt(values[0], 10, 32)
	if err != nil {
		return UserPresencePreference{}, false, nil
	}
	status := PresenceStatus(statusValue)
	if status != PresenceStatusOnline && status != PresenceStatusIdle &&
		status != PresenceStatusDND && status != PresenceStatusInvisible {
		return UserPresencePreference{}, false, nil
	}
	version, err := strconv.ParseInt(values[1], 10, 64)
	if err != nil || version <= 0 {
		return UserPresencePreference{}, false, nil
	}
	return UserPresencePreference{UserID: userID, Status: status, Version: version}, true, nil
}

func (s *RedisStore) SaveUserPresencePreference(
	ctx context.Context,
	preference UserPresencePreference,
) error {
	return s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, userPresencePreferenceKey(preference.UserID), map[string]any{
			"status":  strconv.FormatInt(int64(preference.Status), 10),
			"version": strconv.FormatInt(preference.Version, 10),
		})
		return nil
	})
}

func (s *RedisStore) GetUserPresenceSnapshot(ctx context.Context, userID int64) (UserPresence, bool, error) {
	values, err := s.rds.HmgetCtx(ctx, userPresenceSnapshotKey(userID), "status", "last_seen_at", "version")
	if err != nil {
		return UserPresence{}, false, err
	}
	if len(values) != 3 || values[0] == "" || values[2] == "" {
		return UserPresence{}, false, nil
	}
	statusValue, err := strconv.ParseInt(values[0], 10, 32)
	if err != nil {
		return UserPresence{}, false, nil
	}
	lastSeenAt, err := strconv.ParseInt(values[1], 10, 64)
	if err != nil {
		return UserPresence{}, false, nil
	}
	version, err := strconv.ParseInt(values[2], 10, 64)
	if err != nil || version <= 0 {
		return UserPresence{}, false, nil
	}
	status := PresenceStatus(statusValue)
	switch status {
	case PresenceStatusOffline, PresenceStatusOnline, PresenceStatusIdle, PresenceStatusDND:
	default:
		return UserPresence{}, false, nil
	}
	return UserPresence{
		UserID: userID, Status: status, LastSeenAt: lastSeenAt, Version: version,
	}, true, nil
}

func (s *RedisStore) SaveUserPresenceSnapshot(ctx context.Context, presence UserPresence) error {
	return s.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, userPresenceSnapshotKey(presence.UserID), map[string]any{
			"status":       strconv.FormatInt(int64(presence.Status), 10),
			"last_seen_at": strconv.FormatInt(presence.LastSeenAt, 10),
			"version":      strconv.FormatInt(presence.Version, 10),
		})
		return nil
	})
}

func normalizeUserSession(session UserSession) UserSession {
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

func aggregateUserPresence(
	userID int64,
	preference UserPresencePreference,
	sessions []UserSession,
) UserPresence {
	presence := UserPresence{
		UserID:   userID,
		Status:   PresenceStatusOffline,
		Sessions: sessions,
	}
	for _, session := range sessions {
		if session.LastSeenAt > presence.LastSeenAt {
			presence.LastSeenAt = session.LastSeenAt
		}
	}
	if len(sessions) > 0 && preference.Status != PresenceStatusInvisible {
		presence.Status = normalizePresenceStatus(preference.Status)
	}
	return presence
}
