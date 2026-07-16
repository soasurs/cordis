package server

import (
	"database/sql"
	"errors"

	"github.com/lib/pq"
	"google.golang.org/grpc/codes"

	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

func invalidRequest(message string) error {
	return rpcerror.New(codes.InvalidArgument, rpcerror.MessageDomain, rpcerror.MessageInvalidRequest, message)
}

func notFound() error {
	return rpcerror.New(codes.NotFound, rpcerror.MessageDomain, rpcerror.MessageNotFound, "message not found")
}

func permissionDenied() error {
	return rpcerror.New(codes.PermissionDenied, rpcerror.MessageDomain, rpcerror.MessagePermissionDenied, "permission denied")
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return notFound()
	}
	if errors.Is(err, store.ErrPermissionDenied) {
		return permissionDenied()
	}
	if isCheckViolation(err) {
		return invalidRequest("invalid message state")
	}
	return err
}

func isCheckViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23514"
}
