package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/soasurs/cordis/services/media/v1/config"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

const (
	createUserAvatarUploadOperation = "media.create.user_avatar"
	createGuildIconUploadOperation  = "media.create.guild_icon"
	createAttachmentUploadOperation = "media.create.message_attachment"
)

type createUploadFingerprint struct {
	Version      int    `json:"version"`
	ActorUserID  int64  `json:"actor_user_id"`
	Kind         string `json:"kind"`
	SubjectID    int64  `json:"subject_id"`
	ExpectedSize int64  `json:"expected_size"`
	ContentType  string `json:"content_type"`
	Filename     string `json:"filename"`
}

func createUploadRequestHash(
	actorUserID int64,
	kind store.Kind,
	subjectID int64,
	expectedSize int64,
	contentType, filename string,
) ([]byte, error) {
	value := createUploadFingerprint{
		Version:      1,
		ActorUserID:  actorUserID,
		Kind:         string(kind),
		SubjectID:    subjectID,
		ExpectedSize: expectedSize,
		ContentType:  contentType,
		Filename:     filename,
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal create upload fingerprint: %w", err)
	}
	digest := sha256.Sum256(data)
	return digest[:], nil
}

func uploadOperation(kind store.Kind) (string, error) {
	switch kind {
	case store.KindUserAvatar:
		return createUserAvatarUploadOperation, nil
	case store.KindGuildIcon:
		return createGuildIconUploadOperation, nil
	case store.KindMessageAttachment:
		return createAttachmentUploadOperation, nil
	default:
		return "", errPurposeRequired
	}
}

func validateIdempotencyKey(key string, maxLength int) error {
	switch {
	case key == "":
		return errIdempotencyKeyRequired
	case len(key) > maxLength:
		return errIdempotencyKeyTooLong
	case strings.TrimSpace(key) != key:
		return errIdempotencyKeyWhitespace
	default:
		return nil
	}
}

// idempotencyExpiry returns the retention deadline for an upload idempotency
// key. The retention never drops below the upload session TTL so a retry stays
// valid for the whole upload window.
func idempotencyExpiry(now int64, cfg config.Config) int64 {
	ttl := cfg.Idempotency.CreateUploadTTL()
	if uploadTTL := time.Duration(cfg.Media.UploadSessionTTL()) * time.Second; ttl < uploadTTL {
		ttl = uploadTTL
	}
	return now + ttl.Milliseconds()
}
