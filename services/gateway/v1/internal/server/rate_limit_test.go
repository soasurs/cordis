package server

import (
	"context"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/pkg/clientip"
	coreratelimit "github.com/soasurs/cordis/pkg/ratelimit"
	"github.com/soasurs/cordis/pkg/socketlimit"
	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
	gatewayratelimit "github.com/soasurs/cordis/services/gateway/v1/ratelimit"
)

func TestWebSocketUpgradeRateLimitRunsBeforeAccept(t *testing.T) {
	limiter := &gatewayFakeRateLimiter{decisions: map[string]coreratelimit.Decision{
		gatewayratelimit.PolicyForFamily(gatewayratelimit.PolicyUpgradeIP, clientip.FamilyIPv4): {Allowed: false, RetryAfter: time.Second},
	}}
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		RateLimit: config.RateLimitConfig{Policies: testGatewayPolicies()},
	}, svc.Dependencies{
		Resolver:      fakeResolver{},
		RateLimiter:   limiter,
		SocketLimiter: &gatewayFakeSocketLimiter{allowed: true},
	}))
	req := httptest.NewRequest("GET", "/ws", nil)
	req.RemoteAddr = "198.51.100.7:4321"
	response := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(response, req)

	require.Equal(t, 429, response.Code)
	require.Equal(t, "1", response.Header().Get("Retry-After"))
	require.Equal(t, []gatewayRateCall{{
		policy: gatewayratelimit.PolicyForFamily(gatewayratelimit.PolicyUpgradeIP, clientip.FamilyIPv4),
		key:    "ipv4:198.51.100.7/32",
	}}, limiter.calls)
}

func TestWebSocketUsesIPv6PrefixAndIPv6SocketLimit(t *testing.T) {
	limiter := &gatewayFakeRateLimiter{}
	socketLimiter := &gatewayFakeSocketLimiter{}
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		RateLimit: config.RateLimitConfig{Policies: testGatewayPolicies()},
	}, svc.Dependencies{
		Resolver:      fakeResolver{},
		RateLimiter:   limiter,
		SocketLimiter: socketLimiter,
	}))
	req := httptest.NewRequest("GET", "/ws", nil)
	req.RemoteAddr = "[2001:db8:1234:5678:abcd::1]:4321"
	response := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(response, req)

	require.Equal(t, 429, response.Code)
	require.Equal(t, []gatewayRateCall{{
		policy: gatewayratelimit.PolicyForFamily(gatewayratelimit.PolicyUpgradeIP, clientip.FamilyIPv6),
		key:    "ipv6:2001:db8:1234:5678::/64",
	}}, limiter.calls)
	require.Equal(t, "ipv6:2001:db8:1234:5678::/64", socketLimiter.key)
	require.Equal(t, int64(50000), socketLimiter.maxConnections)
	require.Equal(t, int64(5000), socketLimiter.maxPending)
	require.Equal(t, int64(20), socketLimiter.maxPendingForScope)
}

func TestResumeRateLimitsIPBeforeLogicalSession(t *testing.T) {
	limiter := &gatewayFakeRateLimiter{}
	client := &client{
		sourceScope: clientip.SourceScope(netip.MustParseAddr("198.51.100.7")),
		server:      &Server{svcCtx: &svc.ServiceContext{RateLimiter: limiter}},
	}
	err := client.checkHandshakeRateLimit(t.Context(), envelope{
		Op: opResume,
		D:  []byte(`{"session_id":"logical-1"}`),
	})
	require.NoError(t, err)
	require.Equal(t, []gatewayRateCall{
		{policy: gatewayratelimit.PolicyForFamily(gatewayratelimit.PolicyResumeIP, clientip.FamilyIPv4), key: "ipv4:198.51.100.7/32"},
		{policy: gatewayratelimit.PolicyResumeSession, key: "logical-1"},
	}, limiter.calls)
}

func TestClientEventLimitIsPerConnectionAndResetsEachMinute(t *testing.T) {
	client := &client{server: &Server{svcCtx: &svc.ServiceContext{Cfg: config.Config{
		Gateway: config.GatewayConfig{MaxClientEventsPerMinute: 2},
	}}}}
	now := time.Unix(100, 0)
	require.True(t, client.allowClientEvent(now))
	require.True(t, client.allowClientEvent(now.Add(time.Second)))
	require.False(t, client.allowClientEvent(now.Add(2*time.Second)))
	require.True(t, client.allowClientEvent(now.Add(time.Minute)))
}

type gatewayRateCall struct {
	policy string
	key    string
}

type gatewayFakeRateLimiter struct {
	calls     []gatewayRateCall
	decisions map[string]coreratelimit.Decision
}

func (l *gatewayFakeRateLimiter) Take(_ context.Context, policy, key string, _ int64) (coreratelimit.Decision, error) {
	l.calls = append(l.calls, gatewayRateCall{policy: policy, key: key})
	if decision, ok := l.decisions[policy]; ok {
		return decision, nil
	}
	return coreratelimit.Decision{Allowed: true}, nil
}

type gatewayFakeSocketLimiter struct {
	allowed            bool
	key                string
	maxConnections     int64
	maxPending         int64
	maxPendingForScope int64
	lease              *gatewayFakeSocketLease
}

func (l *gatewayFakeSocketLimiter) Acquire(
	key string,
	maxConnections, maxPending, maxPendingForScope int64,
) (socketlimit.LeaseHandle, bool) {
	l.key = key
	l.maxConnections = maxConnections
	l.maxPending = maxPending
	l.maxPendingForScope = maxPendingForScope
	if !l.allowed {
		return nil, false
	}
	l.lease = new(gatewayFakeSocketLease)
	return l.lease, true
}

type gatewayFakeSocketLease struct {
	ready atomic.Bool
}

func (l *gatewayFakeSocketLease) MarkReady() { l.ready.Store(true) }
func (*gatewayFakeSocketLease) Release()     {}

func testGatewayPolicies() map[string]config.RateLimitPolicy {
	policies := make(map[string]config.RateLimitPolicy)
	for _, name := range gatewayratelimit.RequiredPolicies() {
		policies[name] = config.RateLimitPolicy{Limit: 1, Window: time.Minute}
	}
	return policies
}
