package apierror

import (
	"errors"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
)

const (
	CodeInvalidCredentials         = "auth.invalid_credentials"
	CodeEmailAlreadyExists         = "auth.email_already_exists"
	CodeInvalidAccessToken         = "auth.invalid_access_token"
	CodeInvalidRefreshToken        = "auth.invalid_refresh_token"
	CodeSessionExpired             = "auth.session_expired"
	CodeSessionRevoked             = "auth.session_revoked"
	CodeInvalidTwoFactorCode       = "auth.invalid_two_factor_code"
	CodeTwoFactorChallengeExpired  = "auth.two_factor_challenge_expired"
	CodeTwoFactorNotEnabled        = "auth.two_factor_not_enabled"
	CodeTwoFactorAlreadyEnabled    = "auth.two_factor_already_enabled"
	CodeTwoFactorEnrollmentPending = "auth.two_factor_enrollment_pending"
	CodeInvalidArgument            = "request.invalid_argument"
	CodeCanceled                   = "request.canceled"
	CodeDeadlineExceeded           = "request.deadline_exceeded"
	CodeNotFound                   = "resource.not_found"
	CodeAlreadyExists              = "resource.already_exists"
	CodePermissionDenied           = "auth.permission_denied"
	CodeResourceExhausted          = "system.resource_exhausted"
	CodeUnavailable                = "system.unavailable"
	CodeInternal                   = "system.internal"
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
	{Domain: rpcerror.AuthenticatorDomain, Reason: rpcerror.AuthenticatorInvalidTwoFactorCode}: {
		connectCode: connect.CodeUnauthenticated,
		publicCode:  CodeInvalidTwoFactorCode,
		message:     "Invalid authenticator code.",
	},
	{Domain: rpcerror.AuthenticatorDomain, Reason: rpcerror.AuthenticatorTwoFactorChallengeExpired}: {
		connectCode: connect.CodeUnauthenticated,
		publicCode:  CodeTwoFactorChallengeExpired,
		message:     "Two-factor challenge expired.",
	},
	{Domain: rpcerror.AuthenticatorDomain, Reason: rpcerror.AuthenticatorTwoFactorNotEnabled}: {
		connectCode: connect.CodeFailedPrecondition,
		publicCode:  CodeTwoFactorNotEnabled,
		message:     "Two-factor authentication is not enabled.",
	},
	{Domain: rpcerror.AuthenticatorDomain, Reason: rpcerror.AuthenticatorTwoFactorAlreadyEnabled}: {
		connectCode: connect.CodeFailedPrecondition,
		publicCode:  CodeTwoFactorAlreadyEnabled,
		message:     "Two-factor authentication is already enabled.",
	},
	{Domain: rpcerror.AuthenticatorDomain, Reason: rpcerror.AuthenticatorTwoFactorEnrollmentPending}: {
		connectCode: connect.CodeFailedPrecondition,
		publicCode:  CodeTwoFactorEnrollmentPending,
		message:     "Two-factor enrollment is already pending.",
	},
	{Domain: rpcerror.UserDomain, Reason: rpcerror.UserEmailAlreadyExists}: {
		connectCode: connect.CodeAlreadyExists,
		publicCode:  CodeEmailAlreadyExists,
		message:     "Email already exists.",
	},
	{Domain: rpcerror.MessageDomain, Reason: rpcerror.MessageNotFound}: {
		connectCode: connect.CodeNotFound,
		publicCode:  CodeNotFound,
		message:     "Resource not found.",
	},
	{Domain: rpcerror.MessageDomain, Reason: rpcerror.MessagePermissionDenied}: {
		connectCode: connect.CodePermissionDenied,
		publicCode:  CodePermissionDenied,
		message:     "Permission denied.",
	},
	{Domain: rpcerror.MessageDomain, Reason: rpcerror.MessageInvalidRequest}: {
		connectCode: connect.CodeInvalidArgument,
		publicCode:  CodeInvalidArgument,
		message:     "Invalid request.",
	},
	{Domain: rpcerror.GuildDomain, Reason: rpcerror.GuildNotFound}: {
		connectCode: connect.CodeNotFound,
		publicCode:  CodeNotFound,
		message:     "Resource not found.",
	},
	{Domain: rpcerror.GuildDomain, Reason: rpcerror.GuildPermissionDenied}: {
		connectCode: connect.CodePermissionDenied,
		publicCode:  CodePermissionDenied,
		message:     "Permission denied.",
	},
	{Domain: rpcerror.GuildDomain, Reason: rpcerror.GuildInvalidRequest}: {
		connectCode: connect.CodeInvalidArgument,
		publicCode:  CodeInvalidArgument,
		message:     "Invalid request.",
	},
	{Domain: rpcerror.GuildDomain, Reason: rpcerror.GuildMemberAlreadyExists}: {
		connectCode: connect.CodeAlreadyExists,
		publicCode:  CodeAlreadyExists,
		message:     "Resource already exists.",
	},
	{Domain: rpcerror.GuildDomain, Reason: rpcerror.GuildInviteNotFound}: {
		connectCode: connect.CodeNotFound,
		publicCode:  CodeNotFound,
		message:     "Resource not found.",
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

func PublicCode(err error) string {
	info, ok := PublicInfo(err)
	if !ok {
		return ""
	}
	return info.GetCode()
}

func PublicInfo(err error) (*apiv1.PublicErrorInfo, bool) {
	if err == nil {
		return nil, false
	}

	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return nil, false
	}
	for _, detail := range connectErr.Details() {
		value, err := detail.Value()
		if err != nil {
			continue
		}
		publicInfo, ok := value.(*apiv1.PublicErrorInfo)
		if ok {
			return publicInfo, true
		}
	}
	return nil, false
}

func newConnectError(mapping mapping) error {
	connectErr := connect.NewError(mapping.connectCode, errors.New(mapping.message))
	detail, err := connect.NewErrorDetail(&apiv1.PublicErrorInfo{
		Code:    new(mapping.publicCode),
		Message: new(mapping.message),
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
