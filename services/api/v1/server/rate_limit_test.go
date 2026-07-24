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
		registerResponse:                 registerResponse(),
		loginResponse:                    loginResponse(authenticationResult()),
		requestPasswordResetResponse:     okBoolResponse(new(authenticatorv1.RequestPasswordResetResponse)),
		confirmPasswordResetResponse:     okBoolResponse(new(authenticatorv1.ConfirmPasswordResetResponse)),
		requestEmailVerificationResponse: okBoolResponse(new(authenticatorv1.RequestEmailVerificationResponse)),
	}
	limiter := new(recordingAPILimiter)
	client, closeServer := newRateLimitedAuthenticatorClient(t, internalClient, limiter)
	defer closeServer()

	registerReq := new(apiv1.RegisterRequest)
	registerReq.SetName("User")
	registerReq.SetUsername("user")
	registerReq.SetEmail(" User@Example.COM ")
	registerReq.SetPassword("password")
	_, err := client.Register(t.Context(), registerReq)
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyRegisterIP, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyRegisterEmail, key: apiratelimit.EmailKey("user@example.com")},
	}, limiter.snapshot())

	limiter.reset()
	loginReq := new(apiv1.LoginRequest)
	loginReq.SetEmail("user@example.com")
	loginReq.SetPassword("password")
	_, err = client.Login(t.Context(), loginReq)
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyLoginIP, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyLoginEmail, key: apiratelimit.EmailKey("user@example.com")},
	}, limiter.snapshot())

	limiter.reset()
	passwordResetReq := new(apiv1.RequestPasswordResetRequest)
	passwordResetReq.SetEmail("user@example.com")
	_, err = client.RequestPasswordReset(t.Context(), passwordResetReq)
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyRecoveryRequestIP, key: "127.0.0.1"},
	}, limiter.snapshot())

	limiter.reset()
	confirmResetReq := new(apiv1.ConfirmPasswordResetRequest)
	confirmResetReq.SetToken("token")
	confirmResetReq.SetNewPassword("password")
	_, err = client.ConfirmPasswordReset(t.Context(), confirmResetReq)
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyConfirmPasswordResetIP, key: "127.0.0.1"},
	}, limiter.snapshot())

	limiter.reset()
	requestVerificationReq := new(apiv1.RequestEmailVerificationRequest)
	requestVerificationReq.SetEmail("user@example.com")
	_, err = client.RequestEmailVerification(t.Context(), requestVerificationReq)
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyRecoveryRequestIP, key: "127.0.0.1"},
	}, limiter.snapshot())
}

func TestRegisterRateLimitRejectsBeforeAuthenticatorRPC(t *testing.T) {
	internalClient := new(fakeAuthenticatorClient)
	limiter := &recordingAPILimiter{rejectPolicy: apiratelimit.PolicyRegisterIP}
	client, closeServer := newRateLimitedAuthenticatorClient(t, internalClient, limiter)
	defer closeServer()

	registerReq := new(apiv1.RegisterRequest)
	registerReq.SetName("User")
	registerReq.SetUsername("user")
	registerReq.SetEmail("user@example.com")
	registerReq.SetPassword("password")
	_, err := client.Register(t.Context(), registerReq)
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

	getProfileReq := new(apiv1.GetUserProfileRequest)
	getProfileReq.SetUserId(1001)
	_, err := client.GetUserProfile(t.Context(), getProfileReq)
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyGetUserProfileIP, key: "127.0.0.1"},
	}, limiter.snapshot())

	limiter.reset()
	checkEmailReq := new(apiv1.CheckEmailAvailabilityRequest)
	checkEmailReq.SetEmail("user@example.com")
	_, err = client.CheckEmailAvailability(t.Context(), checkEmailReq)
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

	createMsgReq := new(apiv1.CreateMessageRequest)
	createMsgReq.SetChannelId(2001)
	createMsgReq.SetContent("hello")
	_, err := client.CreateMessage(t.Context(), createMsgReq)
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyAuthenticatedUser, key: "1001"},
		{policy: apiratelimit.PolicyCreateMessageUser, key: "1001"},
		{policy: apiratelimit.PolicyCreateMessageChannel, key: "2001"},
	}, limiter.snapshot())

	limiter.reset()
	getReadStatesReq := new(apiv1.GetReadStatesRequest)
	getReadStatesReq.SetScope(apiv1.ReadStateScopeType_READ_STATE_SCOPE_TYPE_ALL_DMS)
	_, err = client.GetReadStates(t.Context(), getReadStatesReq)
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

	createMsgReq := new(apiv1.CreateMessageRequest)
	createMsgReq.SetChannelId(2001)
	_, err := client.CreateMessage(t.Context(), createMsgReq)
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

	getReadStatesReq := new(apiv1.GetReadStatesRequest)
	getReadStatesReq.SetScope(apiv1.ReadStateScopeType_READ_STATE_SCOPE_TYPE_ALL_DMS)
	_, err := client.GetReadStates(t.Context(), getReadStatesReq)
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

	sendFriendReq := new(apiv1.SendFriendRequestRequest)
	sendFriendReq.SetTargetId(1002)
	_, err := client.SendFriendRequest(t.Context(), sendFriendReq)
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
			blockReq := new(apiv1.BlockUserRequest)
			blockReq.SetTargetId(1002)
			_, err := client.BlockUser(t.Context(), blockReq)
			return err
		},
		func() error {
			unblockReq := new(apiv1.UnblockUserRequest)
			unblockReq.SetTargetId(1002)
			_, err := client.UnblockUser(t.Context(), unblockReq)
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

	createGuildReq := new(apiv1.CreateGuildRequest)
	createGuildReq.SetName("guild")
	_, err := client.CreateGuild(t.Context(), createGuildReq)
	require.NoError(t, err)
	require.Equal(t, []apiRateLimitCall{
		{policy: apiratelimit.PolicySourceIPGuard, key: "127.0.0.1"},
		{policy: apiratelimit.PolicyAuthenticatedUser, key: "1001"},
		{policy: apiratelimit.PolicyCreateGuildUser, key: "1001"},
	}, limiter.snapshot())

	resourceRequests := []func() error{
		func() error {
			createRoleReq := new(apiv1.CreateGuildRoleRequest)
			createRoleReq.SetGuildId(2001)
			createRoleReq.SetName("role")
			_, err := client.CreateGuildRole(t.Context(), createRoleReq)
			return err
		},
		func() error {
			createChannelReq := new(apiv1.CreateGuildChannelRequest)
			createChannelReq.SetGuildId(2001)
			createChannelReq.SetName("channel")
			_, err := client.CreateGuildChannel(t.Context(), createChannelReq)
			return err
		},
		func() error {
			createInviteReq := new(apiv1.CreateGuildInviteRequest)
			createInviteReq.SetGuildId(2001)
			_, err := client.CreateGuildInvite(t.Context(), createInviteReq)
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
	joinInviteReq := new(apiv1.JoinGuildByInviteRequest)
	joinInviteReq.SetCode("invite")
	_, err = client.JoinGuildByInvite(t.Context(), joinInviteReq)
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
