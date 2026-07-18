package ratelimit

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	coreratelimit "github.com/soasurs/cordis/pkg/ratelimit"
)

const testProcedure = "/cordis.test.v1.RateLimitService/Call"

type limiterCall struct {
	policy string
	key    string
	cost   int64
}

type fakeLimiter struct {
	mu        sync.Mutex
	calls     []limiterCall
	decisions map[string]coreratelimit.Decision
	err       error
}

func (l *fakeLimiter) Take(
	_ context.Context,
	policy, key string,
	cost int64,
) (coreratelimit.Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, limiterCall{policy: policy, key: key, cost: cost})
	if l.err != nil {
		return coreratelimit.Decision{}, l.err
	}
	if decision, ok := l.decisions[policy]; ok {
		return decision, nil
	}
	return coreratelimit.Decision{Allowed: true, Limit: 100, Remaining: 99}, nil
}

func (l *fakeLimiter) snapshot() []limiterCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]limiterCall(nil), l.calls...)
}

func TestUnaryInterceptorUsesPeerIPAndIgnoresSpoofedHeader(t *testing.T) {
	limiter := &fakeLimiter{}
	var clientIP string
	client := newTestClient(t, limiter, func(ctx context.Context) error {
		var ok bool
		clientIP, ok = ClientIP(ctx)
		require.True(t, ok)
		return nil
	})
	req := connect.NewRequest(new(emptypb.Empty))
	req.Header().Set("X-Forwarded-For", "198.51.100.7")

	_, err := client.CallUnary(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", clientIP)
	require.Equal(t, []limiterCall{{
		policy: PolicyPublicIP,
		key:    "127.0.0.1",
		cost:   1,
	}}, limiter.snapshot())
}

func TestCheckAuthenticatedReturnsRetryAfter(t *testing.T) {
	limiter := &fakeLimiter{decisions: map[string]coreratelimit.Decision{
		PolicyAuthenticatedUser: {
			Allowed:    false,
			Limit:      10,
			Remaining:  0,
			RetryAfter: 1500 * time.Millisecond,
		},
	}}
	client := newTestClient(t, limiter, func(ctx context.Context) error {
		return CheckAuthenticated(ctx, 42)
	})

	_, err := client.CallUnary(t.Context(), connect.NewRequest(new(emptypb.Empty)))
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	connectErr := new(connect.Error)
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, "2", connectErr.Meta().Get("Retry-After"))
	require.Equal(t, "10", connectErr.Meta().Get("X-RateLimit-Limit"))
	require.Equal(t, []limiterCall{
		{policy: PolicyPublicIP, key: "127.0.0.1", cost: 1},
		{policy: PolicyAuthenticatedUser, key: "42", cost: 1},
	}, limiter.snapshot())
}

func newTestClient(
	t *testing.T,
	limiter coreratelimit.Limiter,
	check func(context.Context) error,
) *connect.Client[emptypb.Empty, emptypb.Empty] {
	t.Helper()
	resolver, err := NewClientIPResolver(nil)
	require.NoError(t, err)
	handler := connect.NewUnaryHandler(
		testProcedure,
		func(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
			if err := check(ctx); err != nil {
				return nil, err
			}
			return connect.NewResponse(new(emptypb.Empty)), nil
		},
		connect.WithInterceptors(UnaryInterceptor(limiter, resolver)),
	)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return connect.NewClient[emptypb.Empty, emptypb.Empty](
		server.Client(),
		server.URL+testProcedure,
	)
}
