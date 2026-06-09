package server

import (
	"database/sql"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	errInvalidEmail   = status.Error(codes.InvalidArgument, "invalid email format")
	errNameRequired   = status.Error(codes.InvalidArgument, "name is required")
	errNameTooLong    = status.Error(codes.InvalidArgument, "name is too long")
	errUserIDRequired = status.Error(codes.InvalidArgument, "user id is required")
	errEmailRequired  = status.Error(codes.InvalidArgument, "email is required")
)

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return status.Error(codes.NotFound, "resource not found")
	}
	return err
}
