package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	coreratelimit "github.com/soasurs/cordis/pkg/ratelimit"
	sessionratelimit "github.com/soasurs/cordis/services/session/v1/ratelimit"
)

func TestPresenceDeduplicatesNoOpClientStateUpdates(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-presence-noop", "gateway-a", "gen-a", identify)
	require.NoError(t, err)

	limiter := &sessionFakeRateLimiter{}
	presence := &recordingPresence{}
	server.svcCtx.RateLimiter = limiter
	server.svcCtx.PresenceClient = presence
	session.mu.Lock()
	binding := session.binding
	session.mu.Unlock()
	update := new(sessionv1.PresenceUpdate)
	update.SetClientState("foreground")

	require.NoError(t, server.updatePresence(t.Context(), session, binding, update))
	require.Empty(t, limiter.calls)
	require.Empty(t, presence.updates)
}

func TestIdentifyPresenceDefaultsAndValidation(t *testing.T) {
	defaults := new(sessionv1.Identify)
	statusValue, hasStatus, clientState, err := identifyPresence(defaults)
	require.NoError(t, err)
	require.False(t, hasStatus)
	require.Equal(t, presencev1.PresenceStatus_PRESENCE_STATUS_UNSPECIFIED, statusValue)
	require.Equal(t, presencev1.ClientState_CLIENT_STATE_FOREGROUND, clientState)

	valid := new(sessionv1.Identify)
	valid.SetStatus("invisible")
	valid.SetClientState("background")
	statusValue, hasStatus, clientState, err = identifyPresence(valid)
	require.NoError(t, err)
	require.True(t, hasStatus)
	require.Equal(t, presencev1.PresenceStatus_PRESENCE_STATUS_INVISIBLE, statusValue)
	require.Equal(t, presencev1.ClientState_CLIENT_STATE_BACKGROUND, clientState)

	tests := []struct {
		name        string
		status      *string
		clientState *string
	}{
		{name: "empty status", status: new("")},
		{name: "offline", status: new("offline")},
		{name: "unknown status", status: new("away")},
		{name: "empty client state", clientState: new("")},
		{name: "unknown client state", clientState: new("mobile")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identify := new(sessionv1.Identify)
			if tt.status != nil {
				identify.SetStatus(*tt.status)
			}
			if tt.clientState != nil {
				identify.SetClientState(*tt.clientState)
			}
			_, _, _, err := identifyPresence(identify)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestPresenceUpdateUsesPartialUpdateSemantics(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-presence-partial", "gateway-a", "gen-a", identify)
	require.NoError(t, err)

	presence := &recordingPresence{}
	server.svcCtx.PresenceClient = presence
	session.mu.Lock()
	binding := session.binding
	session.mu.Unlock()

	statusOnly := new(sessionv1.PresenceUpdate)
	statusOnly.SetStatus("idle")
	require.NoError(t, server.updatePresence(t.Context(), session, binding, statusOnly))
	require.Len(t, presence.updates, 1)
	require.True(t, presence.updates[0].HasStatus())
	require.Equal(t, presencev1.PresenceStatus_PRESENCE_STATUS_IDLE, presence.updates[0].GetStatus())
	require.False(t, presence.updates[0].HasClientState())

	clientStateOnly := new(sessionv1.PresenceUpdate)
	clientStateOnly.SetClientState("background")
	require.NoError(t, server.updatePresence(t.Context(), session, binding, clientStateOnly))
	require.Len(t, presence.updates, 2)
	require.False(t, presence.updates[1].HasStatus())
	require.True(t, presence.updates[1].HasClientState())
	require.Equal(t, presencev1.ClientState_CLIENT_STATE_BACKGROUND, presence.updates[1].GetClientState())
}

func TestPresenceUpdateRejectsMissingAndInvalidFields(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-presence-invalid", "gateway-a", "gen-a", identify)
	require.NoError(t, err)
	session.mu.Lock()
	binding := session.binding
	session.mu.Unlock()

	tests := []struct {
		name        string
		status      *string
		clientState *string
	}{
		{name: "missing fields"},
		{name: "empty status", status: new("")},
		{name: "offline", status: new("offline")},
		{name: "unknown status", status: new("away")},
		{name: "empty client state", clientState: new("")},
		{name: "unknown client state", clientState: new("mobile")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			update := new(sessionv1.PresenceUpdate)
			if tt.status != nil {
				update.SetStatus(*tt.status)
			}
			if tt.clientState != nil {
				update.SetClientState(*tt.clientState)
			}
			err := server.updatePresence(t.Context(), session, binding, update)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestPresenceLimitsEachLogicalSession(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-presence-limit", "gateway-a", "gen-a", identify)
	require.NoError(t, err)

	limiter := &sessionFakeRateLimiter{}
	presence := &recordingPresence{}
	server.svcCtx.RateLimiter = limiter
	server.svcCtx.PresenceClient = presence
	session.mu.Lock()
	binding := session.binding
	session.mu.Unlock()
	for i := range 6 {
		update := new(sessionv1.PresenceUpdate)
		if i%2 == 0 {
			update.SetStatus("idle")
		} else {
			update.SetStatus("online")
		}
		update.SetClientState("foreground")
		err = server.updatePresence(t.Context(), session, binding, update)
		if i < 5 {
			require.NoError(t, err)
		} else {
			require.Equal(t, codes.ResourceExhausted, status.Code(err))
		}
	}
	require.Len(t, presence.updates, 5)
	require.Len(t, limiter.calls, 5)
}

func TestPresenceAppliesCrossDeviceUserQuota(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-presence-user", "gateway-a", "gen-a", identify)
	require.NoError(t, err)

	limiter := &sessionFakeRateLimiter{decisions: map[string]coreratelimit.Decision{
		sessionratelimit.PolicyPresenceUser: {Allowed: false},
	}}
	presence := &recordingPresence{}
	server.svcCtx.RateLimiter = limiter
	server.svcCtx.PresenceClient = presence
	session.mu.Lock()
	binding := session.binding
	session.mu.Unlock()
	update := new(sessionv1.PresenceUpdate)
	update.SetStatus("idle")
	update.SetClientState("foreground")

	err = server.updatePresence(t.Context(), session, binding, update)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Equal(t, []sessionRateCall{{
		policy: sessionratelimit.PolicyPresenceUser, key: "1001", cost: 1,
	}}, limiter.calls)
	require.Empty(t, presence.updates)
}
