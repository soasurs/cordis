package server

import (
	"database/sql"
	"errors"

	"github.com/lib/pq"
	"google.golang.org/grpc/codes"

	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
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

func memberAlreadyExists() error {
	return rpcerror.New(codes.AlreadyExists, rpcerror.GuildDomain, rpcerror.GuildMemberAlreadyExists, "guild member already exists")
}

func inviteNotFound() error {
	return rpcerror.New(codes.NotFound, rpcerror.GuildDomain, rpcerror.GuildInviteNotFound, "guild invite not found")
}

func userBanned() error {
	return rpcerror.New(codes.PermissionDenied, rpcerror.GuildDomain, rpcerror.GuildPermissionDenied, "user is banned from the guild")
}

func resourceLimitExceeded() error {
	return rpcerror.New(codes.ResourceExhausted, rpcerror.GuildDomain, rpcerror.GuildResourceLimitExceeded, "resource limit exceeded")
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return notFound()
	}
	if errors.Is(err, store.ErrMemberAlreadyExists) {
		return memberAlreadyExists()
	}
	if errors.Is(err, store.ErrUserBanned) {
		return userBanned()
	}
	if errors.Is(err, store.ErrResourceLimitExceeded) {
		return resourceLimitExceeded()
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23514" {
		return invalidRequest("invalid guild state")
	}
	return err
}
