package apierror

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
)

func TestFromRPCReason(t *testing.T) {
	err := rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists, "email already exists")
	connectErr := FromRPC(err)

	require.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(connectErr))
	require.Equal(t, CodeEmailAlreadyExists, publicErrorInfo(t, connectErr).GetCode())
}

func TestFromRPCRegistrationReasons(t *testing.T) {
	tests := []struct {
		code        codes.Code
		reason      string
		connectCode connect.Code
		publicCode  string
	}{
		{
			code: codes.InvalidArgument, reason: rpcerror.AuthenticatorInvalidRegistrationInvite,
			connectCode: connect.CodeInvalidArgument, publicCode: CodeInvalidRegistrationInvite,
		},
		{
			code: codes.FailedPrecondition, reason: rpcerror.AuthenticatorRegistrationClosed,
			connectCode: connect.CodeFailedPrecondition, publicCode: CodeRegistrationClosed,
		},
	}
	for _, tc := range tests {
		err := rpcerror.New(tc.code, rpcerror.AuthenticatorDomain, tc.reason, "registration error")
		connectErr := FromRPC(err)
		require.Equal(t, tc.connectCode, connect.CodeOf(connectErr))
		require.Equal(t, tc.publicCode, publicErrorInfo(t, connectErr).GetCode())
	}
}

func TestFromRPCGuildMemberAlreadyExists(t *testing.T) {
	err := rpcerror.New(
		codes.AlreadyExists,
		rpcerror.GuildDomain,
		rpcerror.GuildMemberAlreadyExists,
		"guild member already exists",
	)
	connectErr := FromRPC(err)

	require.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(connectErr))
	require.Equal(t, CodeAlreadyExists, publicErrorInfo(t, connectErr).GetCode())
}

func TestFromRPCAvatarReasonsHaveDistinctPublicCodes(t *testing.T) {
	tests := []struct {
		reason     string
		publicCode string
	}{
		{reason: rpcerror.MediaAvatarFileTooLarge, publicCode: CodeAvatarFileTooLarge},
		{reason: rpcerror.MediaAvatarContentTypeInvalid, publicCode: CodeAvatarContentTypeInvalid},
		{reason: rpcerror.MediaAvatarDimensionsExceeded, publicCode: CodeAvatarDimensionsExceeded},
		{reason: rpcerror.MediaAvatarPixelsExceeded, publicCode: CodeAvatarPixelsExceeded},
	}
	for _, tc := range tests {
		t.Run(tc.reason, func(t *testing.T) {
			err := rpcerror.New(codes.InvalidArgument, rpcerror.MediaDomain, tc.reason, "invalid avatar")
			connectErr := FromRPC(err)

			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(connectErr))
			require.Equal(t, tc.publicCode, publicErrorInfo(t, connectErr).GetCode())
		})
	}
}

func TestFromRPCResourceLimitExceeded(t *testing.T) {
	for _, tc := range []struct {
		domain string
		reason string
	}{
		{rpcerror.GuildDomain, rpcerror.GuildResourceLimitExceeded},
		{rpcerror.MessageDomain, rpcerror.MessageResourceLimitExceeded},
	} {
		err := rpcerror.New(codes.ResourceExhausted, tc.domain, tc.reason, "resource limit exceeded")
		connectErr := FromRPC(err)
		require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(connectErr))
	}
}

func TestFromRPCGuildChannelLayoutConflict(t *testing.T) {
	err := rpcerror.New(
		codes.Aborted,
		rpcerror.GuildDomain,
		rpcerror.GuildChannelLayoutConflict,
		"guild channel layout changed",
	)
	connectErr := FromRPC(err)

	require.Equal(t, connect.CodeAborted, connect.CodeOf(connectErr))
	require.Equal(t, CodeConflict, publicErrorInfo(t, connectErr).GetCode())
}

func TestFromRPCStatusCode(t *testing.T) {
	connectErr := FromRPC(status.Error(codes.InvalidArgument, "bad request"))

	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(connectErr))
	require.Equal(t, CodeInvalidArgument, publicErrorInfo(t, connectErr).GetCode())
}

func TestFromRPCUnknownReasonIsInternal(t *testing.T) {
	err := rpcerror.New(codes.NotFound, "unknown.cordis", "missing", "missing")
	connectErr := FromRPC(err)

	require.Equal(t, connect.CodeInternal, connect.CodeOf(connectErr))
	require.Equal(t, CodeInternal, publicErrorInfo(t, connectErr).GetCode())
}

func publicErrorInfo(t *testing.T, err error) *apiv1.PublicErrorInfo {
	t.Helper()

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)

	for _, detail := range connectErr.Details() {
		value, err := detail.Value()
		require.NoError(t, err, "decode detail")
		publicInfo, ok := value.(*apiv1.PublicErrorInfo)
		if ok {
			return publicInfo
		}
	}

	require.FailNow(t, "missing public error info")
	return nil
}
