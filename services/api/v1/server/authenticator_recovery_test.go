package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

func (f *fakeAuthenticatorClient) RequestPasswordReset(_ context.Context, req *authenticatorv1.RequestPasswordResetRequest, _ ...grpc.CallOption) (*authenticatorv1.RequestPasswordResetResponse, error) {
	f.requestPasswordResetRequest = req
	if f.requestPasswordResetError != nil {
		return nil, f.requestPasswordResetError
	}
	return f.requestPasswordResetResponse, nil
}

func (f *fakeAuthenticatorClient) ConfirmPasswordReset(_ context.Context, req *authenticatorv1.ConfirmPasswordResetRequest, _ ...grpc.CallOption) (*authenticatorv1.ConfirmPasswordResetResponse, error) {
	f.confirmPasswordResetRequest = req
	if f.confirmPasswordResetError != nil {
		return nil, f.confirmPasswordResetError
	}
	return f.confirmPasswordResetResponse, nil
}

func (f *fakeAuthenticatorClient) RequestEmailVerification(_ context.Context, req *authenticatorv1.RequestEmailVerificationRequest, _ ...grpc.CallOption) (*authenticatorv1.RequestEmailVerificationResponse, error) {
	f.requestEmailVerificationRequest = req
	if f.requestEmailVerificationError != nil {
		return nil, f.requestEmailVerificationError
	}
	return f.requestEmailVerificationResponse, nil
}

func (f *fakeAuthenticatorClient) ConfirmEmailVerification(_ context.Context, req *authenticatorv1.ConfirmEmailVerificationRequest, _ ...grpc.CallOption) (*authenticatorv1.ConfirmEmailVerificationResponse, error) {
	f.confirmEmailVerificationRequest = req
	if f.confirmEmailVerificationError != nil {
		return nil, f.confirmEmailVerificationError
	}
	return f.confirmEmailVerificationResponse, nil
}

func okBoolResponse[T interface{ SetOk(bool) }](resp T) T {
	resp.SetOk(true)
	return resp
}

func TestRequestPasswordResetForwardsEmail(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		requestPasswordResetResponse: okBoolResponse(new(authenticatorv1.RequestPasswordResetResponse)),
	}
	server := NewAuthenticator(&svc.ServiceContext{AuthenticatorClient: internalClient})

	resp, err := server.RequestPasswordReset(context.Background(), &apiv1.RequestPasswordResetRequest{
		Email: new("user@example.com"),
	})
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, "user@example.com", internalClient.requestPasswordResetRequest.GetEmail())
}

func TestConfirmPasswordResetForwardsTokenAndMapsError(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		confirmPasswordResetResponse: okBoolResponse(new(authenticatorv1.ConfirmPasswordResetResponse)),
	}
	server := NewAuthenticator(&svc.ServiceContext{AuthenticatorClient: internalClient})

	resp, err := server.ConfirmPasswordReset(context.Background(), &apiv1.ConfirmPasswordResetRequest{
		Token: new("raw-token"), NewPassword: new("new-password"),
	})
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, "raw-token", internalClient.confirmPasswordResetRequest.GetToken())
	require.Equal(t, "new-password", internalClient.confirmPasswordResetRequest.GetNewPassword())

	internalClient.confirmPasswordResetError = rpcerror.New(
		codes.InvalidArgument,
		rpcerror.AuthenticatorDomain,
		rpcerror.AuthenticatorInvalidPasswordResetToken,
		"invalid or expired password reset token",
	)
	_, err = server.ConfirmPasswordReset(context.Background(), &apiv1.ConfirmPasswordResetRequest{
		Token: new("bad-token"), NewPassword: new("new-password"),
	})
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestRequestEmailVerificationUsesAuthenticatedUser(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:                   verifyAccessTokenResponse(1001),
		requestEmailVerificationResponse: okBoolResponse(new(authenticatorv1.RequestEmailVerificationResponse)),
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	resp, err := client.RequestEmailVerification(context.Background(), &apiv1.RequestEmailVerificationRequest{})
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, int64(1001), internalClient.requestEmailVerificationRequest.GetUserId())
}

func TestRequestEmailVerificationRequiresAccessToken(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "")
	defer closeServer()

	_, err := client.RequestEmailVerification(context.Background(), &apiv1.RequestEmailVerificationRequest{})
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	require.Nil(t, internalClient.requestEmailVerificationRequest)
}

func TestConfirmEmailVerificationForwardsToken(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		confirmEmailVerificationResponse: okBoolResponse(new(authenticatorv1.ConfirmEmailVerificationResponse)),
	}
	server := NewAuthenticator(&svc.ServiceContext{AuthenticatorClient: internalClient})

	resp, err := server.ConfirmEmailVerification(context.Background(), &apiv1.ConfirmEmailVerificationRequest{
		Token: new("raw-verify-token"),
	})
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, "raw-verify-token", internalClient.confirmEmailVerificationRequest.GetToken())
}
