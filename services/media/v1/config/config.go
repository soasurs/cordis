package config

import (
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
	Backend      string `json:",default=s3"`
	Endpoint     string
	Region       string `json:",default=auto"`
	Bucket       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool `json:",default=true"`
	Secure       bool `json:",default=true"`
}

func (c ObjectStoreConfig) ToObjectStoreConfig() objectstore.Config {
	return objectstore.Config{
		Endpoint:     c.Endpoint,
		Region:       c.Region,
		Bucket:       c.Bucket,
		AccessKey:    c.AccessKey,
		SecretKey:    c.SecretKey,
		UsePathStyle: c.UsePathStyle,
		Secure:       c.Secure,
	}
}

type MediaConfig struct {
	UploadSessionTTLSeconds       int     `json:",default=900"`
	PresignedURLTTLSeconds        int     `json:",default=900"`
	MaxUploadSizeBytes            int64   `json:",default=524288000"`
	MaxActiveUploadsPerUser       int64   `json:",default=5"`
	StagingCleanupIntervalSeconds int     `json:",default=300"`
	ImageVariantSizes             []int32 `json:",default=[64,128,256,512]"`
	ImageProcessingTimeoutMs      int     `json:",default=30000"`
	MaxConcurrentImageProcessing  int64   `json:",default=4"`
	MaxImageSizeBytes             int64   `json:",default=10485760"`
	MaxImageDimension             int32   `json:",default=4096"`
	MaxImagePixels                int64   `json:",default=16777216"`
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
