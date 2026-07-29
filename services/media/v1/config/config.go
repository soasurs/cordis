package config

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/services/media/v1/internal/objectstore"
)

type Config struct {
	zrpc.RpcServerConf
	Database    database.Config   `json:",optional"`
	ObjectStore ObjectStoreConfig `json:",optional"`
	Media       MediaConfig       `json:",optional"`
}

type ObjectStoreConfig struct {
	Backend                 string `json:",default=s3"`
	Endpoint                string
	Region                  string `json:",default=auto"`
	PublicBucket            string
	StagingBucket           string
	AttachmentBucket        string
	AttachmentPublicBaseURL string
	AccessKey               string
	SecretKey               string
	UsePathStyle            bool `json:",default=true"`
	Secure                  bool `json:",default=true"`
}

func (c ObjectStoreConfig) ToObjectStoreConfig(bucket string) objectstore.Config {
	return objectstore.Config{
		Endpoint:     c.Endpoint,
		Region:       c.Region,
		Bucket:       bucket,
		AccessKey:    c.AccessKey,
		SecretKey:    c.SecretKey,
		UsePathStyle: c.UsePathStyle,
		Secure:       c.Secure,
	}
}

type MediaConfig struct {
	UploadSessionTTLSeconds       int                              `json:",default=900"`
	PresignedURLTTLSeconds        int                              `json:",default=900"`
	MaxUploadSizeBytes            int64                            `json:",default=524288000"`
	MaxActiveUploadsPerUser       int64                            `json:",default=5"`
	StagingCleanupIntervalSeconds int                              `json:",default=300"`
	ImageProcessingTimeoutMs      int                              `json:",default=30000"`
	MaxConcurrentImageProcessing  int64                            `json:",default=4"`
	ImageConstraints              ImageConstraintsConfig           `json:",optional"`
	AttachmentImageInspection     AttachmentImageInspectionProfile `json:",optional"`
	AttachmentAccessMode          string                           `json:",default=public"`
	AttachmentDownloadTTLSeconds  int                              `json:",default=86400"`
}

// ImageConstraintsConfig holds per-purpose image upload limits enforced by Media.
type ImageConstraintsConfig struct {
	UserAvatar ImageConstraintProfile `json:",optional"`
	GuildIcon  ImageConstraintProfile `json:",optional"`
}

// ImageConstraintProfile is the size, dimension, and MIME policy for one upload purpose.
type ImageConstraintProfile struct {
	MaxSizeBytes        int64    `json:",default=10485760"`
	MaxDimension        int32    `json:",default=4096"`
	MaxPixels           int64    `json:",default=16777216"`
	AllowedContentTypes []string `json:",optional"`
}

// AttachmentImageInspectionProfile bounds best-effort attachment metadata
// extraction. It does not limit whether an attachment upload may complete.
type AttachmentImageInspectionProfile ImageConstraintProfile

// ImagePurpose selects which image constraint profile to apply.
type ImagePurpose string

const (
	ImagePurposeUserAvatar ImagePurpose = "user_avatar"
	ImagePurposeGuildIcon  ImagePurpose = "guild_icon"
)

const (
	AttachmentAccessPublic    = "public"
	AttachmentAccessPresigned = "presigned"
)

var defaultAllowedImageContentTypes = []string{
	"image/jpeg",
	"image/png",
	"image/webp",
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ObjectStore.PublicBucket) == "" {
		return errors.New("public object store bucket is required")
	}
	if strings.TrimSpace(c.ObjectStore.StagingBucket) == "" {
		return errors.New("staging object store bucket is required")
	}
	if strings.TrimSpace(c.ObjectStore.AttachmentBucket) == "" {
		return errors.New("attachment object store bucket is required")
	}
	switch c.Media.AttachmentAccess() {
	case AttachmentAccessPublic:
		baseURL, err := url.Parse(c.ObjectStore.AttachmentPublicBaseURL)
		if err != nil {
			return fmt.Errorf("parse attachment public base url: %w", err)
		}
		if baseURL.Scheme != "https" || baseURL.Host == "" ||
			baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
			return errors.New("attachment public base url must be an absolute https url without credentials, query, or fragment")
		}
	case AttachmentAccessPresigned:
	default:
		return fmt.Errorf("unsupported attachment access mode %q", c.Media.AttachmentAccessMode)
	}
	return c.Media.Validate()
}

func (c MediaConfig) UploadSessionTTL() int64 {
	if c.UploadSessionTTLSeconds <= 0 {
		return 900
	}
	return int64(c.UploadSessionTTLSeconds)
}

func (c MediaConfig) PresignedURLTTL() int64 {
	if c.PresignedURLTTLSeconds <= 0 {
		return 900
	}
	return int64(c.PresignedURLTTLSeconds)
}

func (c MediaConfig) AttachmentAccess() string {
	if value := strings.ToLower(strings.TrimSpace(c.AttachmentAccessMode)); value != "" {
		return value
	}
	return AttachmentAccessPublic
}

func (c MediaConfig) AttachmentDownloadTTL() int64 {
	if c.AttachmentDownloadTTLSeconds <= 0 {
		return 86400
	}
	return int64(c.AttachmentDownloadTTLSeconds)
}

func (c MediaConfig) MaxUploadSize() int64 {
	if c.MaxUploadSizeBytes <= 0 {
		return 524288000
	}
	return c.MaxUploadSizeBytes
}

func (c MediaConfig) MaxActiveUploads() int64 {
	if c.MaxActiveUploadsPerUser <= 0 {
		return 5
	}
	return c.MaxActiveUploadsPerUser
}

func (c MediaConfig) StagingCleanupInterval() int64 {
	if c.StagingCleanupIntervalSeconds <= 0 {
		return 300
	}
	return int64(c.StagingCleanupIntervalSeconds)
}

func (c MediaConfig) ImageProcessingTimeout() time.Duration {
	if c.ImageProcessingTimeoutMs <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.ImageProcessingTimeoutMs) * time.Millisecond
}

func (c MediaConfig) ImageProcessingLimit() int64 {
	if c.MaxConcurrentImageProcessing <= 0 {
		return 4
	}
	return c.MaxConcurrentImageProcessing
}

// Validate checks that advertised image policies can be enforced.
func (c MediaConfig) Validate() error {
	profiles := []struct {
		name    string
		profile ImageConstraintProfile
	}{
		{name: "user avatar", profile: c.ImageConstraints.UserAvatar},
		{name: "guild icon", profile: c.ImageConstraints.GuildIcon},
	}
	for _, value := range profiles {
		if err := validateAllowedImageContentTypes(value.name, value.profile.AllowedContentTypes); err != nil {
			return err
		}
		if value.profile.normalized().MaxSizeBytes > c.MaxUploadSize() {
			return fmt.Errorf("%s max size must not exceed media max upload size", value.name)
		}
	}
	if err := validateAllowedImageContentTypes(
		"attachment image inspection",
		ImageConstraintProfile(c.AttachmentImageInspection).AllowedContentTypes,
	); err != nil {
		return err
	}
	return nil
}

// ImageConstraintsFor returns the normalized upload profile for purpose.
func (c MediaConfig) ImageConstraintsFor(purpose ImagePurpose) ImageConstraintProfile {
	switch purpose {
	case ImagePurposeUserAvatar:
		return c.ImageConstraints.UserAvatar.normalized()
	case ImagePurposeGuildIcon:
		return c.ImageConstraints.GuildIcon.normalized()
	default:
		return ImageConstraintProfile{}.normalized()
	}
}

// AttachmentImageInspectionConstraints returns the normalized best-effort
// attachment inspection budget.
func (c MediaConfig) AttachmentImageInspectionConstraints() ImageConstraintProfile {
	return ImageConstraintProfile(c.AttachmentImageInspection).normalized()
}

func (p ImageConstraintProfile) normalized() ImageConstraintProfile {
	if p.MaxSizeBytes <= 0 {
		p.MaxSizeBytes = 10 << 20
	}
	if p.MaxDimension <= 0 {
		p.MaxDimension = 4096
	}
	if p.MaxPixels <= 0 {
		p.MaxPixels = 4096 * 4096
	}
	if len(p.AllowedContentTypes) == 0 {
		p.AllowedContentTypes = append([]string(nil), defaultAllowedImageContentTypes...)
		return p
	}
	types := make([]string, 0, len(p.AllowedContentTypes))
	seen := make(map[string]struct{}, len(p.AllowedContentTypes))
	for _, contentType := range p.AllowedContentTypes {
		normalized := strings.ToLower(strings.TrimSpace(contentType))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		types = append(types, normalized)
	}
	if len(types) == 0 {
		types = append([]string(nil), defaultAllowedImageContentTypes...)
	}
	p.AllowedContentTypes = types
	return p
}

// AllowsContentType reports whether contentType is in the profile allowlist.
func (p ImageConstraintProfile) AllowsContentType(contentType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(contentType))
	return slices.Contains(p.AllowedContentTypes, normalized)
}

func validateAllowedImageContentTypes(name string, contentTypes []string) error {
	for _, contentType := range contentTypes {
		normalized := strings.ToLower(strings.TrimSpace(contentType))
		if normalized == "" || !slices.Contains(defaultAllowedImageContentTypes, normalized) {
			return fmt.Errorf("%s has unsupported content type %q", name, contentType)
		}
	}
	return nil
}
