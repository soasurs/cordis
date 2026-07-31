package server

import (
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
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

func resourceLimitExceeded(message string) error {
	return rpcerror.New(codes.ResourceExhausted, rpcerror.MessageDomain, rpcerror.MessageResourceLimitExceeded, message)
}

func idempotencyKeyReused() error {
	return rpcerror.New(
		codes.InvalidArgument,
		rpcerror.MessageDomain,
		rpcerror.MessageIdempotencyKeyReused,
		"idempotency key was already used with different request parameters",
	)
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
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
