package server

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"mime"
	"strings"
	"unicode"
	"unicode/utf8"

	"google.golang.org/grpc/codes"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/media/v1/internal/processing"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

func uploadPurpose(
	req *mediav1.CreateUploadRequest,
	actorUserID int64,
) (store.Kind, int64, string, error) {
	switch {
	case req.HasUserAvatar():
		return store.KindUserAvatar, actorUserID, "", nil
	case req.HasGuildIcon():
		guildID := req.GetGuildIcon().GetGuildId()
		if guildID <= 0 {
			return "", 0, "", errGuildIDRequired
		}
		return store.KindGuildIcon, guildID, "", nil
	case req.HasMessageAttachment():
		purpose := req.GetMessageAttachment()
		channelID := purpose.GetChannelId()
		if channelID <= 0 {
			return "", 0, "", errChannelIDRequired
		}
		return store.KindMessageAttachment, channelID, purpose.GetFilename(), nil
	default:
		return "", 0, "", errPurposeRequired
	}
}

func imageTooLargeError(kind store.Kind) error {
	if kind == store.KindUserAvatar {
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaAvatarFileTooLarge,
			"avatar file is too large",
		)
	}
	return errSizeExceeded
}

func imageContentTypeInvalidError(kind store.Kind) error {
	if kind == store.KindUserAvatar {
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaAvatarContentTypeInvalid,
			"avatar content type is invalid",
		)
	}
	return errContentTypeInvalid
}

func mapAvatarProcessingError(kind store.Kind, err error) error {
	if kind != store.KindUserAvatar {
		return nil
	}
	switch {
	case errors.Is(err, processing.ErrImageTooLarge):
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaAvatarFileTooLarge,
			"avatar file is too large",
		)
	case errors.Is(err, processing.ErrImageContentTypeInvalid):
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaAvatarContentTypeInvalid,
			"avatar content type is invalid",
		)
	case errors.Is(err, processing.ErrImageDimensionsExceeded):
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaAvatarDimensionsExceeded,
			"avatar dimensions exceed limit",
		)
	case errors.Is(err, processing.ErrImagePixelsExceeded):
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaAvatarPixelsExceeded,
			"avatar pixel count exceeds limit",
		)
	default:
		return nil
	}
}

func uploadObjectKey(asset *store.Asset) string {
	if asset.StagingKey != "" {
		return asset.StagingKey
	}
	return asset.PublishedKey
}

func normalizeContentType(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errContentTypeRequired
	}
	trimmed := strings.TrimSpace(value)
	mediaType, params, err := mime.ParseMediaType(trimmed)
	mediaType = strings.ToLower(mediaType)
	if err != nil || mediaType == "" || len(params) != 0 || trimmed != mediaType {
		return "", errContentTypeInvalid
	}
	return mediaType, nil
}

func validateAttachmentFilename(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) ||
		len(value) > 255 || value == "." || value == ".." ||
		strings.ContainsAny(value, `/\`) {
		return "", errFilenameInvalid
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", errFilenameInvalid
		}
	}
	return value, nil
}

func newStorageToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}
