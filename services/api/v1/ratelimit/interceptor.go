package ratelimit

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/zeromicro/go-zero/core/logx"

	coreratelimit "github.com/soasurs/cordis/pkg/ratelimit"
)

const (
	// PolicySourceIPGuard is a high-volume safety guard shared by requests
	// from one public source address. Per-user and endpoint policies provide
	// the normal business quotas.
	PolicySourceIPGuard = "source_ip_guard"
	// PolicyAuthenticatedUser is the general per-user policy applied after
	// access-token verification succeeds.
	PolicyAuthenticatedUser = "authenticated_user"
	// PolicyRegisterIP limits registration attempts from one source address.
	PolicyRegisterIP = "register_ip"
	// PolicyRegisterEmail limits registration attempts for one normalized email.
	PolicyRegisterEmail = "register_email"
	// PolicyLoginIP limits login attempts from one source address.
	PolicyLoginIP = "login_ip"
	// PolicyLoginEmail limits login attempts for one normalized email.
	PolicyLoginEmail = "login_email"
	// PolicyConfirmPasswordResetIP limits password-reset confirmations by source address.
	PolicyConfirmPasswordResetIP = "confirm_password_reset_ip"
	// PolicyGetUserProfileIP limits anonymous profile reads by source address.
	PolicyGetUserProfileIP = "get_user_profile_ip"
	// PolicyCheckEmailAvailabilityIP limits anonymous availability checks by source address.
	PolicyCheckEmailAvailabilityIP = "check_email_availability_ip"
	// PolicyRecoveryRequestIP limits aggregate recovery mail requests by source address.
	PolicyRecoveryRequestIP = "recovery_request_ip"
)

var requiredPolicies = [...]string{
	PolicySourceIPGuard,
	PolicyAuthenticatedUser,
	PolicyRegisterIP,
	PolicyRegisterEmail,
	PolicyLoginIP,
	PolicyLoginEmail,
	PolicyConfirmPasswordResetIP,
	PolicyGetUserProfileIP,
	PolicyCheckEmailAvailabilityIP,
	PolicyRecoveryRequestIP,
}

type requestState struct {
	limiter  coreratelimit.Limiter
	clientIP string
}

type requestStateKey struct{}

// UnaryInterceptor applies the coarse source-IP guard and makes request
// limiter state available to authentication and later endpoint policies.
func UnaryInterceptor(
	limiter coreratelimit.Limiter,
	resolver *ClientIPResolver,
) connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if limiter == nil || resolver == nil {
				return nil, connect.NewError(connect.CodeInternal, errors.New("rate limiter is not configured"))
			}
			clientIP, err := resolver.Resolve(req.Peer().Addr, req.Header())
			if err != nil {
				logx.WithContext(ctx).Errorw("resolve client ip", logx.Field("error", err))
				return nil, connect.NewError(connect.CodeInternal, errors.New("resolve client address"))
			}
			state := &requestState{limiter: limiter, clientIP: clientIP.String()}
			ctx = context.WithValue(ctx, requestStateKey{}, state)
			decision, err := limiter.Take(ctx, PolicySourceIPGuard, state.clientIP, 1)
			if err != nil {
				return nil, limiterError(ctx, err)
			}
			if !decision.Allowed {
				return nil, exhaustedError(decision)
			}
			return next(ctx, req)
		}
	})
}

// CheckAuthenticated applies the general authenticated-user policy when the
// request passed through UnaryInterceptor. Direct handler calls without an
// interceptor are left unchanged for focused unit tests.
func CheckAuthenticated(ctx context.Context, userID int64) error {
	return CheckKey(ctx, PolicyAuthenticatedUser, strconv.FormatInt(userID, 10))
}

// ClientIP returns the trusted client address extracted by UnaryInterceptor.
func ClientIP(ctx context.Context) (string, bool) {
	state, ok := ctx.Value(requestStateKey{}).(*requestState)
	if !ok || state == nil || state.clientIP == "" {
		return "", false
	}
	return state.clientIP, true
}

// CheckIP consumes one unit from policy using the trusted client IP.
func CheckIP(ctx context.Context, policy string) error {
	clientIP, ok := ClientIP(ctx)
	if !ok {
		return nil
	}
	return CheckKey(ctx, policy, clientIP)
}

// CheckKey consumes one unit from a named policy. Direct handler calls that
// bypass UnaryInterceptor are unchanged for focused unit tests.
func CheckKey(ctx context.Context, policy, key string) error {
	state, ok := ctx.Value(requestStateKey{}).(*requestState)
	if !ok || state == nil {
		return nil
	}
	decision, err := state.limiter.Take(ctx, policy, key, 1)
	if err != nil {
		return limiterError(ctx, err)
	}
	if !decision.Allowed {
		return exhaustedError(decision)
	}
	return nil
}

// EmailKey returns a stable, non-reversible key for a normalized email.
func EmailKey(email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	return fmt.Sprintf("%x", sha256.Sum256([]byte(normalized)))
}

// RequiredPolicies returns every API policy required at service startup.
func RequiredPolicies() []string {
	return append([]string(nil), requiredPolicies[:]...)
}

func limiterError(ctx context.Context, err error) error {
	logx.WithContext(ctx).Errorw("rate limiter", logx.Field("error", err))
	return connect.NewError(connect.CodeInternal, errors.New("rate limiter unavailable"))
}

func exhaustedError(decision coreratelimit.Decision) error {
	retryAfter := max(decision.RetryAfter, time.Second)
	retryAfterSeconds := (retryAfter + time.Second - 1) / time.Second
	err := connect.NewError(connect.CodeResourceExhausted, errors.New("rate limit exceeded"))
	err.Meta().Set("Retry-After", strconv.FormatInt(int64(retryAfterSeconds), 10))
	err.Meta().Set("X-RateLimit-Limit", strconv.FormatInt(decision.Limit, 10))
	err.Meta().Set("X-RateLimit-Remaining", strconv.FormatInt(decision.Remaining, 10))
	return err
}
