package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Store implements ObjectStore against any S3-compatible backend.
type S3Store struct {
	client   *minio.Client
	bucket   string
	endpoint string
}

// NewS3 creates a new S3Store with the given configuration.
func NewS3(cfg Config) (*S3Store, error) {
	s3Client, err := minio.New(cfg.Endpoint, &minio.Options{
		Region:       cfg.Region,
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       cfg.Secure,
		BucketLookup: toBucketLookup(cfg.UsePathStyle),
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}
	return &S3Store{
		client:   s3Client,
		bucket:   cfg.Bucket,
		endpoint: cfg.Endpoint,
	}, nil
}

func (s *S3Store) CreatePresignedPutURL(
	ctx context.Context,
	key string,
	contentType string,
	contentLength int64,
	expiresInSeconds int64,
) (string, error) {
	headers := make(http.Header)
	headers.Set("Content-Type", contentType)
	headers.Set("Content-Length", strconv.FormatInt(contentLength, 10))
	u, err := s.client.PresignHeader(
		ctx,
		http.MethodPut,
		s.bucket,
		key,
		time.Duration(expiresInSeconds)*time.Second,
		nil,
		headers,
	)
	if err != nil {
		return "", fmt.Errorf("create presigned put url: %w", err)
	}
	return u.String(), nil
}

func (s *S3Store) StatObject(ctx context.Context, key string) (*ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrObjectNotFound
		}
		return nil, fmt.Errorf("stat object: %w", err)
	}
	return &ObjectInfo{Size: info.Size, ContentType: info.ContentType}, nil
}

func (s *S3Store) GetObject(ctx context.Context, key string) (io.ReadCloser, *ObjectInfo, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("get object: %w", err)
	}
	stat, err := obj.Stat()
	if err != nil {
		obj.Close()
		if isNotFound(err) {
			return nil, nil, ErrObjectNotFound
		}
		return nil, nil, fmt.Errorf("stat object: %w", err)
	}
	return obj, &ObjectInfo{
		Size:        stat.Size,
		ContentType: stat.ContentType,
	}, nil
}

func isNotFound(err error) bool {
	if errors.Is(err, ErrObjectNotFound) {
		return true
	}
	switch minio.ToErrorResponse(err).Code {
	case "NoSuchKey", "NoSuchObject", "NotFound":
		return true
	default:
		return false
	}
}

func (s *S3Store) PutObject(ctx context.Context, key, contentType string, data io.Reader) error {
	opts := minio.PutObjectOptions{ContentType: contentType}
	_, err := s.client.PutObject(ctx, s.bucket, key, data, -1, opts)
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

func (s *S3Store) DeleteObject(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

func (s *S3Store) CreatePresignedGetURL(ctx context.Context, key string, expiresInSeconds int64) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, time.Duration(expiresInSeconds)*time.Second, nil)
	if err != nil {
		return "", fmt.Errorf("create presigned get url: %w", err)
	}
	return u.String(), nil
}

func (s *S3Store) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	objCh := s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	var keys []string
	for obj := range objCh {
		if obj.Err != nil {
			return nil, fmt.Errorf("list objects: %w", obj.Err)
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}

func toBucketLookup(usePathStyle bool) minio.BucketLookupType {
	if usePathStyle {
		return minio.BucketLookupPath
	}
	return minio.BucketLookupDNS
}
