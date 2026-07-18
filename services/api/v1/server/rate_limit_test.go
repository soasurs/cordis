package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	coreratelimit "github.com/soasurs/cordis/pkg/ratelimit"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

type apiRateLimitCall struct {
	policy string
	key    string
}

type recordingAPILimiter struct {
	mu           sync.Mutex
	calls        []apiRateLimitCall
	rejectPolicy string
}

func (l *recordingAPILimiter) Take(
	_ context.Context,
	policy, key string,
	_ int64,
) (coreratelimit.Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, apiRateLimitCall{policy: policy, key: key})
	if policy == l.rejectPolicy {
		return coreratelimit.Decision{Limit: 1, RetryAfter: time.Minute}, nil
	}
	return coreratelimit.Decision{Allowed: true, Limit: 100, Remaining: 99}, nil
}

func (l *recordingAPILimiter) reset() {
	l.mu.Lock()
	l.calls = nil
	l.mu.Unlock()
}

func (l *recordingAPILimiter) snapshot() []apiRateLimitCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]apiRateLimitCall(nil), l.calls...)
}

func TestAuthenticationEndpointsApplyNamedPolicies(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		registerResponse:                 registerResponse(authenticationResult()),
		loginResponse:                    loginResponse(authenticationResult()),
		requestPasswordResetResponse:     okBoolResponse(new(authenticatorv1.RequestPasswordResetResponse)),
		confirmPasswordResetResponse:     okBoolResponse(new(authenticatorv1.ConfirmPasswordResetResponse)),
		verifyResponse:                   verifyAccessTokenResponse(1001),
		requestEmailVerificationResponse: okBoolResponse(new(authenticatorv1.RequestEmailVerificationResponse)),
	}
	limiter := new(recordingAPILimiter)
	client, closeServer := newRateLimitedAuthenticatorClient(t, internalClient, limiter)
	defer closeServer()

	_, err := client.Register(t.Context(), &apiv1.RegisterRequest{
		Name: new("User"), Username: new("user"), Email: new(" User@Example.COM "), Password: new("password"),
	})
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyRegisterIP, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyRegisterEmail, key: apiratelimit.EmailKey("user@example.com")},
	}, limiter.snapshot())
	require.Equal(t, "127.0.0.1", internalClient.registerRequest.GetIp())

	limiter.reset()
	_, err = client.Login(t.Context(), &apiv1.LoginRequest{
		Email: new("user@example.com"), Password: new("password"),
	})
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyLoginIP, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyLoginEmail, key: apiratelimit.EmailKey("user@example.com")},
	}, limiter.snapshot())

	limiter.reset()
	_, err = client.RequestPasswordReset(t.Context(), &apiv1.RequestPasswordResetRequest{
		Email: new("user@example.com"),
	})
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyRecoveryRequestIP, key: "127.0.0.1"},
	}, limiter.snapshot())

	limiter.reset()
	_, err = client.ConfirmPasswordReset(t.Context(), &apiv1.ConfirmPasswordResetRequest{
		Token: new("token"), NewPassword: new("password"),
	})
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyConfirmPasswordResetIP, key: "127.0.0.1"},
	}, limiter.snapshot())

	limiter.reset()
	_, err = client.RequestEmailVerification(t.Context(), &apiv1.RequestEmailVerificationRequest{})
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyAuthenticatedUser, key: "1001"},
		{policy: apiratelimit.PolicyRecoveryRequestIP, key: "127.0.0.1"},
	}, limiter.snapshot())
}

func TestRegisterRateLimitRejectsBeforeAuthenticatorRPC(t *testing.T) {
	internalClient := new(fakeAuthenticatorClient)
	limiter := &recordingAPILimiter{rejectPolicy: apiratelimit.PolicyRegisterIP}
	client, closeServer := newRateLimitedAuthenticatorClient(t, internalClient, limiter)
	defer closeServer()

	_, err := client.Register(t.Context(), &apiv1.RegisterRequest{
		Name: new("User"), Username: new("user"), Email: new("user@example.com"), Password: new("password"),
	})
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	require.Nil(t, internalClient.registerRequest)
}

func TestAnonymousUserEndpointsApplyIPPolicies(t *testing.T) {
	profileResp := new(userv1.GetUserProfileResponse)
	profileResp.SetProfile(internalUserProfile())
	availabilityResp := new(userv1.CheckEmailAvailabilityResponse)
	availabilityResp.SetAvailable(true)
	internalClient := &fakeUserClient{
		getUserProfileResponse:         profileResp,
		checkEmailAvailabilityResponse: availabilityResp,
	}
	limiter := new(recordingAPILimiter)
	client, closeServer := newRateLimitedUserClient(t, internalClient, limiter)
	defer closeServer()

	_, err := client.GetUserProfile(t.Context(), &apiv1.GetUserProfileRequest{UserId: new(int64(1001))})
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyGetUserProfileIP, key: "127.0.0.1"},
	}, limiter.snapshot())

	limiter.reset()
	_, err = client.CheckEmailAvailability(t.Context(), &apiv1.CheckEmailAvailabilityRequest{
		Email: new("user@example.com"),
	})
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyCheckEmailAvailabilityIP, key: "127.0.0.1"},
	}, limiter.snapshot())
}

func newRateLimitedAuthenticatorClient(
	t *testing.T,
	internalClient *fakeAuthenticatorClient,
	limiter coreratelimit.Limiter,
) (apiv1connect.AuthenticatorServiceClient, func()) {
	t.Helper()
	resolver, err := apiratelimit.NewClientIPResolver(nil)
	require.NoError(t, err)
	path, handler := apiv1connect.NewAuthenticatorServiceHandler(
		NewAuthenticator(&svc.ServiceContext{AuthenticatorClient: internalClient}),
		connect.WithInterceptors(apiratelimit.UnaryInterceptor(limiter, resolver)),
	)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	httpClient := &http.Client{Transport: bearerRoundTripper{
		base:        http.DefaultTransport,
		accessToken: "access-token",
	}}
	return apiv1connect.NewAuthenticatorServiceClient(httpClient, httpServer.URL), httpServer.Close
}

func newRateLimitedUserClient(
	t *testing.T,
	internalClient *fakeUserClient,
	limiter coreratelimit.Limiter,
) (apiv1connect.UserServiceClient, func()) {
	t.Helper()
	resolver, err := apiratelimit.NewClientIPResolver(nil)
	require.NoError(t, err)
	path, handler := apiv1connect.NewUserServiceHandler(
		NewUser(&svc.ServiceContext{UserClient: internalClient}),
		connect.WithInterceptors(apiratelimit.UnaryInterceptor(limiter, resolver)),
	)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	return apiv1connect.NewUserServiceClient(httpServer.Client(), httpServer.URL), httpServer.Close
}
