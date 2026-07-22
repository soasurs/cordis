package server

import (
	"database/sql"
	"errors"

	"github.com/lib/pq"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

// isUsernameViolation distinguishes the handle's unique index from the email
// index inside the same transaction.
func isUsernameViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == "user_profiles_username_active_idx"
}

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
