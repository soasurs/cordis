package interceptors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"

	coreratelimit "github.com/soasurs/cordis/pkg/ratelimit"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
)

type allowRateLimiter struct{}

func (allowRateLimiter) Take(context.Context, string, string, int64) (coreratelimit.Decision, error) {
	return coreratelimit.Decision{Allowed: true}, nil
}

func TestAssembleInterceptorsPreservesOuterToInnerOrder(t *testing.T) {
	var events []string
	named := func(name string) connect.Interceptor {
		return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
				events = append(events, name+"_before")
				response, err := next(ctx, request)
				events = append(events, name+"_after")
				return response, err
			}
		})
	}
	chain := assembleInterceptors(
		[]connect.Interceptor{named("observability")},
		[]connect.Interceptor{named("protection")},
		[]connect.Interceptor{named("rate_limit")},
	)
	handler := wrapUnary(chain,
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			events = append(events, "handler")
			return connect.NewResponse(new(struct{})), nil
		},
	)

	_, err := handler(t.Context(), connect.NewRequest(new(struct{})))

	require.NoError(t, err)
	require.Equal(t, []string{
		"observability_before",
		"protection_before",
		"rate_limit_before",
		"handler",
		"rate_limit_after",
		"protection_after",
		"observability_after",
	}, events)
}

func TestNewRequiresHandlerBoundsAndDependencies(t *testing.T) {
	_, err := New(Config{})
	require.EqualError(t, err, "api inbound max message bytes must be positive")

	_, err = New(Config{MaxMessageBytes: 1})
	require.EqualError(t, err, "api rate limiter is required")

	_, err = New(Config{
		MaxMessageBytes: 1,
		RateLimiter:     allowRateLimiter{},
	})
	require.EqualError(t, err, "api client IP resolver is required")

	resolver, err := apiratelimit.NewClientIPResolver(nil)
	require.NoError(t, err)
	runtime, err := New(Config{
		Timeout:         3 * time.Second,
		MaxConcurrency:  1,
		MaxMessageBytes: 1,
		ServiceMaxMessageBytes: map[string]int{
			"message": 2,
		},
		RateLimiter:      allowRateLimiter{},
		ClientIPResolver: resolver,
	})
	require.NoError(t, err)
	require.Len(t, runtime.HandlerOptions(UserService), 3)
	require.Equal(t, 1, runtime.maxMessageBytesFor(UserService))
	require.Equal(t, 2, runtime.maxMessageBytesFor(MessageService))
}

func TestNewRejectsInvalidOverrides(t *testing.T) {
	_, err := New(Config{
		MaxMessageBytes: 1,
		ProcedureTimeouts: map[string]time.Duration{
			"not-a-procedure": time.Second,
		},
	})
	require.EqualError(t, err, "api procedure timeout key must be a full procedure")

	_, err = New(Config{
		MaxMessageBytes: 1,
		ServiceMaxMessageBytes: map[string]int{
			"unknown": 1,
		},
	})
	require.EqualError(t, err, "api service max message bytes has unknown service")
}

func TestHTTPMaxBytesHandlerReturnsConnectResourceExhausted(t *testing.T) {
	const procedure = "/test.v1.TestService/Echo"
	handler := connect.NewUnaryHandler(
		procedure,
		func(_ context.Context, request *connect.Request[wrapperspb.StringValue],
		) (*connect.Response[wrapperspb.StringValue], error) {
			return connect.NewResponse(request.Msg), nil
		},
	)
	server := httptest.NewServer(http.MaxBytesHandler(handler, 32))
	t.Cleanup(server.Close)
	client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](
		server.Client(),
		server.URL+procedure,
	)

	_, err := client.CallUnary(t.Context(), connect.NewRequest(wrapperspb.String(strings.Repeat("x", 128))))

	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
}
