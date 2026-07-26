package interceptors

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/zeromicro/go-zero/core/breaker"
	"github.com/zeromicro/go-zero/core/load"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/soasurs/cordis/pkg/concurrencylimit"
)

const concurrencyLimiterName = "api_inbound"

var (
	rejectedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cordis",
			Subsystem: "api_protection",
			Name:      "rejected_total",
			Help:      "Public API requests rejected by an inbound protection.",
		},
		[]string{"procedure", "reason"},
	)
	panicTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cordis",
			Subsystem: "api_protection",
			Name:      "panic_total",
			Help:      "Panics recovered by the public Connect-RPC API.",
		},
		[]string{"procedure"},
	)
)

type tryLimiter interface {
	TryAcquire(weight int64) (release func(), ok bool)
}

type circuitBreaker interface {
	AllowCtx(ctx context.Context) (breaker.Promise, error)
}

type procedureBreakers struct {
	mu      sync.Mutex
	byName  map[string]circuitBreaker
	factory func(string) circuitBreaker
}

type protectionConfig struct {
	Timeout           time.Duration
	ProcedureTimeouts map[string]time.Duration
	MaxConcurrency    int64
	CPUThreshold      int64
	Breaker           bool
}

type protectionRuntime struct {
	timeout           time.Duration
	procedureTimeouts map[string]time.Duration
	limiter           tryLimiter
	shedder           load.Shedder
	breakers          *procedureBreakers
}

func newProtectionRuntime(cfg protectionConfig) (*protectionRuntime, error) {
	if cfg.Timeout <= 0 {
		return nil, errors.New("api inbound timeout must be positive")
	}
	if cfg.MaxConcurrency <= 0 {
		return nil, errors.New("api inbound max concurrency must be positive")
	}
	if cfg.CPUThreshold < 0 || cfg.CPUThreshold >= 1000 {
		return nil, errors.New("api inbound CPU threshold must be between 0 and 999")
	}
	limiter, err := concurrencylimit.New(concurrencyLimiterName, cfg.MaxConcurrency)
	if err != nil {
		return nil, fmt.Errorf("create API inbound concurrency limiter: %w", err)
	}
	runtime := &protectionRuntime{
		timeout:           cfg.Timeout,
		procedureTimeouts: cloneMap(cfg.ProcedureTimeouts),
		limiter:           limiter,
	}
	if cfg.CPUThreshold > 0 {
		runtime.shedder = load.NewAdaptiveShedder(load.WithCpuThreshold(cfg.CPUThreshold))
	}
	if cfg.Breaker {
		runtime.breakers = newProcedureBreakers(func(procedure string) circuitBreaker {
			return breaker.NewBreaker(breaker.WithName("api_inbound:" + procedure))
		})
	}
	return runtime, nil
}

// interceptors returns protection interceptors ordered from the outer request
// deadline to the innermost per-procedure circuit breaker.
func (r *protectionRuntime) interceptors() []connect.Interceptor {
	interceptors := []connect.Interceptor{
		r.timeoutInterceptor(),
		recoveryInterceptor(),
		r.concurrencyInterceptor(),
	}
	if r.shedder != nil {
		interceptors = append(interceptors, r.sheddingInterceptor())
	}
	if r.breakers != nil {
		interceptors = append(interceptors, r.breakerInterceptor())
	}
	return interceptors
}

// recoverPanic converts handler panics into an opaque public error while retaining
// procedure and stack information in server-side telemetry.
func recoverPanic(ctx context.Context, spec connect.Spec, _ http.Header, recovered any) error {
	panicTotal.WithLabelValues(spec.Procedure).Inc()
	logx.WithContext(ctx).Errorw("connect handler panic",
		logx.Field("procedure", spec.Procedure),
		logx.Field("panic_type", fmt.Sprintf("%T", recovered)),
		logx.Field("stack", string(debug.Stack())),
	)
	return connect.NewError(connect.CodeInternal, errors.New("internal error"))
}

func (r *protectionRuntime) timeoutInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			ctx, cancel := context.WithTimeout(ctx, r.timeoutFor(req.Spec().Procedure))
			defer cancel()

			type result struct {
				response connect.AnyResponse
				err      error
			}
			done := make(chan result, 1)
			go func() {
				response, err := next(ctx, req)
				done <- result{response: response, err: err}
			}()

			select {
			case callResult := <-done:
				if err := ctx.Err(); err != nil {
					rejectedTotal.WithLabelValues(req.Spec().Procedure, contextReason(err)).Inc()
					return nil, connectContextError(err)
				}
				return callResult.response, callResult.err
			case <-ctx.Done():
				rejectedTotal.WithLabelValues(req.Spec().Procedure, contextReason(ctx.Err())).Inc()
				return nil, connectContextError(ctx.Err())
			}
		}
	})
}

func (r *protectionRuntime) timeoutFor(procedure string) time.Duration {
	if timeout, ok := r.procedureTimeouts[procedure]; ok {
		return timeout
	}
	return r.timeout
}

func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	if source == nil {
		return nil
	}
	cloned := make(map[K]V, len(source))
	maps.Copy(cloned, source)
	return cloned
}

func recoveryInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (response connect.AnyResponse, err error) {
			// WithRecover protects the service implementation itself. This
			// outer recovery runs inside the timeout worker so panics from
			// downstream interceptors cannot escape across the goroutine.
			defer func() {
				if recovered := recover(); recovered != nil {
					response = nil
					err = recoverPanic(ctx, req.Spec(), req.Header(), recovered)
				}
			}()
			return next(ctx, req)
		}
	})
}

func (r *protectionRuntime) concurrencyInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			release, ok := r.limiter.TryAcquire(1)
			if !ok {
				rejectedTotal.WithLabelValues(req.Spec().Procedure, "concurrency").Inc()
				return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("server concurrency limit exceeded"))
			}
			defer release()
			return next(ctx, req)
		}
	})
}

func (r *protectionRuntime) sheddingInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (response connect.AnyResponse, callErr error) {
			promise, err := r.shedder.Allow()
			if err != nil {
				rejectedTotal.WithLabelValues(req.Spec().Procedure, "shedding").Inc()
				return nil, connect.NewError(connect.CodeUnavailable, errors.New("server overloaded"))
			}
			completed := false
			defer func() {
				if !completed || connect.CodeOf(callErr) == connect.CodeUnavailable {
					promise.Fail()
				} else {
					promise.Pass()
				}
			}()
			response, callErr = next(ctx, req)
			completed = true
			return response, callErr
		}
	})
}

func (r *protectionRuntime) breakerInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (response connect.AnyResponse, callErr error) {
			procedureBreaker := r.breakers.get(req.Spec().Procedure)
			promise, err := procedureBreaker.AllowCtx(ctx)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return nil, connectContextError(ctxErr)
				}
				rejectedTotal.WithLabelValues(req.Spec().Procedure, "breaker").Inc()
				return nil, connect.NewError(connect.CodeUnavailable, errors.New("service temporarily unavailable"))
			}
			completed := false
			defer func() {
				switch {
				case !completed:
					promise.Reject("panic")
				case breakerFailure(callErr):
					promise.Reject(connect.CodeOf(callErr).String())
				default:
					promise.Accept()
				}
			}()
			response, callErr = next(ctx, req)
			completed = true
			return response, callErr
		}
	})
}

func newProcedureBreakers(factory func(string) circuitBreaker) *procedureBreakers {
	return &procedureBreakers{
		byName:  make(map[string]circuitBreaker),
		factory: factory,
	}
}

func (b *procedureBreakers) get(procedure string) circuitBreaker {
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing, ok := b.byName[procedure]; ok {
		return existing
	}
	created := b.factory(procedure)
	b.byName[procedure] = created
	return created
}

func breakerFailure(err error) bool {
	if err == nil {
		return false
	}
	switch connect.CodeOf(err) {
	case connect.CodeUnknown,
		connect.CodeDeadlineExceeded,
		connect.CodeInternal,
		connect.CodeUnavailable,
		connect.CodeDataLoss:
		return true
	default:
		return false
	}
}

func connectContextError(err error) error {
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, errors.New("request canceled"))
	}
	return connect.NewError(connect.CodeDeadlineExceeded, errors.New("request deadline exceeded"))
}

func contextReason(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "timeout"
}
