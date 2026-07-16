package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/services/api/v1/svc"
	"google.golang.org/grpc"
)

type fakeUserClient struct {
	userv1.UserServiceClient
	getUserRequest                 *userv1.GetUserRequest
	getUserResponse                *userv1.GetUserResponse
	getUserError                   error
	getUserProfileRequest          *userv1.GetUserProfileRequest
	getUserProfileResponse         *userv1.GetUserProfileResponse
	getUserProfileError            error
	checkEmailAvailabilityRequest  *userv1.CheckEmailAvailabilityRequest
	checkEmailAvailabilityResponse *userv1.CheckEmailAvailabilityResponse
	checkEmailAvailabilityError    error
	updateEmailRequest             *userv1.UpdateEmailRequest
	updateEmailResponse            *userv1.UpdateEmailResponse
	updateEmailError               error
	changePasswordRequest          *userv1.ChangePasswordRequest
	changePasswordResponse         *userv1.ChangePasswordResponse
	changePasswordError            error
	updateUserProfileRequest       *userv1.UpdateUserProfileRequest
	updateUserProfileResponse      *userv1.UpdateUserProfileResponse
	updateUserProfileError         error
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

func (f *fakeUserClient) ChangePassword(_ context.Context, req *userv1.ChangePasswordRequest, _ ...grpc.CallOption) (*userv1.ChangePasswordResponse, error) {
	f.changePasswordRequest = req
	return f.changePasswordResponse, f.changePasswordError
}

func (f *fakeUserClient) UpdateUserProfile(_ context.Context, req *userv1.UpdateUserProfileRequest, _ ...grpc.CallOption) (*userv1.UpdateUserProfileResponse, error) {
	f.updateUserProfileRequest = req
	return f.updateUserProfileResponse, f.updateUserProfileError
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

	resp, err := client.GetCurrentUser(context.Background(), &apiv1.GetCurrentUserRequest{})
	require.NoError(t, err)
	require.Equal(t, "access-token", authenticatorClient.verifyRequest.GetAccessToken())
	require.Equal(t, int64(1001), userClient.getUserRequest.GetUserId())
	require.Equal(t, int64(1001), userClient.getUserProfileRequest.GetUserId())
	require.Equal(t, "user@example.com", resp.GetUser().GetEmail())
	require.Equal(t, "display name", resp.GetProfile().GetName())
}

func TestGetCurrentUserRequiresAccessToken(t *testing.T) {
	client, closeServer := newUserHTTPClient(t, &fakeAuthenticatorClient{}, &fakeUserClient{}, "")
	defer closeServer()

	_, err := client.GetCurrentUser(context.Background(), &apiv1.GetCurrentUserRequest{})
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

	resp, err := server.GetUserProfile(context.Background(), &apiv1.GetUserProfileRequest{
		UserId: new(int64(1001)),
	})
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

	resp, err := server.CheckEmailAvailability(context.Background(), &apiv1.CheckEmailAvailabilityRequest{
		Email: new("user@example.com"),
	})
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

	resp, err := client.UpdateEmail(context.Background(), &apiv1.UpdateEmailRequest{
		Email: new("new@example.com"),
	})
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

	resp, err := client.UpdateUserProfile(context.Background(), &apiv1.UpdateUserProfileRequest{
		Name:      new("new name"),
		AvatarUri: new("avatar://2"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), userClient.updateUserProfileRequest.GetUserId())
	require.Equal(t, "new name", userClient.updateUserProfileRequest.GetName())
	require.Equal(t, "avatar://2", userClient.updateUserProfileRequest.GetAvatarUri())
	require.Equal(t, int64(1001), resp.GetProfile().GetUserId())
}

func TestChangePasswordUsesAuthenticatedUser(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse:              verifyAccessTokenResponse(1001),
		revokeOtherSessionsResponse: new(authenticatorv1.RevokeOtherSessionsResponse),
	}
	svcResp := new(userv1.ChangePasswordResponse)
	svcResp.SetOk(true)
	userClient := &fakeUserClient{
		changePasswordResponse: svcResp,
	}
	client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
	defer closeServer()

	resp, err := client.ChangePassword(context.Background(), &apiv1.ChangePasswordRequest{
		OldPassword: new("old-password"),
		NewPassword: new("new-password"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), userClient.changePasswordRequest.GetUserId())
	require.Equal(t, "old-password", userClient.changePasswordRequest.GetOldPassword())
	require.Equal(t, "new-password", userClient.changePasswordRequest.GetNewPassword())
	require.Equal(t, int64(1001), authenticatorClient.revokeOtherSessionsRequest.GetUserId())
	require.Equal(t, int64(2001), authenticatorClient.revokeOtherSessionsRequest.GetCurrentSessionId())
	require.True(t, resp.GetOk())
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
	profile.SetAvatarUri("https://example.com/avatar.png")
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
