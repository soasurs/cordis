package server

import (
	"database/sql"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	errInvalidEmail         = status.Error(codes.InvalidArgument, "invalid email format")
	errNameRequired         = status.Error(codes.InvalidArgument, "name is required")
	errNameTooLong          = status.Error(codes.InvalidArgument, "name is too long")
	errUserIDRequired       = status.Error(codes.InvalidArgument, "user id is required")
	errEmailRequired        = status.Error(codes.InvalidArgument, "email is required")
	errInvalidUsername      = status.Error(codes.InvalidArgument, "username must be 2-32 lowercase letters, digits, or underscores")
	errTargetIDRequired     = status.Error(codes.InvalidArgument, "target id is required")
	errSelfRelationship     = status.Error(codes.InvalidArgument, "cannot form a relationship with yourself")
	errInvalidCursor        = status.Error(codes.InvalidArgument, "cursor must not be negative")
	errBatchTooLarge        = status.Error(codes.InvalidArgument, "too many target ids")
	errProfileBatchTooLarge = status.Error(codes.InvalidArgument, "too many user ids")
	errInvalidLimit         = status.Error(codes.InvalidArgument, "limit is out of range")
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
