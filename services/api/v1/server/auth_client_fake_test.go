package server

import (
	"context"

	"google.golang.org/grpc"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
)

type fakeAuthenticatorClient struct {
	authenticatorv1.AuthenticatorServiceClient
	registerRequest                *authenticatorv1.RegisterRequest
	registerResponse               *authenticatorv1.RegisterResponse
	registerError                  error
	loginRequest                   *authenticatorv1.LoginRequest
	loginResponse                  *authenticatorv1.LoginResponse
	loginError                     error
	refreshRequest                 *authenticatorv1.RefreshRequest
	refreshResponse                *authenticatorv1.RefreshResponse
	refreshError                   error
	logoutRequest                  *authenticatorv1.LogoutRequest
	logoutResponse                 *authenticatorv1.LogoutResponse
	logoutError                    error
	verifyRequest                  *authenticatorv1.VerifyAccessTokenRequest
	verifyResponse                 *authenticatorv1.VerifyAccessTokenResponse
	verifyError                    error
	authenticateCookieRequest      *authenticatorv1.AuthenticateCookieRequest
	authenticateCookieResponse     *authenticatorv1.AuthenticateCookieResponse
	authenticateCookieError        error
	createGatewayTicketRequest     *authenticatorv1.CreateGatewayTicketRequest
	createGatewayTicketResponse    *authenticatorv1.CreateGatewayTicketResponse
	createGatewayTicketError       error
	listSessionsRequest            *authenticatorv1.ListSessionsRequest
	listSessionsResponse           *authenticatorv1.ListSessionsResponse
	listSessionsError              error
	revokeUserSessionRequest       *authenticatorv1.RevokeUserSessionRequest
	revokeUserSessionResponse      *authenticatorv1.RevokeUserSessionResponse
	revokeUserSessionError         error
	revokeOtherSessionsRequest     *authenticatorv1.RevokeOtherSessionsRequest
	revokeOtherSessionsResponse    *authenticatorv1.RevokeOtherSessionsResponse
	revokeOtherSessionsError       error
	completeTwoFactorLoginRequest  *authenticatorv1.CompleteTwoFactorLoginRequest
	completeTwoFactorLoginResponse *authenticatorv1.CompleteTwoFactorLoginResponse
	completeTwoFactorLoginError    error
	twoFactorStatusRequest         *authenticatorv1.GetTwoFactorStatusRequest
	twoFactorStatusResponse        *authenticatorv1.GetTwoFactorStatusResponse
	twoFactorStatusError           error
	beginEnrollmentRequest         *authenticatorv1.BeginTwoFactorEnrollmentRequest
	beginEnrollmentResponse        *authenticatorv1.BeginTwoFactorEnrollmentResponse
	beginEnrollmentError           error
	confirmEnrollmentRequest       *authenticatorv1.ConfirmTwoFactorEnrollmentRequest
	confirmEnrollmentResponse      *authenticatorv1.ConfirmTwoFactorEnrollmentResponse
	confirmEnrollmentError         error
	disableTwoFactorRequest        *authenticatorv1.DisableTwoFactorRequest
	disableTwoFactorResponse       *authenticatorv1.DisableTwoFactorResponse
	disableTwoFactorError          error

	requestPasswordResetRequest      *authenticatorv1.RequestPasswordResetRequest
	requestPasswordResetResponse     *authenticatorv1.RequestPasswordResetResponse
	requestPasswordResetError        error
	confirmPasswordResetRequest      *authenticatorv1.ConfirmPasswordResetRequest
	confirmPasswordResetResponse     *authenticatorv1.ConfirmPasswordResetResponse
	confirmPasswordResetError        error
	requestEmailVerificationRequest  *authenticatorv1.RequestEmailVerificationRequest
	requestEmailVerificationResponse *authenticatorv1.RequestEmailVerificationResponse
	requestEmailVerificationError    error
	confirmEmailVerificationRequest  *authenticatorv1.ConfirmEmailVerificationRequest
	confirmEmailVerificationResponse *authenticatorv1.ConfirmEmailVerificationResponse
	confirmEmailVerificationError    error
	regenRecoveryCodesRequest        *authenticatorv1.RegenerateTwoFactorRecoveryCodesRequest
	regenRecoveryCodesResponse       *authenticatorv1.RegenerateTwoFactorRecoveryCodesResponse
	regenRecoveryCodesError          error

	changePasswordRequest  *authenticatorv1.ChangePasswordRequest
	changePasswordResponse *authenticatorv1.ChangePasswordResponse
	changePasswordError    error
}

func (f *fakeAuthenticatorClient) ChangePassword(_ context.Context, req *authenticatorv1.ChangePasswordRequest, _ ...grpc.CallOption) (*authenticatorv1.ChangePasswordResponse, error) {
	f.changePasswordRequest = req
	if f.changePasswordError != nil {
		return nil, f.changePasswordError
	}
	return f.changePasswordResponse, nil
}

func (f *fakeAuthenticatorClient) Register(_ context.Context, req *authenticatorv1.RegisterRequest, _ ...grpc.CallOption) (*authenticatorv1.RegisterResponse, error) {
	f.registerRequest = req
	if f.registerError != nil {
		return nil, f.registerError
	}
	return f.registerResponse, nil
}

func (f *fakeAuthenticatorClient) Login(_ context.Context, req *authenticatorv1.LoginRequest, _ ...grpc.CallOption) (*authenticatorv1.LoginResponse, error) {
	f.loginRequest = req
	if f.loginError != nil {
		return nil, f.loginError
	}
	return f.loginResponse, nil
}

func (f *fakeAuthenticatorClient) Refresh(_ context.Context, req *authenticatorv1.RefreshRequest, _ ...grpc.CallOption) (*authenticatorv1.RefreshResponse, error) {
	f.refreshRequest = req
	if f.refreshError != nil {
		return nil, f.refreshError
	}
	return f.refreshResponse, nil
}

func (f *fakeAuthenticatorClient) Logout(_ context.Context, req *authenticatorv1.LogoutRequest, _ ...grpc.CallOption) (*authenticatorv1.LogoutResponse, error) {
	f.logoutRequest = req
	if f.logoutError != nil {
		return nil, f.logoutError
	}
	return f.logoutResponse, nil
}

func (f *fakeAuthenticatorClient) VerifyAccessToken(_ context.Context, req *authenticatorv1.VerifyAccessTokenRequest, _ ...grpc.CallOption) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	f.verifyRequest = req
	if f.verifyError != nil {
		return nil, f.verifyError
	}
	return f.verifyResponse, nil
}

func (f *fakeAuthenticatorClient) AuthenticateCookie(_ context.Context, req *authenticatorv1.AuthenticateCookieRequest, _ ...grpc.CallOption) (*authenticatorv1.AuthenticateCookieResponse, error) {
	f.authenticateCookieRequest = req
	return f.authenticateCookieResponse, f.authenticateCookieError
}

func (f *fakeAuthenticatorClient) CreateGatewayTicket(_ context.Context, req *authenticatorv1.CreateGatewayTicketRequest, _ ...grpc.CallOption) (*authenticatorv1.CreateGatewayTicketResponse, error) {
	f.createGatewayTicketRequest = req
	return f.createGatewayTicketResponse, f.createGatewayTicketError
}

func (f *fakeAuthenticatorClient) ListSessions(_ context.Context, req *authenticatorv1.ListSessionsRequest, _ ...grpc.CallOption) (*authenticatorv1.ListSessionsResponse, error) {
	f.listSessionsRequest = req
	return f.listSessionsResponse, f.listSessionsError
}

func (f *fakeAuthenticatorClient) RevokeUserSession(_ context.Context, req *authenticatorv1.RevokeUserSessionRequest, _ ...grpc.CallOption) (*authenticatorv1.RevokeUserSessionResponse, error) {
	f.revokeUserSessionRequest = req
	return f.revokeUserSessionResponse, f.revokeUserSessionError
}

func (f *fakeAuthenticatorClient) RevokeOtherSessions(_ context.Context, req *authenticatorv1.RevokeOtherSessionsRequest, _ ...grpc.CallOption) (*authenticatorv1.RevokeOtherSessionsResponse, error) {
	f.revokeOtherSessionsRequest = req
	return f.revokeOtherSessionsResponse, f.revokeOtherSessionsError
}

func (f *fakeAuthenticatorClient) CompleteTwoFactorLogin(_ context.Context, req *authenticatorv1.CompleteTwoFactorLoginRequest, _ ...grpc.CallOption) (*authenticatorv1.CompleteTwoFactorLoginResponse, error) {
	f.completeTwoFactorLoginRequest = req
	return f.completeTwoFactorLoginResponse, f.completeTwoFactorLoginError
}

func (f *fakeAuthenticatorClient) GetTwoFactorStatus(_ context.Context, req *authenticatorv1.GetTwoFactorStatusRequest, _ ...grpc.CallOption) (*authenticatorv1.GetTwoFactorStatusResponse, error) {
	f.twoFactorStatusRequest = req
	return f.twoFactorStatusResponse, f.twoFactorStatusError
}

func (f *fakeAuthenticatorClient) BeginTwoFactorEnrollment(_ context.Context, req *authenticatorv1.BeginTwoFactorEnrollmentRequest, _ ...grpc.CallOption) (*authenticatorv1.BeginTwoFactorEnrollmentResponse, error) {
	f.beginEnrollmentRequest = req
	return f.beginEnrollmentResponse, f.beginEnrollmentError
}

func (f *fakeAuthenticatorClient) ConfirmTwoFactorEnrollment(_ context.Context, req *authenticatorv1.ConfirmTwoFactorEnrollmentRequest, _ ...grpc.CallOption) (*authenticatorv1.ConfirmTwoFactorEnrollmentResponse, error) {
	f.confirmEnrollmentRequest = req
	return f.confirmEnrollmentResponse, f.confirmEnrollmentError
}

func (f *fakeAuthenticatorClient) DisableTwoFactor(_ context.Context, req *authenticatorv1.DisableTwoFactorRequest, _ ...grpc.CallOption) (*authenticatorv1.DisableTwoFactorResponse, error) {
	f.disableTwoFactorRequest = req
	return f.disableTwoFactorResponse, f.disableTwoFactorError
}

func (f *fakeAuthenticatorClient) RegenerateTwoFactorRecoveryCodes(_ context.Context, req *authenticatorv1.RegenerateTwoFactorRecoveryCodesRequest, _ ...grpc.CallOption) (*authenticatorv1.RegenerateTwoFactorRecoveryCodesResponse, error) {
	f.regenRecoveryCodesRequest = req
	return f.regenRecoveryCodesResponse, f.regenRecoveryCodesError
}
