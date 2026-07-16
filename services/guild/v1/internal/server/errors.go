package server

import (
	"database/sql"
	"errors"

	"github.com/lib/pq"
	"google.golang.org/grpc/codes"

	"github.com/soasurs/cordis/pkg/rpcerror"
)

func invalidRequest(message string) error {
	return rpcerror.New(codes.InvalidArgument, rpcerror.GuildDomain, rpcerror.GuildInvalidRequest, message)
}

func notFound() error {
	return rpcerror.New(codes.NotFound, rpcerror.GuildDomain, rpcerror.GuildNotFound, "guild not found")
}

func permissionDenied() error {
	return rpcerror.New(codes.PermissionDenied, rpcerror.GuildDomain, rpcerror.GuildPermissionDenied, "permission denied")
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return notFound()
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23514" {
		return invalidRequest("invalid guild state")
	}
	return err
}
