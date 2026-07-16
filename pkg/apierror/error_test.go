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
