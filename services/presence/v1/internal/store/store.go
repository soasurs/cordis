package store

import "context"

type PresenceStatus int32

const (
	PresenceStatusOffline   PresenceStatus = 1
	PresenceStatusOnline    PresenceStatus = 2
	PresenceStatusIdle      PresenceStatus = 3
	PresenceStatusDND       PresenceStatus = 4
	PresenceStatusInvisible PresenceStatus = 5
)

type ClientState int32

const (
	ClientStateForeground ClientState = 1
	ClientStateBackground ClientState = 2
)

type UserSession struct {
	UserID      int64
	SessionID   string
	GatewayID   string
	Generation  string
	DeviceType  string
	Status      PresenceStatus
	ClientState ClientState
	LastSeenAt  int64
	ExpiresAt   int64
}

type UserPresence struct {
	UserID     int64
	Status     PresenceStatus
	LastSeenAt int64
	Sessions   []UserSession
}

type Store interface {
	WithUserMutation(ctx context.Context, userID int64, fn func(context.Context) error) error
	UpsertUserSession(ctx context.Context, session UserSession) (UserPresence, error)
	RefreshUserSessions(ctx context.Context, sessions []UserSession) ([]string, error)
	UpdateUserSession(ctx context.Context, session UserSession) (UserPresence, error)
	RemoveUserSession(ctx context.Context, userID int64, sessionID string) error
	ResolveUsersPresence(ctx context.Context, userIDs []int64) ([]UserPresence, error)
}
