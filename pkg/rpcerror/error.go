package rpcerror

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Info struct {
	Domain string
	Reason string
}

type Key struct {
	Domain string
	Reason string
}

func (i Info) Key() Key {
	return Key{
		Domain: i.Domain,
		Reason: i.Reason,
	}
}

func New(code codes.Code, domain, reason, message string) error {
	st := status.New(code, message)
	st, err := st.WithDetails(&errdetails.ErrorInfo{
		Domain: domain,
		Reason: reason,
	})
	if err != nil {
		return status.Error(code, message)
	}
	return st.Err()
}

func Parse(err error) (Info, bool) {
	if err == nil {
		return Info{}, false
	}

	st := status.Convert(err)
	for _, detail := range st.Details() {
		errorInfo, ok := detail.(*errdetails.ErrorInfo)
		if !ok {
			continue
		}
		if errorInfo.GetDomain() == "" || errorInfo.GetReason() == "" {
			continue
		}
		return Info{
			Domain: errorInfo.GetDomain(),
			Reason: errorInfo.GetReason(),
		}, true
	}
	return Info{}, false
}

func Is(err error, domain, reason string) bool {
	info, ok := Parse(err)
	return ok && info.Domain == domain && info.Reason == reason
}

func Code(err error) codes.Code {
	if err == nil {
		return codes.OK
	}

	var st interface {
		GRPCStatus() *status.Status
	}
	if errors.As(err, &st) && st.GRPCStatus() != nil {
		return st.GRPCStatus().Code()
	}
	return codes.Unknown
}
