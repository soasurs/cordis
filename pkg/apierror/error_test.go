package apierror

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFromRPCReason(t *testing.T) {
	err := rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists, "email already exists")
	connectErr := FromRPC(err)

	if connect.CodeOf(connectErr) != connect.CodeAlreadyExists {
		t.Fatalf("connect code = %v, want %v", connect.CodeOf(connectErr), connect.CodeAlreadyExists)
	}
	if publicErrorInfo(t, connectErr).GetCode() != CodeEmailAlreadyExists {
		t.Fatalf("public code = %q, want %q", publicErrorInfo(t, connectErr).GetCode(), CodeEmailAlreadyExists)
	}
}

func TestFromRPCStatusCode(t *testing.T) {
	connectErr := FromRPC(status.Error(codes.InvalidArgument, "bad request"))

	if connect.CodeOf(connectErr) != connect.CodeInvalidArgument {
		t.Fatalf("connect code = %v, want %v", connect.CodeOf(connectErr), connect.CodeInvalidArgument)
	}
	if publicErrorInfo(t, connectErr).GetCode() != CodeInvalidArgument {
		t.Fatalf("public code = %q, want %q", publicErrorInfo(t, connectErr).GetCode(), CodeInvalidArgument)
	}
}

func TestFromRPCUnknownReasonIsInternal(t *testing.T) {
	err := rpcerror.New(codes.NotFound, "unknown.cordis", "missing", "missing")
	connectErr := FromRPC(err)

	if connect.CodeOf(connectErr) != connect.CodeInternal {
		t.Fatalf("connect code = %v, want %v", connect.CodeOf(connectErr), connect.CodeInternal)
	}
	if publicErrorInfo(t, connectErr).GetCode() != CodeInternal {
		t.Fatalf("public code = %q, want %q", publicErrorInfo(t, connectErr).GetCode(), CodeInternal)
	}
}

func publicErrorInfo(t *testing.T, err error) *apiv1.PublicErrorInfo {
	t.Helper()

	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error: %v", err)
	}

	for _, detail := range connectErr.Details() {
		value, err := detail.Value()
		if err != nil {
			t.Fatalf("decode detail: %v", err)
		}
		publicInfo, ok := value.(*apiv1.PublicErrorInfo)
		if ok {
			return publicInfo
		}
	}

	t.Fatal("missing public error info")
	return nil
}
