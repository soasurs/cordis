package server

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/soasurs/cordis/services/media/v1/internal/objectstore"
)

type fakeObject struct {
	data        []byte
	contentType string
}

type fakeObjectStore struct {
	mu                  sync.Mutex
	objects             map[string]fakeObject
	lastPresignedKey    string
	lastPresignedType   string
	lastPresignedLength int64
	lastDownloadKey     string
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: make(map[string]fakeObject)}
}

func (f *fakeObjectStore) CreatePresignedPutRequest(
	_ context.Context,
	key string,
	contentType string,
	contentLength int64,
	_ int64,
) (*objectstore.PresignedPutRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastPresignedKey = key
	f.lastPresignedType = contentType
	f.lastPresignedLength = contentLength
	return &objectstore.PresignedPutRequest{
		URL: "https://s3.example.com/upload/" + key,
		RequestHeaders: map[string]string{
			"Content-Length": strconv.FormatInt(contentLength, 10),
			"Content-Type":   contentType,
		},
	}, nil
}

func (f *fakeObjectStore) StatObject(_ context.Context, key string) (*objectstore.ObjectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	object, ok := f.objects[key]
	if !ok {
		return nil, objectstore.ErrObjectNotFound
	}
	return &objectstore.ObjectInfo{
		Size:        int64(len(object.data)),
		ContentType: object.contentType,
	}, nil
}

func (f *fakeObjectStore) GetObject(
	_ context.Context,
	key string,
) (io.ReadCloser, *objectstore.ObjectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	object, ok := f.objects[key]
	if !ok {
		return nil, nil, objectstore.ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(object.data)), &objectstore.ObjectInfo{
		Size:        int64(len(object.data)),
		ContentType: object.contentType,
	}, nil
}

func (f *fakeObjectStore) PutObject(
	_ context.Context,
	key string,
	contentType string,
	data io.Reader,
) error {
	value, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = fakeObject{data: value, contentType: contentType}
	return nil
}

func (f *fakeObjectStore) DeleteObject(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	return nil
}

func (f *fakeObjectStore) CreatePresignedGetURL(
	_ context.Context,
	key string,
	_ int64,
) (string, error) {
	f.mu.Lock()
	f.lastDownloadKey = key
	f.mu.Unlock()
	return "https://s3.example.com/" + key, nil
}

func (f *fakeObjectStore) ListObjects(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (f *fakeObjectStore) setObject(key, contentType string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = fakeObject{data: data, contentType: contentType}
}

func (f *fakeObjectStore) hasObject(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[key]
	return ok
}
