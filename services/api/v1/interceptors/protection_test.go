package interceptors

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/breaker"
	"github.com/zeromicro/go-zero/core/load"
)

type fakeLimiter struct {
	available atomic.Bool
	inUse     atomic.Int64
	onAcquire func()
	onRelease func()
}

func (l *fakeLimiter) TryAcquire(_ int64) (func(), bool) {
	if !l.available.CompareAndSwap(true, false) {
		return nil, false
	}
	if l.onAcquire != nil {
		l.onAcquire()
	}
	l.inUse.Add(1)
	return func() {
		if l.onRelease != nil {
			l.onRelease()
		}
		l.inUse.Add(-1)
		l.available.Store(true)
	}, true
}

type fakeLoadPromise struct {
	passed atomic.Int64
	failed atomic.Int64
	onPass func()
	onFail func()
}

func (p *fakeLoadPromise) Pass() {
	if p.onPass != nil {
		p.onPass()
	}
	p.passed.Add(1)
}

func (p *fakeLoadPromise) Fail() {
	if p.onFail != nil {
		p.onFail()
	}
	p.failed.Add(1)
}

type fakeShedder struct {
	promise load.Promise
	err     error
	onAllow func()
}

func (s fakeShedder) Allow() (load.Promise, error) {
	if s.onAllow != nil {
		s.onAllow()
	}
	return s.promise, s.err
}

type fakeBreakerPromise struct {
	accepted atomic.Int64
	rejected atomic.Int64
	onAccept func()
	onReject func()
}

func (p *fakeBreakerPromise) Accept() {
	if p.onAccept != nil {
		p.onAccept()
	}
	p.accepted.Add(1)
}

func (p *fakeBreakerPromise) Reject(string) {
	if p.onReject != nil {
		p.onReject()
	}
	p.rejected.Add(1)
}

type fakeCircuitBreaker struct {
	promise breaker.Promise
	err     error
	onAllow func()
}

func (b fakeCircuitBreaker) AllowCtx(context.Context) (breaker.Promise, error) {
	if b.onAllow != nil {
		b.onAllow()
	}
	return b.promise, b.err
}

func TestNewValidatesConfig(t *testing.T) {
	_, err := newProtectionRuntime(protectionConfig{})
	require.EqualError(t, err, "api inbound timeout must be positive")

	_, err = newProtectionRuntime(protectionConfig{Timeout: time.Second, MaxConcurrency: 1, CPUThreshold: 1000})
	require.EqualError(t, err, "api inbound CPU threshold must be between 0 and 999")

	runtime, err := newProtectionRuntime(protectionConfig{
		Timeout:        time.Second,
		MaxConcurrency: 1,
		CPUThreshold:   0,
	})
	require.NoError(t, err)
	require.Nil(t, runtime.shedder)
	require.Nil(t, runtime.breakers)
}

func TestTimeoutInterceptorReturnsDeadlineExceeded(t *testing.T) {
	runtime := &protectionRuntime{timeout: 10 * time.Millisecond}
	interceptor := runtime.timeoutInterceptor()
	handler := interceptor.WrapUnary(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	_, err := handler(t.Context(), connect.NewRequest(new(struct{})))

	require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
}

func TestTimeoutForUsesProcedureOverride(t *testing.T) {
	runtime := &protectionRuntime{
		timeout: time.Second,
		procedureTimeouts: map[string]time.Duration{
			"/api.v1.GuildService/ListGuildMembers": 2 * time.Second,
		},
	}

	require.Equal(t, 2*time.Second, runtime.timeoutFor("/api.v1.GuildService/ListGuildMembers"))
	require.Equal(t, time.Second, runtime.timeoutFor("/api.v1.UserService/GetCurrentUser"))
}

func TestRuntimeTimeoutKeepsConcurrencyHeldUntilHandlerExits(t *testing.T) {
	limiter := new(fakeLimiter)
	limiter.available.Store(true)
	runtime := &protectionRuntime{
		timeout: 10 * time.Millisecond,
		limiter: limiter,
	}
	releaseHandler := make(chan struct{})
	handler := wrapUnary(runtime.interceptors(),
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			<-releaseHandler
			return connect.NewResponse(new(struct{})), nil
		},
	)

	_, err := handler(t.Context(), connect.NewRequest(new(struct{})))
	require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
	require.Equal(t, int64(1), limiter.inUse.Load())

	_, err = handler(t.Context(), connect.NewRequest(new(struct{})))
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))

	close(releaseHandler)
	require.Eventually(t, func() bool {
		return limiter.inUse.Load() == 0
	}, time.Second, time.Millisecond)
}

func TestRuntimeRecoversPanicInsideTimeoutWorker(t *testing.T) {
	limiter := new(fakeLimiter)
	limiter.available.Store(true)
	runtime := &protectionRuntime{
		timeout: time.Second,
		limiter: limiter,
	}
	handler := wrapUnary(runtime.interceptors(),
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			panic("handler panic")
		},
	)

	_, err := handler(t.Context(), connect.NewRequest(new(struct{})))

	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	require.NotContains(t, err.Error(), "handler panic")
	require.Zero(t, limiter.inUse.Load())
}

func TestRuntimeInterceptorOrder(t *testing.T) {
	var events []string
	appendEvent := func(event string) func() {
		return func() {
			events = append(events, event)
		}
	}
	limiter := &fakeLimiter{
		onAcquire: appendEvent("concurrency_acquire"),
		onRelease: appendEvent("concurrency_release"),
	}
	limiter.available.Store(true)
	loadPromise := &fakeLoadPromise{onPass: appendEvent("shedding_pass")}
	breakerPromise := &fakeBreakerPromise{onAccept: appendEvent("breaker_accept")}
	runtime := &protectionRuntime{
		timeout: time.Second,
		limiter: limiter,
		shedder: fakeShedder{
			promise: loadPromise,
			onAllow: appendEvent("shedding_allow"),
		},
		breakers: newProcedureBreakers(func(string) circuitBreaker {
			return fakeCircuitBreaker{
				promise: breakerPromise,
				onAllow: appendEvent("breaker_allow"),
			}
		}),
	}
	handler := wrapUnary(runtime.interceptors(),
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			events = append(events, "handler")
			return connect.NewResponse(new(struct{})), nil
		},
	)

	_, err := handler(t.Context(), connect.NewRequest(new(struct{})))

	require.NoError(t, err)
	require.Equal(t, []string{
		"concurrency_acquire",
		"shedding_allow",
		"breaker_allow",
		"handler",
		"breaker_accept",
		"shedding_pass",
		"concurrency_release",
	}, events)
}

func TestConcurrencyInterceptorRejectsWhenFull(t *testing.T) {
	limiter := new(fakeLimiter)
	limiter.available.Store(true)
	runtime := &protectionRuntime{limiter: limiter}
	started := make(chan struct{})
	release := make(chan struct{})
	handler := runtime.concurrencyInterceptor().WrapUnary(
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			close(started)
			<-release
			return connect.NewResponse(new(struct{})), nil
		},
	)
	firstDone := make(chan error, 1)
	go func() {
		_, err := handler(t.Context(), connect.NewRequest(new(struct{})))
		firstDone <- err
	}()
	<-started

	_, err := handler(t.Context(), connect.NewRequest(new(struct{})))

	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	require.Equal(t, int64(1), limiter.inUse.Load())
	close(release)
	require.NoError(t, <-firstDone)
	require.Zero(t, limiter.inUse.Load())
}

func TestRecoveryInterceptorConvertsPanic(t *testing.T) {
	handler := recoveryInterceptor().WrapUnary(
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			panic("sensitive panic value")
		},
	)

	_, err := handler(t.Context(), connect.NewRequest(new(struct{})))

	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	require.Equal(t, "internal", err.Error()[:len("internal")])
	require.NotContains(t, err.Error(), "sensitive")
}

func TestSheddingInterceptorReportsOutcome(t *testing.T) {
	promise := new(fakeLoadPromise)
	runtime := &protectionRuntime{shedder: fakeShedder{promise: promise}}
	success := runtime.sheddingInterceptor().WrapUnary(
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(new(struct{})), nil
		},
	)
	_, err := success(t.Context(), connect.NewRequest(new(struct{})))
	require.NoError(t, err)
	require.Equal(t, int64(1), promise.passed.Load())

	unavailable := runtime.sheddingInterceptor().WrapUnary(
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("unavailable"))
		},
	)
	_, err = unavailable(t.Context(), connect.NewRequest(new(struct{})))
	require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
	require.Equal(t, int64(1), promise.failed.Load())
}

func TestSheddingInterceptorRejectsOverload(t *testing.T) {
	runtime := &protectionRuntime{shedder: fakeShedder{err: load.ErrServiceOverloaded}}
	handler := runtime.sheddingInterceptor().WrapUnary(
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			t.Fatal("handler must not run")
			return nil, nil
		},
	)

	_, err := handler(t.Context(), connect.NewRequest(new(struct{})))

	require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
}

func TestSheddingInterceptorFailsPromiseOnPanic(t *testing.T) {
	promise := new(fakeLoadPromise)
	runtime := &protectionRuntime{shedder: fakeShedder{promise: promise}}
	handler := runtime.sheddingInterceptor().WrapUnary(
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			panic("downstream interceptor panic")
		},
	)

	require.Panics(t, func() {
		_, _ = handler(t.Context(), connect.NewRequest(new(struct{})))
	})
	require.Zero(t, promise.passed.Load())
	require.Equal(t, int64(1), promise.failed.Load())
}

func TestBreakerInterceptorClassifiesResults(t *testing.T) {
	promise := new(fakeBreakerPromise)
	runtime := &protectionRuntime{
		breakers: newProcedureBreakers(func(string) circuitBreaker {
			return fakeCircuitBreaker{promise: promise}
		}),
	}
	success := runtime.breakerInterceptor().WrapUnary(
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(new(struct{})), nil
		},
	)
	_, err := success(t.Context(), connect.NewRequest(new(struct{})))
	require.NoError(t, err)
	require.Equal(t, int64(1), promise.accepted.Load())

	clientError := runtime.breakerInterceptor().WrapUnary(
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid"))
		},
	)
	_, err = clientError(t.Context(), connect.NewRequest(new(struct{})))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Equal(t, int64(2), promise.accepted.Load())

	serverError := runtime.breakerInterceptor().WrapUnary(
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			return nil, connect.NewError(connect.CodeInternal, errors.New("internal"))
		},
	)
	_, err = serverError(t.Context(), connect.NewRequest(new(struct{})))
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	require.Equal(t, int64(1), promise.rejected.Load())
}

func TestBreakerInterceptorRejectsOpenProcedure(t *testing.T) {
	runtime := &protectionRuntime{
		breakers: newProcedureBreakers(func(string) circuitBreaker {
			return fakeCircuitBreaker{err: breaker.ErrServiceUnavailable}
		}),
	}
	handler := runtime.breakerInterceptor().WrapUnary(
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			t.Fatal("handler must not run")
			return nil, nil
		},
	)

	_, err := handler(t.Context(), connect.NewRequest(new(struct{})))

	require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
}

func TestBreakerInterceptorRejectsPromiseOnPanic(t *testing.T) {
	promise := new(fakeBreakerPromise)
	runtime := &protectionRuntime{
		breakers: newProcedureBreakers(func(string) circuitBreaker {
			return fakeCircuitBreaker{promise: promise}
		}),
	}
	handler := runtime.breakerInterceptor().WrapUnary(
		func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			panic("downstream interceptor panic")
		},
	)

	require.Panics(t, func() {
		_, _ = handler(t.Context(), connect.NewRequest(new(struct{})))
	})
	require.Zero(t, promise.accepted.Load())
	require.Equal(t, int64(1), promise.rejected.Load())
}

func TestBreakerFailure(t *testing.T) {
	require.False(t, breakerFailure(nil))
	require.False(t, breakerFailure(connect.NewError(connect.CodeResourceExhausted, errors.New("limited"))))
	require.True(t, breakerFailure(connect.NewError(connect.CodeDeadlineExceeded, context.DeadlineExceeded)))
	require.True(t, breakerFailure(errors.New("uncoded")))
}

func wrapUnary(interceptors []connect.Interceptor, handler connect.UnaryFunc) connect.UnaryFunc {
	for index := len(interceptors) - 1; index >= 0; index-- {
		handler = interceptors[index].WrapUnary(handler)
	}
	return handler
}
