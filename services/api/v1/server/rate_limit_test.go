package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
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

type recordingKeyedConcurrencyLimiter struct {
	keys     []string
	weights  []int64
	releases int
	err      error
}

func (l *recordingKeyedConcurrencyLimiter) Acquire(_ context.Context, key string, weight int64) (func(), error) {
	l.keys = append(l.keys, key)
	l.weights = append(l.weights, weight)
	if l.err != nil {
		return nil, l.err
	}
	return func() { l.releases++ }, nil
}

func (l *recordingAPILimiter) Take(
	_ context.Context,
	policy, key string,
	_ int64,
) (coreratelimit.Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	normalizedPolicy := strings.TrimSuffix(strings.TrimSuffix(policy, "_ipv4"), "_ipv6")
	normalizedKey := strings.TrimPrefix(key, "ipv4:")
	normalizedKey = strings.TrimSuffix(normalizedKey, "/32")
	l.calls = append(l.calls, apiRateLimitCall{policy: normalizedPolicy, key: normalizedKey})
	if normalizedPolicy == l.rejectPolicy {
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

func TestMessageEndpointsApplyBusinessPoliciesAndReadStateConcurrency(t *testing.T) {
	createResp := createMessageResponse(internalMessage())
	readStatesResp := new(messagev1.GetReadStatesResponse)
	messageClient := &fakeMessageClient{
		createResponse:        createResp,
		getReadStatesResponse: readStatesResp,
	}
	limiter := new(recordingAPILimiter)
	concurrencyLimiter := new(recordingKeyedConcurrencyLimiter)
	client, closeServer := newRateLimitedMessageClient(t, messageClient, limiter, concurrencyLimiter)
	defer closeServer()

	_, err := client.CreateMessage(t.Context(), &apiv1.CreateMessageRequest{
		ChannelId: new(int64(2001)), Content: new("hello"),
	})
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyAuthenticatedUser, key: "1001"},
		{policy: apiratelimit.PolicyCreateMessageUser, key: "1001"},
		{policy: apiratelimit.PolicyCreateMessageChannel, key: "2001"},
	}, limiter.snapshot())

	limiter.reset()
	_, err = client.GetReadStates(t.Context(), &apiv1.GetReadStatesRequest{
		Scope: apiv1.ReadStateScopeType_READ_STATE_SCOPE_TYPE_ALL_DMS.Enum(),
	})
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyAuthenticatedUser, key: "1001"},
	}, limiter.snapshot())
	require.Equal(t, []string{"1001"}, concurrencyLimiter.keys)
	require.Equal(t, []int64{1}, concurrencyLimiter.weights)
	require.Equal(t, 1, concurrencyLimiter.releases)
}

func TestMessageBusinessLimitersRejectBeforeMessageRPC(t *testing.T) {
	messageClient := new(fakeMessageClient)
	limiter := &recordingAPILimiter{rejectPolicy: apiratelimit.PolicyCreateMessageUser}
	client, closeServer := newRateLimitedMessageClient(
		t,
		messageClient,
		limiter,
	)
	defer closeServer()

	_, err := client.CreateMessage(t.Context(), &apiv1.CreateMessageRequest{ChannelId: new(int64(2001))})
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	require.Nil(t, messageClient.createRequest)
}

func TestGetReadStatesConcurrencyCancellationStopsBeforeMessageRPC(t *testing.T) {
	messageClient := new(fakeMessageClient)
	client, closeServer := newRateLimitedMessageClient(
		t,
		messageClient,
		new(recordingAPILimiter),
		&recordingKeyedConcurrencyLimiter{err: context.Canceled},
	)
	defer closeServer()

	_, err := client.GetReadStates(t.Context(), &apiv1.GetReadStatesRequest{
		Scope: apiv1.ReadStateScopeType_READ_STATE_SCOPE_TYPE_ALL_DMS.Enum(),
	})
	require.Equal(t, connect.CodeCanceled, connect.CodeOf(err))
	require.Nil(t, messageClient.getReadStatesRequest)
}

func TestRelationshipEndpointsApplyBusinessPolicies(t *testing.T) {
	sendResp := new(userv1.SendFriendRequestResponse)
	sendResp.SetRelationship(internalRelationship())
	blockResp := new(userv1.BlockUserResponse)
	blockResp.SetRelationship(internalRelationship())
	unblockResp := new(userv1.UnblockUserResponse)
	unblockResp.SetOk(true)
	userClient := &fakeUserClient{
		sendFriendRequestResponse: sendResp,
		blockUserResponse:         blockResp,
		unblockUserResponse:       unblockResp,
	}
	limiter := new(recordingAPILimiter)
	client, closeServer := newRateLimitedAuthenticatedUserClient(t, userClient, limiter)
	defer closeServer()

	_, err := client.SendFriendRequest(t.Context(), &apiv1.SendFriendRequestRequest{TargetId: new(int64(1002))})
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyAuthenticatedUser, key: "1001"},
		{policy: apiratelimit.PolicyRelationshipWrite, key: "1001"},
		{policy: apiratelimit.PolicySendFriendRequestMinute, key: "1001"},
		{policy: apiratelimit.PolicySendFriendRequestDay, key: "1001"},
	}, limiter.snapshot())

	for _, mutate := range []func() error{
		func() error {
			_, err := client.BlockUser(t.Context(), &apiv1.BlockUserRequest{TargetId: new(int64(1002))})
			return err
		},
		func() error {
			_, err := client.UnblockUser(t.Context(), &apiv1.UnblockUserRequest{TargetId: new(int64(1002))})
			return err
		},
	} {
		limiter.reset()
		require.NoError(t, mutate())
		require.Equal(t, []apiRateLimitCall{
			{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
			{policy: apiratelimit.PolicyAuthenticatedUser, key: "1001"},
			{policy: apiratelimit.PolicyRelationshipWrite, key: "1001"},
			{policy: apiratelimit.PolicyBlockUnblockDebounce, key: "1001:1002"},
		}, limiter.snapshot())
	}
}

func TestGuildEndpointsApplyBusinessPolicies(t *testing.T) {
	createGuildResp := new(guildv1.CreateGuildResponse)
	createGuildResp.SetGuild(internalGuild())
	createRoleResp := new(guildv1.CreateGuildRoleResponse)
	createRoleResp.SetRole(internalGuildRole())
	createChannelResp := new(guildv1.CreateGuildChannelResponse)
	createChannelResp.SetChannel(internalGuildChannel())
	createInviteResp := new(guildv1.CreateGuildInviteResponse)
	createInviteResp.SetInvite(internalGuildInvite())
	joinInviteResp := new(guildv1.JoinGuildByInviteResponse)
	joinInviteResp.SetGuild(internalGuild())
	joinInviteResp.SetMember(internalGuildMember())
	guildClient := &fakeGuildClient{
		createResponse:        createGuildResp,
		createRoleResponse:    createRoleResp,
		createChannelResponse: createChannelResp,
		createInviteFn: func(*guildv1.CreateGuildInviteRequest) (*guildv1.CreateGuildInviteResponse, error) {
			return createInviteResp, nil
		},
		joinInviteFn: func(*guildv1.JoinGuildByInviteRequest) (*guildv1.JoinGuildByInviteResponse, error) {
			return joinInviteResp, nil
		},
	}
	limiter := new(recordingAPILimiter)
	client, closeServer := newRateLimitedGuildClient(t, guildClient, limiter)
	defer closeServer()

	_, err := client.CreateGuild(t.Context(), &apiv1.CreateGuildRequest{Name: new("guild")})
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyAuthenticatedUser, key: "1001"},
		{policy: apiratelimit.PolicyCreateGuildUser, key: "1001"},
	}, limiter.snapshot())

	resourceRequests := []func() error{
		func() error {
			_, err := client.CreateGuildRole(t.Context(), &apiv1.CreateGuildRoleRequest{GuildId: new(int64(2001)), Name: new("role")})
			return err
		},
		func() error {
			_, err := client.CreateGuildChannel(t.Context(), &apiv1.CreateGuildChannelRequest{GuildId: new(int64(2001)), Name: new("channel")})
			return err
		},
		func() error {
			_, err := client.CreateGuildInvite(t.Context(), &apiv1.CreateGuildInviteRequest{GuildId: new(int64(2001))})
			return err
		},
	}
	for _, request := range resourceRequests {
		limiter.reset()
		require.NoError(t, request())
		require.Equal(t, []apiRateLimitCall{
			{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
			{policy: apiratelimit.PolicyAuthenticatedUser, key: "1001"},
			{policy: apiratelimit.PolicyGuildResourceCreateActor, key: "1001"},
			{policy: apiratelimit.PolicyGuildResourceCreateGuild, key: "2001"},
		}, limiter.snapshot())
	}

	limiter.reset()
	_, err = client.JoinGuildByInvite(t.Context(), &apiv1.JoinGuildByInviteRequest{Code: new("invite")})
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyAuthenticatedUser, key: "1001"},
		{policy: apiratelimit.PolicyJoinGuildInviteUser, key: "1001"},
		{policy: apiratelimit.PolicyJoinGuildInviteIP, key: "127.0.0.1"},
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

func newRateLimitedAuthenticatedUserClient(
	t *testing.T,
	internalClient *fakeUserClient,
	limiter coreratelimit.Limiter,
) (apiv1connect.UserServiceClient, func()) {
	t.Helper()
	resolver, err := apiratelimit.NewClientIPResolver(nil)
	require.NoError(t, err)
	path, handler := apiv1connect.NewUserServiceHandler(
		NewUser(&svc.ServiceContext{
			AuthenticatorClient: &fakeAuthenticatorClient{verifyResponse: verifyAccessTokenResponse(1001)},
			UserClient:          internalClient,
		}),
		connect.WithInterceptors(apiratelimit.UnaryInterceptor(limiter, resolver)),
	)
	return newAuthenticatedUserClient(path, handler)
}

func newRateLimitedMessageClient(
	t *testing.T,
	internalClient *fakeMessageClient,
	limiter coreratelimit.Limiter,
	concurrencyLimiters ...svc.KeyedConcurrencyLimiter,
) (apiv1connect.MessageServiceClient, func()) {
	t.Helper()
	var concurrencyLimiter svc.KeyedConcurrencyLimiter
	if len(concurrencyLimiters) > 0 {
		concurrencyLimiter = concurrencyLimiters[0]
	}
	resolver, err := apiratelimit.NewClientIPResolver(nil)
	require.NoError(t, err)
	path, handler := apiv1connect.NewMessageServiceHandler(
		NewMessage(&svc.ServiceContext{
			AuthenticatorClient: &fakeAuthenticatorClient{verifyResponse: verifyAccessTokenResponse(1001)},
			MessageClient:       internalClient,
			ReadStatesLimiter:   concurrencyLimiter,
		}),
		connect.WithInterceptors(apiratelimit.UnaryInterceptor(limiter, resolver)),
	)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	httpClient := &http.Client{Transport: bearerRoundTripper{
		base:        http.DefaultTransport,
		accessToken: "access-token",
	}}
	return apiv1connect.NewMessageServiceClient(httpClient, httpServer.URL), httpServer.Close
}

func newRateLimitedGuildClient(
	t *testing.T,
	internalClient *fakeGuildClient,
	limiter coreratelimit.Limiter,
) (apiv1connect.GuildServiceClient, func()) {
	t.Helper()
	resolver, err := apiratelimit.NewClientIPResolver(nil)
	require.NoError(t, err)
	path, handler := apiv1connect.NewGuildServiceHandler(
		NewGuild(&svc.ServiceContext{
			AuthenticatorClient: &fakeAuthenticatorClient{verifyResponse: verifyAccessTokenResponse(1001)},
			GuildClient:         internalClient,
		}),
		connect.WithInterceptors(apiratelimit.UnaryInterceptor(limiter, resolver)),
	)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	httpClient := &http.Client{Transport: bearerRoundTripper{
		base:        http.DefaultTransport,
		accessToken: "access-token",
	}}
	return apiv1connect.NewGuildServiceClient(httpClient, httpServer.URL), httpServer.Close
}

func newAuthenticatedUserClient(path string, handler http.Handler) (apiv1connect.UserServiceClient, func()) {
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	httpClient := &http.Client{Transport: bearerRoundTripper{
		base:        http.DefaultTransport,
		accessToken: "access-token",
	}}
	return apiv1connect.NewUserServiceClient(httpClient, httpServer.URL), httpServer.Close
}
