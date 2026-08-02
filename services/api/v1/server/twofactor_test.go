package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

func TestCompleteTwoFactorLoginMapsRequestAndResponse(t *testing.T) {
	internalClient := &fakeAuthenticatorClient{
		completeTwoFactorLoginResponse: completeTwoFactorLoginResponse(authenticationResult()),
	}
	server := NewAuthenticator(&svc.ServiceContext{AuthenticatorClient: internalClient})

	req := new(apiv1.CompleteTwoFactorLoginRequest)
	req.SetChallengeToken("challenge-token")
	req.SetCode("123456")
	resp, err := server.CompleteTwoFactorLogin(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "challenge-token", internalClient.completeTwoFactorLoginRequest.GetChallengeToken())
	require.Equal(t, "123456", internalClient.completeTwoFactorLoginRequest.GetCode())
	assertAPIAuthenticationResult(t, resp.GetResult())
}

func TestGetTwoFactorStatus(t *testing.T) {
	svcResp := new(authenticatorv1.GetTwoFactorStatusResponse)
	svcResp.SetEnabled(true)
	svcResp.SetRecoveryCodesRemaining(8)
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:          verifyAccessTokenResponse(1001),
		twoFactorStatusResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	resp, err := client.GetTwoFactorStatus(context.Background(), new(apiv1.GetTwoFactorStatusRequest))
	require.NoError(t, err)
	require.Equal(t, int64(1001), internalClient.twoFactorStatusRequest.GetUserId())
	require.True(t, resp.GetEnabled())
	require.Equal(t, int32(8), resp.GetRecoveryCodesRemaining())
}

func TestBeginTwoFactorEnrollment(t *testing.T) {
	svcResp := new(authenticatorv1.BeginTwoFactorEnrollmentResponse)
	svcResp.SetEnrollmentToken("enroll-token")
	svcResp.SetOtpauthUri("otpauth://totp/...")
	svcResp.SetManualEntryKey("ABCDEFGHIJKLMNOP")
	svcResp.SetExpiresAt(3001)
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:          verifyAccessTokenResponse(1001),
		beginEnrollmentResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	req := new(apiv1.BeginTwoFactorEnrollmentRequest)
	req.SetPassword("password")
	resp, err := client.BeginTwoFactorEnrollment(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), internalClient.beginEnrollmentRequest.GetUserId())
	require.Equal(t, "password", internalClient.beginEnrollmentRequest.GetPassword())
	require.Equal(t, "enroll-token", resp.GetEnrollmentToken())
	require.Equal(t, "otpauth://totp/...", resp.GetOtpauthUri())
	require.Equal(t, "ABCDEFGHIJKLMNOP", resp.GetManualEntryKey())
	require.Equal(t, int64(3001), resp.GetExpiresAt())
}

func TestConfirmTwoFactorEnrollment(t *testing.T) {
	svcResp := new(authenticatorv1.ConfirmTwoFactorEnrollmentResponse)
	svcResp.SetRecoveryCodes([]string{"code1", "code2"})
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:            verifyAccessTokenResponse(1001),
		confirmEnrollmentResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	req := new(apiv1.ConfirmTwoFactorEnrollmentRequest)
	req.SetEnrollmentToken("enroll-token")
	req.SetCode("123456")
	resp, err := client.ConfirmTwoFactorEnrollment(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), internalClient.confirmEnrollmentRequest.GetUserId())
	require.Equal(t, int64(2001), internalClient.confirmEnrollmentRequest.GetCurrentSessionId())
	require.Equal(t, "enroll-token", internalClient.confirmEnrollmentRequest.GetEnrollmentToken())
	require.Equal(t, "123456", internalClient.confirmEnrollmentRequest.GetCode())
	require.Equal(t, []string{"code1", "code2"}, resp.GetRecoveryCodes())
}

func TestDisableTwoFactorWithCode(t *testing.T) {
	svcResp := new(authenticatorv1.DisableTwoFactorResponse)
	svcResp.SetOk(true)
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:           verifyAccessTokenResponse(1001),
		disableTwoFactorResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	req := new(apiv1.DisableTwoFactorRequest)
	req.SetPassword("password")
	req.SetCode("123456")
	resp, err := client.DisableTwoFactor(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), internalClient.disableTwoFactorRequest.GetUserId())
	require.Equal(t, int64(2001), internalClient.disableTwoFactorRequest.GetCurrentSessionId())
	require.Equal(t, "password", internalClient.disableTwoFactorRequest.GetPassword())
	require.Equal(t, "123456", internalClient.disableTwoFactorRequest.GetCode())
	require.False(t, internalClient.disableTwoFactorRequest.HasRecoveryCode())
	require.True(t, resp.GetOk())
}

func TestDisableTwoFactorWithRecoveryCode(t *testing.T) {
	svcResp := new(authenticatorv1.DisableTwoFactorResponse)
	svcResp.SetOk(true)
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:           verifyAccessTokenResponse(1001),
		disableTwoFactorResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	req := new(apiv1.DisableTwoFactorRequest)
	req.SetPassword("password")
	req.SetRecoveryCode("recovery-code")
	resp, err := client.DisableTwoFactor(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "recovery-code", internalClient.disableTwoFactorRequest.GetRecoveryCode())
	require.True(t, resp.GetOk())
}

func TestRegenerateTwoFactorRecoveryCodes(t *testing.T) {
	svcResp := new(authenticatorv1.RegenerateTwoFactorRecoveryCodesResponse)
	svcResp.SetRecoveryCodes([]string{"new1", "new2", "new3"})
	internalClient := &fakeAuthenticatorClient{
		verifyResponse:             verifyAccessTokenResponse(1001),
		regenRecoveryCodesResponse: svcResp,
	}
	client, closeServer := newAuthenticatorHTTPClient(t, internalClient, "access-token")
	defer closeServer()

	req := new(apiv1.RegenerateTwoFactorRecoveryCodesRequest)
	req.SetPassword("password")
	req.SetCode("123456")
	resp, err := client.RegenerateTwoFactorRecoveryCodes(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), internalClient.regenRecoveryCodesRequest.GetUserId())
	require.Equal(t, int64(2001), internalClient.regenRecoveryCodesRequest.GetCurrentSessionId())
	require.Equal(t, "password", internalClient.regenRecoveryCodesRequest.GetPassword())
	require.Equal(t, "123456", internalClient.regenRecoveryCodesRequest.GetCode())
	require.Equal(t, []string{"new1", "new2", "new3"}, resp.GetRecoveryCodes())
}
