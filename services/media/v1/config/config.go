package config

import (
	"errors"
	"fmt"
	"net/url"
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
	UploadSessionTTLSeconds       int    `json:",default=900"`
	PresignedURLTTLSeconds        int    `json:",default=900"`
	MaxUploadSizeBytes            int64  `json:",default=524288000"`
	MaxActiveUploadsPerUser       int64  `json:",default=5"`
	StagingCleanupIntervalSeconds int    `json:",default=300"`
	ImageProcessingTimeoutMs      int    `json:",default=30000"`
	MaxConcurrentImageProcessing  int64  `json:",default=4"`
	MaxImageSizeBytes             int64  `json:",default=10485760"`
	MaxImageDimension             int32  `json:",default=4096"`
	MaxImagePixels                int64  `json:",default=16777216"`
	AttachmentAccessMode          string `json:",default=public"`
	AttachmentDownloadTTLSeconds  int    `json:",default=86400"`
}

const (
	AttachmentAccessPublic    = "public"
	AttachmentAccessPresigned = "presigned"
)

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
	return nil
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

func (c MediaConfig) MaxImageSize() int64 {
	if c.MaxImageSizeBytes <= 0 {
		return 10 << 20
	}
	return c.MaxImageSizeBytes
}

func (c MediaConfig) MaxImageDim() int32 {
	if c.MaxImageDimension <= 0 {
		return 4096
	}
	return c.MaxImageDimension
}

func (c MediaConfig) MaxPixels() int64 {
	if c.MaxImagePixels <= 0 {
		return 4096 * 4096
	}
	return c.MaxImagePixels
}
