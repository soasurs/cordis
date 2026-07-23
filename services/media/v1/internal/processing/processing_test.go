package processing

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/media/v1/config"
	"github.com/soasurs/cordis/services/media/v1/internal/objectstore"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

func TestJPEGOrientationAndTransform(t *testing.T) {
	require.Equal(t, 6, jpegOrientation(testJPEGEXIF(6)))

	source := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	source.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	source.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	source.SetNRGBA(0, 2, color.NRGBA{B: 255, A: 255})

	rotated := applyOrientation(source, 6)
	require.Equal(t, image.Rect(0, 0, 3, 2), rotated.Bounds())
	require.Equal(t, source.At(0, 2), rotated.At(0, 0))
	require.Equal(t, source.At(0, 0), rotated.At(2, 0))
	require.Equal(t, source.At(1, 0), rotated.At(2, 1))
}

func TestProcessorRejectsPixelLimitBeforePublication(t *testing.T) {
	source := testPNG(t, 4, 3)
	objectStore := &processorObjectStore{
		objects: map[string]processorObject{
			"staging/1": {data: source, contentType: "image/png"},
		},
	}
	processor := NewProcessor(objectStore, objectStore, config.MediaConfig{
		ImageProcessingTimeoutMs:     1000,
		MaxConcurrentImageProcessing: 1,
		MaxImageSizeBytes:            1 << 20,
		MaxImageDimension:            100,
		MaxImagePixels:               10,
	})
	asset := &store.Asset{
		ID:           1,
		SubjectID:    10,
		Kind:         store.KindUserAvatar,
		StagingKey:   "staging/1",
		ExpectedSize: int64(len(source)),
		ContentType:  "image/png",
	}

	_, err := processor.Process(t.Context(), asset)
	require.ErrorContains(t, err, "pixel count")
	require.Len(t, objectStore.objects, 1)
}

func TestAnimationDetection(t *testing.T) {
	pngData := testPNG(t, 1, 1)
	animationChunk := []byte{
		0, 0, 0, 0,
		'a', 'c', 'T', 'L',
		0, 0, 0, 0,
	}
	animatedPNG := append(append([]byte{}, pngData[:8]...), animationChunk...)
	animatedPNG = append(animatedPNG, pngData[8:]...)
	require.True(t, imageAnimated(animatedPNG, "png"))
	require.False(t, imageAnimated(pngData, "png"))

	animatedWebP := []byte{
		'R', 'I', 'F', 'F', 4, 0, 0, 0, 'W', 'E', 'B', 'P',
		'A', 'N', 'I', 'M', 0, 0, 0, 0,
	}
	require.True(t, imageAnimated(animatedWebP, "webp"))
}

func TestProcessorAppliesTimeout(t *testing.T) {
	objectStore := &blockingObjectStore{}
	processor := NewProcessor(objectStore, objectStore, config.MediaConfig{
		ImageProcessingTimeoutMs:     20,
		MaxConcurrentImageProcessing: 1,
	})
	_, err := processor.Process(t.Context(), &store.Asset{
		StagingKey:   "staging/1",
		ExpectedSize: 1,
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestProcessorBoundsConcurrency(t *testing.T) {
	source := testPNG(t, 1, 1)
	objectStore := &blockingObjectStore{
		data:        source,
		contentType: "image/png",
		entered:     make(chan struct{}, 1),
		release:     make(chan struct{}),
	}
	processor := NewProcessor(objectStore, objectStore, config.MediaConfig{
		ImageProcessingTimeoutMs:     1000,
		MaxConcurrentImageProcessing: 1,
		MaxImageSizeBytes:            1 << 20,
		MaxImageDimension:            100,
		MaxImagePixels:               100,
	})
	asset := &store.Asset{
		ID:           1,
		SubjectID:    10,
		Kind:         store.KindUserAvatar,
		StagingKey:   "staging/1",
		ExpectedSize: int64(len(source)),
		ContentType:  "image/png",
	}

	firstResult := make(chan error, 1)
	go func() {
		_, err := processor.Process(t.Context(), asset)
		firstResult <- err
	}()
	<-objectStore.entered

	secondCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := processor.Process(secondCtx, asset)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, int32(1), objectStore.calls.Load())

	close(objectStore.release)
	require.NoError(t, <-firstResult)
}

type processorObject struct {
	data        []byte
	contentType string
}

type processorObjectStore struct {
	objects map[string]processorObject
}

type blockingObjectStore struct {
	objectstore.ObjectStore
	data        []byte
	contentType string
	entered     chan struct{}
	release     chan struct{}
	calls       atomic.Int32
}

func (f *blockingObjectStore) GetObject(
	ctx context.Context,
	_ string,
) (io.ReadCloser, *objectstore.ObjectInfo, error) {
	f.calls.Add(1)
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.release == nil {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-f.release:
	}
	return io.NopCloser(bytes.NewReader(f.data)), &objectstore.ObjectInfo{
		Size:        int64(len(f.data)),
		ContentType: f.contentType,
	}, nil
}

func (*blockingObjectStore) PutObject(
	_ context.Context,
	_ string,
	_ string,
	data io.Reader,
) error {
	_, err := io.Copy(io.Discard, data)
	return err
}

func (f *processorObjectStore) CreatePresignedPutRequest(
	context.Context,
	string,
	string,
	int64,
	int64,
) (*objectstore.PresignedPutRequest, error) {
	panic("not used")
}

func (f *processorObjectStore) StatObject(
	_ context.Context,
	key string,
) (*objectstore.ObjectInfo, error) {
	value, ok := f.objects[key]
	if !ok {
		return nil, objectstore.ErrObjectNotFound
	}
	return &objectstore.ObjectInfo{
		Size:        int64(len(value.data)),
		ContentType: value.contentType,
	}, nil
}

func (f *processorObjectStore) GetObject(
	_ context.Context,
	key string,
) (io.ReadCloser, *objectstore.ObjectInfo, error) {
	value, ok := f.objects[key]
	if !ok {
		return nil, nil, objectstore.ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(value.data)), &objectstore.ObjectInfo{
		Size:        int64(len(value.data)),
		ContentType: value.contentType,
	}, nil
}

func (f *processorObjectStore) PutObject(
	_ context.Context,
	key string,
	contentType string,
	data io.Reader,
) error {
	value, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	f.objects[key] = processorObject{data: value, contentType: contentType}
	return nil
}

func (f *processorObjectStore) DeleteObject(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

func (*processorObjectStore) CreatePresignedGetURL(
	context.Context,
	string,
	int64,
) (string, error) {
	panic("not used")
}

func (f *processorObjectStore) ListObjects(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func testJPEGEXIF(orientation uint16) []byte {
	tiff := make([]byte, 26)
	copy(tiff[:2], "II")
	binary.LittleEndian.PutUint16(tiff[2:4], 42)
	binary.LittleEndian.PutUint32(tiff[4:8], 8)
	binary.LittleEndian.PutUint16(tiff[8:10], 1)
	binary.LittleEndian.PutUint16(tiff[10:12], 0x0112)
	binary.LittleEndian.PutUint16(tiff[12:14], 3)
	binary.LittleEndian.PutUint32(tiff[14:18], 1)
	binary.LittleEndian.PutUint16(tiff[18:20], orientation)
	payload := append([]byte("Exif\x00\x00"), tiff...)

	result := []byte{0xff, 0xd8, 0xff, 0xe1}
	length := make([]byte, 2)
	binary.BigEndian.PutUint16(length, uint16(len(payload)+2))
	result = append(result, length...)
	result = append(result, payload...)
	return append(result, 0xff, 0xd9)
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}
