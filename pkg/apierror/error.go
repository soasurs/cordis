package apierror

import (
	"errors"

	"connectrpc.com/connect"
	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	CodeInvalidCredentials  = "auth.invalid_credentials"
	CodeEmailAlreadyExists  = "auth.email_already_exists"
	CodeInvalidAccessToken  = "auth.invalid_access_token"
	CodeInvalidRefreshToken = "auth.invalid_refresh_token"
	CodeSessionExpired      = "auth.session_expired"
	CodeSessionRevoked      = "auth.session_revoked"
	CodeInvalidArgument     = "request.invalid_argument"
	CodeCanceled            = "request.canceled"
	CodeDeadlineExceeded    = "request.deadline_exceeded"
	CodeNotFound            = "resource.not_found"
	CodeAlreadyExists       = "resource.already_exists"
	CodePermissionDenied    = "auth.permission_denied"
	CodeResourceExhausted   = "system.resource_exhausted"
	CodeUnavailable         = "system.unavailable"
	CodeInternal            = "system.internal"
)

type mapping struct {
	connectCode connect.Code
	publicCode  string
	message     string
}

var reasonMappings = map[rpcerror.Key]mapping{
	{Domain: rpcerror.AuthenticatorDomain, Reason: rpcerror.AuthenticatorInvalidCredentials}: {
		connectCode: connect.CodeUnauthenticated,
		publicCode:  CodeInvalidCredentials,
		message:     "Invalid email or password.",
	},
	{Domain: rpcerror.AuthenticatorDomain, Reason: rpcerror.AuthenticatorInvalidAccessToken}: {
		connectCode: connect.CodeUnauthenticated,
		publicCode:  CodeInvalidAccessToken,
		message:     "Invalid access token.",
	},
	{Domain: rpcerror.AuthenticatorDomain, Reason: rpcerror.AuthenticatorInvalidRefreshToken}: {
		connectCode: connect.CodeUnauthenticated,
		publicCode:  CodeInvalidRefreshToken,
		message:     "Invalid refresh token.",
	},
	{Domain: rpcerror.AuthenticatorDomain, Reason: rpcerror.AuthenticatorSessionExpired}: {
		connectCode: connect.CodeUnauthenticated,
		publicCode:  CodeSessionExpired,
		message:     "Session expired.",
	},
	{Domain: rpcerror.AuthenticatorDomain, Reason: rpcerror.AuthenticatorSessionRevoked}: {
		connectCode: connect.CodeUnauthenticated,
		publicCode:  CodeSessionRevoked,
		message:     "Session revoked.",
	},
	{Domain: rpcerror.UserDomain, Reason: rpcerror.UserEmailAlreadyExists}: {
		connectCode: connect.CodeAlreadyExists,
		publicCode:  CodeEmailAlreadyExists,
		message:     "Email already exists.",
	},
}

var codeMappings = map[codes.Code]mapping{
	codes.Canceled: {
		connectCode: connect.CodeCanceled,
		publicCode:  CodeCanceled,
		message:     "Request canceled.",
	},
	codes.InvalidArgument: {
		connectCode: connect.CodeInvalidArgument,
		publicCode:  CodeInvalidArgument,
		message:     "Invalid request.",
	},
	codes.DeadlineExceeded: {
		connectCode: connect.CodeDeadlineExceeded,
		publicCode:  CodeDeadlineExceeded,
		message:     "Request timed out.",
	},
	codes.NotFound: {
		connectCode: connect.CodeNotFound,
		publicCode:  CodeNotFound,
		message:     "Resource not found.",
	},
	codes.AlreadyExists: {
		connectCode: connect.CodeAlreadyExists,
		publicCode:  CodeAlreadyExists,
		message:     "Resource already exists.",
	},
	codes.PermissionDenied: {
		connectCode: connect.CodePermissionDenied,
		publicCode:  CodePermissionDenied,
		message:     "Permission denied.",
	},
	codes.ResourceExhausted: {
		connectCode: connect.CodeResourceExhausted,
		publicCode:  CodeResourceExhausted,
		message:     "Resource exhausted.",
	},
	codes.FailedPrecondition: {
		connectCode: connect.CodeFailedPrecondition,
		publicCode:  CodeInvalidArgument,
		message:     "Invalid request state.",
	},
	codes.Aborted: {
		connectCode: connect.CodeAborted,
		publicCode:  CodeInternal,
		message:     "Request aborted.",
	},
	codes.Unavailable: {
		connectCode: connect.CodeUnavailable,
		publicCode:  CodeUnavailable,
		message:     "Service unavailable.",
	},
	codes.Unauthenticated: {
		connectCode: connect.CodeUnauthenticated,
		publicCode:  CodeInvalidCredentials,
		message:     "Unauthenticated.",
	},
}

func FromRPC(err error) error {
	if err == nil {
		return nil
	}

	if info, ok := rpcerror.Parse(err); ok {
		if mapping, ok := reasonMappings[info.Key()]; ok {
			return newConnectError(mapping)
		}
		return newConnectError(internalMapping())
	}

	if mapping, ok := codeMappings[status.Code(err)]; ok {
		return newConnectError(mapping)
	}
	return newConnectError(internalMapping())
}

func newConnectError(mapping mapping) error {
	connectErr := connect.NewError(mapping.connectCode, errors.New(mapping.message))
	detail, err := connect.NewErrorDetail(&apiv1.PublicErrorInfo{
		Code:    proto.String(mapping.publicCode),
		Message: proto.String(mapping.message),
	})
	if err == nil {
		connectErr.AddDetail(detail)
	}
	return connectErr
}

func internalMapping() mapping {
	return mapping{
		connectCode: connect.CodeInternal,
		publicCode:  CodeInternal,
		message:     "Internal server error.",
	}
}
