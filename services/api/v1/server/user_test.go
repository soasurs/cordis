package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	coreratelimit "github.com/soasurs/cordis/pkg/ratelimit"
	"github.com/soasurs/cordis/pkg/rpcerror"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

type fakeUserClient struct {
	userv1.UserServiceClient
	getUserRequest                   *userv1.GetUserRequest
	getUserResponse                  *userv1.GetUserResponse
	getUserError                     error
	getUserProfileRequest            *userv1.GetUserProfileRequest
	getUserProfileResponse           *userv1.GetUserProfileResponse
	getUserProfileError              error
	checkEmailAvailabilityRequest    *userv1.CheckEmailAvailabilityRequest
	checkEmailAvailabilityResponse   *userv1.CheckEmailAvailabilityResponse
	checkEmailAvailabilityError      error
	updateEmailRequest               *userv1.UpdateEmailRequest
	updateEmailResponse              *userv1.UpdateEmailResponse
	updateEmailError                 error
	updateUserProfileRequest         *userv1.UpdateUserProfileRequest
	updateUserProfileResponse        *userv1.UpdateUserProfileResponse
	updateUserProfileError           error
	createAvatarUploadRequest        *userv1.CreateAvatarUploadRequest
	createAvatarUploadResponse       *userv1.CreateAvatarUploadResponse
	updateUsernameRequest            *userv1.UpdateUsernameRequest
	updateUsernameResponse           *userv1.UpdateUsernameResponse
	updateUsernameError              error
	getUserProfileByUsernameRequest  *userv1.GetUserProfileByUsernameRequest
	getUserProfileByUsernameResponse *userv1.GetUserProfileByUsernameResponse
	getUserProfileByUsernameError    error
	sendFriendRequestRequest         *userv1.SendFriendRequestRequest
	sendFriendRequestResponse        *userv1.SendFriendRequestResponse
	sendFriendRequestError           error
	acceptFriendRequestRequest       *userv1.AcceptFriendRequestRequest
	acceptFriendRequestResponse      *userv1.AcceptFriendRequestResponse
	acceptFriendRequestError         error
	declineFriendRequestRequest      *userv1.DeclineFriendRequestRequest
	declineFriendRequestResponse     *userv1.DeclineFriendRequestResponse
	declineFriendRequestError        error
	removeFriendRequest              *userv1.RemoveFriendRequest
	removeFriendResponse             *userv1.RemoveFriendResponse
	removeFriendError                error
	blockUserRequest                 *userv1.BlockUserRequest
	blockUserResponse                *userv1.BlockUserResponse
	blockUserError                   error
	unblockUserRequest               *userv1.UnblockUserRequest
	unblockUserResponse              *userv1.UnblockUserResponse
	unblockUserError                 error
	listRelationshipsRequest         *userv1.ListRelationshipsRequest
	listRelationshipsResponse        *userv1.ListRelationshipsResponse
	listRelationshipsError           error
}

type authenticatedQuotaLimiter struct{}

func (authenticatedQuotaLimiter) Take(
	_ context.Context,
	policy, _ string,
	_ int64,
) (coreratelimit.Decision, error) {
	if policy == apiratelimit.PolicyAuthenticatedUser {
		return coreratelimit.Decision{
			Limit:      1,
			RetryAfter: time.Minute,
		}, nil
	}
	return coreratelimit.Decision{Allowed: true, Limit: 100, Remaining: 99}, nil
}

func (f *fakeUserClient) GetUser(_ context.Context, req *userv1.GetUserRequest, _ ...grpc.CallOption) (*userv1.GetUserResponse, error) {
	f.getUserRequest = req
	return f.getUserResponse, f.getUserError
}

func (f *fakeUserClient) GetUserProfile(_ context.Context, req *userv1.GetUserProfileRequest, _ ...grpc.CallOption) (*userv1.GetUserProfileResponse, error) {
	f.getUserProfileRequest = req
	return f.getUserProfileResponse, f.getUserProfileError
}

func (f *fakeUserClient) CheckEmailAvailability(_ context.Context, req *userv1.CheckEmailAvailabilityRequest, _ ...grpc.CallOption) (*userv1.CheckEmailAvailabilityResponse, error) {
	f.checkEmailAvailabilityRequest = req
	return f.checkEmailAvailabilityResponse, f.checkEmailAvailabilityError
}

func (f *fakeUserClient) UpdateEmail(_ context.Context, req *userv1.UpdateEmailRequest, _ ...grpc.CallOption) (*userv1.UpdateEmailResponse, error) {
	f.updateEmailRequest = req
	return f.updateEmailResponse, f.updateEmailError
}

func (f *fakeUserClient) UpdateUserProfile(_ context.Context, req *userv1.UpdateUserProfileRequest, _ ...grpc.CallOption) (*userv1.UpdateUserProfileResponse, error) {
	f.updateUserProfileRequest = req
	return f.updateUserProfileResponse, f.updateUserProfileError
}

func (f *fakeUserClient) CreateAvatarUpload(
	_ context.Context,
	req *userv1.CreateAvatarUploadRequest,
	_ ...grpc.CallOption,
) (*userv1.CreateAvatarUploadResponse, error) {
	f.createAvatarUploadRequest = req
	return f.createAvatarUploadResponse, nil
}

func TestGetCurrentUserOverConnectHTTP(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	userClient := &fakeUserClient{
		getUserResponse:        getUserResponse(internalUser()),
		getUserProfileResponse: getUserProfileResponse(internalUserProfile()),
	}
	client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
	defer closeServer()

	req := new(apiv1.GetCurrentUserRequest)
	resp, err := client.GetCurrentUser(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "access-token", authenticatorClient.verifyRequest.GetAccessToken())
	require.Equal(t, int64(1001), userClient.getUserRequest.GetUserId())
	require.Equal(t, int64(1001), userClient.getUserProfileRequest.GetUserId())
	require.Equal(t, "user@example.com", resp.GetUser().GetEmail())
	require.Equal(t, "display name", resp.GetProfile().GetName())
}

func TestGetCurrentUserAppliesAuthenticatedQuotaAfterTokenVerification(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	userClient := new(fakeUserClient)
	resolver, err := apiratelimit.NewClientIPResolver(nil)
	require.NoError(t, err)
	path, handler := apiv1connect.NewUserServiceHandler(
		NewUser(&svc.ServiceContext{
			AuthenticatorClient: authenticatorClient,
			UserClient:          userClient,
		}),
		connect.WithInterceptors(apiratelimit.UnaryInterceptor(authenticatedQuotaLimiter{}, resolver)),
	)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	client := apiv1connect.NewUserServiceClient(
		&http.Client{Transport: bearerRoundTripper{
			base:        http.DefaultTransport,
			accessToken: "access-token",
		}},
		httpServer.URL,
	)

	req := new(apiv1.GetCurrentUserRequest)
	_, err = client.GetCurrentUser(t.Context(), req)
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	connectErr := new(connect.Error)
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, "60", connectErr.Meta().Get("Retry-After"))
	require.NotNil(t, authenticatorClient.verifyRequest)
	require.Nil(t, userClient.getUserRequest)
}

func TestGetCurrentUserRequiresAccessToken(t *testing.T) {
	client, closeServer := newUserHTTPClient(t, &fakeAuthenticatorClient{}, &fakeUserClient{}, "")
	defer closeServer()

	req := new(apiv1.GetCurrentUserRequest)
	_, err := client.GetCurrentUser(context.Background(), req)
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	require.Equal(t, apierror.CodeInvalidAccessToken, publicErrorInfo(t, err).GetCode())
}

func TestGetUserProfileIsPublic(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{}
	userClient := &fakeUserClient{
		getUserProfileResponse: getUserProfileResponse(internalUserProfile()),
	}
	server := NewUser(&svc.ServiceContext{
		AuthenticatorClient: authenticatorClient,
		UserClient:          userClient,
	})

	req := new(apiv1.GetUserProfileRequest)
	req.SetUserId(1001)
	resp, err := server.GetUserProfile(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, authenticatorClient.verifyRequest)
	require.Equal(t, int64(1001), userClient.getUserProfileRequest.GetUserId())
	require.Equal(t, "display name", resp.GetProfile().GetName())
}

func TestCheckEmailAvailability(t *testing.T) {
	svcResp := new(userv1.CheckEmailAvailabilityResponse)
	svcResp.SetAvailable(true)
	userClient := &fakeUserClient{
		checkEmailAvailabilityResponse: svcResp,
	}
	server := NewUser(&svc.ServiceContext{UserClient: userClient})

	req := new(apiv1.CheckEmailAvailabilityRequest)
	req.SetEmail("user@example.com")
	resp, err := server.CheckEmailAvailability(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "user@example.com", userClient.checkEmailAvailabilityRequest.GetEmail())
	require.True(t, resp.GetAvailable())
}

func TestUpdateEmailUsesAuthenticatedUser(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	svcResp := new(userv1.UpdateEmailResponse)
	svcResp.SetUser(internalUser())
	userClient := &fakeUserClient{
		updateEmailResponse: svcResp,
	}
	client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
	defer closeServer()

	req := new(apiv1.UpdateEmailRequest)
	req.SetEmail("new@example.com")
	resp, err := client.UpdateEmail(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), userClient.updateEmailRequest.GetUserId())
	require.Equal(t, "new@example.com", userClient.updateEmailRequest.GetEmail())
	require.Equal(t, int64(1001), resp.GetUser().GetUserId())
}

func TestUpdateUserProfileUsesAuthenticatedUser(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	svcResp := new(userv1.UpdateUserProfileResponse)
	svcResp.SetProfile(internalUserProfile())
	userClient := &fakeUserClient{
		updateUserProfileResponse: svcResp,
	}
	client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
	defer closeServer()

	req := new(apiv1.UpdateUserProfileRequest)
	req.SetName("new name")
	resp, err := client.UpdateUserProfile(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), userClient.updateUserProfileRequest.GetUserId())
	require.True(t, userClient.updateUserProfileRequest.HasName())
	require.Equal(t, "new name", userClient.updateUserProfileRequest.GetName())
	require.Equal(t, int64(1001), resp.GetProfile().GetUserId())

	req = new(apiv1.UpdateUserProfileRequest)
	req.SetName("")
	_, err = client.UpdateUserProfile(context.Background(), req)
	require.NoError(t, err)
	require.True(t, userClient.updateUserProfileRequest.HasName())
	require.Empty(t, userClient.updateUserProfileRequest.GetName())
}

func TestCreateAvatarUploadUsesAuthenticatedUser(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	svcResp := new(userv1.CreateAvatarUploadResponse)
	svcResp.SetUploadId(7001)
	svcResp.SetPresignedUrl("https://upload.example/7001")
	svcResp.SetExpiresAt(9001)
	userClient := &fakeUserClient{createAvatarUploadResponse: svcResp}
	client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
	defer closeServer()

	req := new(apiv1.CreateAvatarUploadRequest)
	req.SetExpectedSize(123)
	req.SetContentType("image/png")
	resp, err := client.CreateAvatarUpload(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), userClient.createAvatarUploadRequest.GetUserId())
	require.Equal(t, int64(123), userClient.createAvatarUploadRequest.GetExpectedSize())
	require.Equal(t, "image/png", userClient.createAvatarUploadRequest.GetContentType())
	require.Equal(t, int64(7001), resp.GetUploadId())
}

func TestChangePasswordUsesAuthenticatedUser(t *testing.T) {
	svcResp := new(authenticatorv1.ChangePasswordResponse)
	svcResp.SetOk(true)
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse:         verifyAccessTokenResponse(1001),
		changePasswordResponse: svcResp,
	}
	client, closeServer := newUserHTTPClient(t, authenticatorClient, &fakeUserClient{}, "access-token")
	defer closeServer()

	req := new(apiv1.ChangePasswordRequest)
	req.SetOldPassword("old-password")
	req.SetNewPassword("new-password")
	resp, err := client.ChangePassword(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), authenticatorClient.changePasswordRequest.GetUserId())
	require.Equal(t, int64(2001), authenticatorClient.changePasswordRequest.GetCurrentSessionId())
	require.Equal(t, "old-password", authenticatorClient.changePasswordRequest.GetOldPassword())
	require.Equal(t, "new-password", authenticatorClient.changePasswordRequest.GetNewPassword())
	require.True(t, resp.GetOk())
}

func TestUserErrorMappings(t *testing.T) {
	tests := map[string]struct {
		err         error
		connectCode connect.Code
		publicCode  string
	}{
		"email already exists": {
			err:         rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists, "email already exists"),
			connectCode: connect.CodeAlreadyExists,
			publicCode:  apierror.CodeEmailAlreadyExists,
		},
		"invalid argument": {
			err:         status.Error(codes.InvalidArgument, "bad input"),
			connectCode: connect.CodeInvalidArgument,
			publicCode:  apierror.CodeInvalidArgument,
		},
		"generic not found": {
			err:         status.Error(codes.NotFound, "user not found"),
			connectCode: connect.CodeNotFound,
			publicCode:  apierror.CodeNotFound,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			userClient := &fakeUserClient{getUserProfileError: tt.err}
			server := NewUser(&svc.ServiceContext{UserClient: userClient})
			req := new(apiv1.GetUserProfileRequest)
			req.SetUserId(1001)
			_, err := server.GetUserProfile(context.Background(), req)
			require.Equal(t, tt.connectCode, connect.CodeOf(err))
			require.Equal(t, tt.publicCode, publicErrorInfo(t, err).GetCode())
		})
	}
}

func newUserHTTPClient(
	t *testing.T,
	authenticatorClient *fakeAuthenticatorClient,
	userClient *fakeUserClient,
	accessToken string,
) (apiv1connect.UserServiceClient, func()) {
	t.Helper()

	svcCtx := &svc.ServiceContext{
		AuthenticatorClient: authenticatorClient,
		UserClient:          userClient,
	}
	path, handler := apiv1connect.NewUserServiceHandler(NewUser(svcCtx))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)

	httpClient := &http.Client{Transport: bearerRoundTripper{
		base:        http.DefaultTransport,
		accessToken: accessToken,
	}}
	return apiv1connect.NewUserServiceClient(httpClient, httpServer.URL), httpServer.Close
}

func verifyAccessTokenResponse(userID int64) *authenticatorv1.VerifyAccessTokenResponse {
	resp := new(authenticatorv1.VerifyAccessTokenResponse)
	resp.SetOk(true)
	resp.SetUserId(userID)
	resp.SetSessionId(2001)
	resp.SetExpiresAt(3001)
	return resp
}

func internalUser() *userv1.User {
	user := new(userv1.User)
	user.SetUserId(1001)
	user.SetEmail("user@example.com")
	user.SetCreatedAt(2001)
	user.SetUpdatedAt(3001)
	return user
}

func internalUserProfile() *userv1.UserProfile {
	profile := new(userv1.UserProfile)
	profile.SetUserId(1001)
	profile.SetName("display name")
	profile.SetAvatarAssetId(6001)
	profile.SetCreatedAt(2001)
	profile.SetUpdatedAt(3001)
	return profile
}

func getUserResponse(user *userv1.User) *userv1.GetUserResponse {
	resp := new(userv1.GetUserResponse)
	resp.SetUser(user)
	return resp
}

func getUserProfileResponse(profile *userv1.UserProfile) *userv1.GetUserProfileResponse {
	resp := new(userv1.GetUserProfileResponse)
	resp.SetProfile(profile)
	return resp
}

type bearerRoundTripper struct {
	base        http.RoundTripper
	accessToken string
}

func (r bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	if r.accessToken != "" {
		cloned.Header.Set("Authorization", bearerPrefix+r.accessToken)
	}
	return r.base.RoundTrip(cloned)
}
