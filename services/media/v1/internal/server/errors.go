package server

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/soasurs/cordis/pkg/rpcerror"
)

var (
	errActorUserIDRequired      = status.Error(codes.InvalidArgument, "actor user id is required")
	errPurposeRequired          = status.Error(codes.InvalidArgument, "upload purpose is required")
	errGuildIDRequired          = status.Error(codes.InvalidArgument, "guild id is required")
	errChannelIDRequired        = status.Error(codes.InvalidArgument, "channel id is required")
	errSizeRequired             = status.Error(codes.InvalidArgument, "upload size must be positive")
	errSizeExceeded             = status.Error(codes.InvalidArgument, "upload size exceeds limit")
	errContentTypeRequired      = status.Error(codes.InvalidArgument, "content type is required")
	errContentTypeInvalid       = status.Error(codes.InvalidArgument, "content type is invalid")
	errFilenameInvalid          = status.Error(codes.InvalidArgument, "attachment filename is invalid")
	errSizeMismatch             = status.Error(codes.FailedPrecondition, "uploaded object size does not match")
	errContentTypeMismatch      = status.Error(codes.FailedPrecondition, "uploaded object content type does not match")
	errUploadLimit              = status.Error(codes.ResourceExhausted, "active upload limit reached")
	errUploadNotFound           = status.Error(codes.NotFound, "upload not found")
	errWrongOwner               = status.Error(codes.PermissionDenied, "wrong upload owner")
	errNotUploaded              = status.Error(codes.FailedPrecondition, "asset not uploaded")
	errAlreadyCompleted         = status.Error(codes.AlreadyExists, "upload already completed")
	errAlreadyAborted           = status.Error(codes.FailedPrecondition, "upload already aborted")
	errProcessingFailed         = status.Error(codes.Internal, "image processing failed")
	errAssetNotFound            = status.Error(codes.NotFound, "asset not found")
	errAssetNotReady            = status.Error(codes.FailedPrecondition, "asset not ready")
	errObjectStoreDown          = status.Error(codes.Unavailable, "object storage unavailable")
	errProcessingInterrupted    = status.Error(codes.Unavailable, "image processing interrupted")
	errAssetNotDownloadable     = status.Error(codes.InvalidArgument, "asset is not a downloadable attachment")
	errTooManyAssets            = status.Error(codes.InvalidArgument, "too many asset ids")
	errIdempotencyKeyRequired   = status.Error(codes.InvalidArgument, "idempotency key must not be empty")
	errIdempotencyKeyTooLong    = status.Error(codes.InvalidArgument, "idempotency key is too long")
	errIdempotencyKeyWhitespace = status.Error(codes.InvalidArgument, "idempotency key must not have leading or trailing whitespace")
)

func idempotencyKeyReused() error {
	return rpcerror.New(
		codes.InvalidArgument,
		rpcerror.MediaDomain,
		rpcerror.MediaIdempotencyKeyReused,
		"idempotency key was already used with different request parameters",
	)
}
