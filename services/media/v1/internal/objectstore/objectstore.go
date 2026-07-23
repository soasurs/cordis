// Package objectstore defines the provider-neutral interface for
// S3-compatible object storage backends.
package objectstore

import (
	"context"
	"errors"
	"io"
)

var ErrObjectNotFound = errors.New("object not found")

// ObjectStore is a conservative S3-compatible storage abstraction. Only the
// operations needed by the Media service are exposed.
type ObjectStore interface {
	// CreatePresignedPutURL returns a short-lived URL for a single PUT whose
	// Content-Type and Content-Length are part of the signature.
	CreatePresignedPutURL(
		ctx context.Context,
		key string,
		contentType string,
		contentLength int64,
		expiresInSeconds int64,
	) (string, error)

	// StatObject returns the object's current size and content type.
	StatObject(ctx context.Context, key string) (*ObjectInfo, error)

	// GetObject returns the object at the given key.
	GetObject(ctx context.Context, key string) (io.ReadCloser, *ObjectInfo, error)

	// PutObject writes data to the given key.
	PutObject(ctx context.Context, key, contentType string, data io.Reader) error

	// DeleteObject deletes the object at the given key. Deleting a
	// non-existent key does not error.
	DeleteObject(ctx context.Context, key string) error

	// CreatePresignedGetURL returns a signed download URL for the given key.
	CreatePresignedGetURL(ctx context.Context, key string, expiresInSeconds int64) (string, error)

	// ListObjects returns keys with the given prefix.
	ListObjects(ctx context.Context, prefix string) ([]string, error)
}

// ObjectInfo carries the minimal metadata for an object.
type ObjectInfo struct {
	Size        int64
	ContentType string
}

// Config holds the configuration for connecting to an S3-compatible backend.
type Config struct {
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	Secure       bool
}
