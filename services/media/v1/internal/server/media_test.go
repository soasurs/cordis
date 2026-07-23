package server

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	sn "github.com/bwmarrin/snowflake"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/services/media/v1/config"
	"github.com/soasurs/cordis/services/media/v1/internal/objectstore"
	"github.com/soasurs/cordis/services/media/v1/internal/processing"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
	"github.com/soasurs/cordis/services/media/v1/internal/svc"
)

type fakeStore struct {
	mu     sync.Mutex
	assets map[int64]*store.Asset
	locks  map[int64]*sync.Mutex
}

func newFakeStore() *fakeStore {
	return &fakeStore{assets: make(map[int64]*store.Asset), locks: make(map[int64]*sync.Mutex)}
}

func (f *fakeStore) CreateAssetWithQuota(
	_ context.Context,
	asset *store.Asset,
	activeUploadLimit int64,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var count int64
	for _, current := range f.assets {
		if current.UserID == asset.UserID && current.Status == store.StatusCreated {
			count++
		}
	}
	if count >= activeUploadLimit {
		return store.ErrActiveUploadLimit
	}
	f.assets[asset.ID] = asset
	return nil
}

func (f *fakeStore) createAsset(asset *store.Asset) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assets[asset.ID] = asset
}

func (f *fakeStore) GetAsset(_ context.Context, id int64) (*store.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	asset, ok := f.assets[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return asset, nil
}

func (f *fakeStore) UpdateAsset(_ context.Context, asset *store.Asset) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.assets[asset.ID]; !ok {
		return store.ErrNotFound
	}
	f.assets[asset.ID] = asset
	return nil
}

func (f *fakeStore) ListExpiredUploads(_ context.Context, before int64) ([]*store.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var expired []*store.Asset
	for _, asset := range f.assets {
		if asset.ExpiresAt > 0 &&
			asset.ExpiresAt <= before &&
			asset.Status == store.StatusCreated {
			expired = append(expired, asset)
		}
	}
	return expired, nil
}

func (f *fakeStore) AcquireAssetLock(
	_ context.Context,
	id int64,
) (store.AssetStore, func(), error) {
	f.mu.Lock()
	lock := f.locks[id]
	if lock == nil {
		lock = new(sync.Mutex)
		f.locks[id] = lock
	}
	f.mu.Unlock()
	lock.Lock()
	return f, lock.Unlock, nil
}

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
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: make(map[string]fakeObject)}
}

func (f *fakeObjectStore) CreatePresignedPutURL(
	_ context.Context,
	key string,
	contentType string,
	contentLength int64,
	_ int64,
) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastPresignedKey = key
	f.lastPresignedType = contentType
	f.lastPresignedLength = contentLength
	return "https://s3.example.com/upload/" + key, nil
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

func newTestServer(t *testing.T) (*MediaServer, *fakeStore, *fakeObjectStore) {
	t.Helper()
	assetStore := newFakeStore()
	objStore := newFakeObjectStore()
	node, err := sn.NewNode(1)
	require.NoError(t, err)

	mediaConfig := config.MediaConfig{
		UploadSessionTTLSeconds:      900,
		PresignedURLTTLSeconds:       900,
		MaxUploadSizeBytes:           524288000,
		MaxActiveUploadsPerUser:      5,
		ImageVariantSizes:            []int32{64, 128, 256, 512},
		ImageProcessingTimeoutMs:     30000,
		MaxConcurrentImageProcessing: 2,
		MaxImageSizeBytes:            10 << 20,
		MaxImageDimension:            4096,
		MaxImagePixels:               4096 * 4096,
	}
	processor := processing.NewProcessor(objStore, mediaConfig)
	svcCtx := &svc.ServiceContext{
		Cfg: config.Config{
			ObjectStore: config.ObjectStoreConfig{Backend: "r2"},
			Media:       mediaConfig,
		},
		Store:       assetStore,
		Snowflake:   node,
		ObjectStore: objStore,
		Processor:   processor,
	}
	return New(svcCtx), assetStore, objStore
}

func TestCreateUploadSignsExactImageContract(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	req := newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_USER_AVATAR,
		1024,
		"image/png",
	)

	resp, err := srv.CreateUpload(t.Context(), req)
	require.NoError(t, err)
	require.NotZero(t, resp.GetUploadId())
	require.NotEmpty(t, resp.GetPresignedUrl())

	asset, err := assets.GetAsset(t.Context(), resp.GetUploadId())
	require.NoError(t, err)
	require.Equal(t, store.StatusCreated, asset.Status)
	require.Equal(t, store.KindUserAvatar, asset.Kind)
	require.Equal(t, "r2", asset.StorageBackend)
	require.Equal(t, "staging/"+fmtID(resp.GetUploadId()), asset.StagingKey)
	require.Equal(t, asset.StagingKey, objects.lastPresignedKey)
	require.Equal(t, "image/png", objects.lastPresignedType)
	require.Equal(t, int64(1024), objects.lastPresignedLength)
}

func TestCreateOpaqueUploadUsesPrivateFinalKey(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	req := newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT,
		1024,
		"application/pdf",
	)

	resp, err := srv.CreateUpload(t.Context(), req)
	require.NoError(t, err)
	asset, err := assets.GetAsset(t.Context(), resp.GetUploadId())
	require.NoError(t, err)
	require.Empty(t, asset.StagingKey)
	require.Equal(t, "private/"+fmtID(asset.ID)+"/original", asset.PublishedKey)
	require.Equal(t, asset.PublishedKey, objects.lastPresignedKey)
}

func TestCreateUploadValidation(t *testing.T) {
	srv, _, _ := newTestServer(t)
	tests := []struct {
		name string
		req  *mediav1.CreateUploadRequest
		code codes.Code
	}{
		{name: "user id required", req: new(mediav1.CreateUploadRequest), code: codes.InvalidArgument},
		{
			name: "size required",
			req: newCreateRequest(
				mediav1.AssetKind_ASSET_KIND_USER_AVATAR,
				0,
				"image/png",
			),
			code: codes.InvalidArgument,
		},
		{
			name: "image type rejected",
			req: newCreateRequest(
				mediav1.AssetKind_ASSET_KIND_USER_AVATAR,
				1024,
				"image/svg+xml",
			),
			code: codes.InvalidArgument,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := srv.CreateUpload(t.Context(), test.req)
			require.Equal(t, test.code, status.Code(err))
		})
	}
}

func TestCreateUploadQuotaIsAtomic(t *testing.T) {
	srv, _, _ := newTestServer(t)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := srv.CreateUpload(t.Context(), newCreateRequest(
				mediav1.AssetKind_ASSET_KIND_USER_AVATAR,
				1024,
				"image/png",
			))
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var successes, exhausted int
	for err := range results {
		switch status.Code(err) {
		case codes.OK:
			successes++
		case codes.ResourceExhausted:
			exhausted++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 5, successes)
	require.Equal(t, 7, exhausted)
}

func TestCompleteImageUploadPublishesBeforeDeletingStaging(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	source := testPNG(t, 96, 48)
	createResp, err := srv.CreateUpload(t.Context(), newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_USER_AVATAR,
		int64(len(source)),
		"image/png",
	))
	require.NoError(t, err)
	asset, err := assets.GetAsset(t.Context(), createResp.GetUploadId())
	require.NoError(t, err)
	objects.setObject(asset.StagingKey, "image/png", source)

	resp, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
	require.NoError(t, err)
	require.Equal(t, asset.ID, resp.GetAssetId())
	require.Equal(t, int64(len(source)), resp.GetMetadata().GetSize())
	require.Equal(t, "image/webp", resp.GetMetadata().GetContentType())
	require.Equal(t, int32(96), resp.GetMetadata().GetWidth())
	require.Equal(t, int32(48), resp.GetMetadata().GetHeight())
	require.Len(t, resp.GetVariants(), 4)
	require.False(t, objects.hasObject(asset.StagingKey))
	for _, variant := range resp.GetVariants() {
		require.Positive(t, variant.GetSize())
		require.Positive(t, variant.GetMaxDimension())
		require.True(t, objects.hasObject(
			"public/"+fmtID(asset.ID)+"/"+fmtID(int64(variant.GetMaxDimension()))+".webp",
		))
	}

	retry, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
	require.NoError(t, err)
	require.Equal(t, resp.GetAssetId(), retry.GetAssetId())
	require.Equal(t, resp.GetVariants(), retry.GetVariants())
}

func TestCompleteUploadResumesCompletingAndProcessing(t *testing.T) {
	for _, initialStatus := range []store.Status{store.StatusCompleting, store.StatusProcessing} {
		t.Run(string(initialStatus), func(t *testing.T) {
			srv, assets, objects := newTestServer(t)
			source := testPNG(t, 20, 10)
			asset := &store.Asset{
				ID:           123,
				UserID:       1001,
				Kind:         store.KindUserAvatar,
				Status:       initialStatus,
				StagingKey:   "staging/123",
				ExpectedSize: int64(len(source)),
				ActualSize:   int64(len(source)),
				ContentType:  "image/png",
			}
			assets.createAsset(asset)
			objects.setObject(asset.StagingKey, "image/png", source)

			resp, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
			require.NoError(t, err)
			require.Equal(t, asset.ID, resp.GetAssetId())
			require.Equal(t, store.StatusReady, asset.Status)
		})
	}
}

func TestCompleteOpaqueUploadKeepsPrivateObject(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	source := []byte("opaque attachment")
	createResp, err := srv.CreateUpload(t.Context(), newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT,
		int64(len(source)),
		"application/octet-stream",
	))
	require.NoError(t, err)
	asset, err := assets.GetAsset(t.Context(), createResp.GetUploadId())
	require.NoError(t, err)
	objects.setObject(asset.PublishedKey, asset.ContentType, source)

	resp, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
	require.NoError(t, err)
	require.Equal(t, "application/octet-stream", resp.GetMetadata().GetContentType())
	require.True(t, objects.hasObject(asset.PublishedKey))
	require.Equal(t, store.StatusReady, asset.Status)
}

func TestCompleteUploadRejectsObjectMetadataMismatch(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		data        []byte
	}{
		{name: "size", contentType: "image/png", data: []byte("short")},
		{name: "content type", contentType: "image/jpeg", data: bytes.Repeat([]byte("x"), 10)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv, assets, objects := newTestServer(t)
			asset := &store.Asset{
				ID:           123,
				UserID:       1001,
				Kind:         store.KindUserAvatar,
				Status:       store.StatusCreated,
				StagingKey:   "staging/123",
				ExpectedSize: 10,
				ContentType:  "image/png",
			}
			assets.createAsset(asset)
			objects.setObject(asset.StagingKey, test.contentType, test.data)

			_, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
			require.Equal(t, codes.FailedPrecondition, status.Code(err))
			require.Equal(t, store.StatusFailed, asset.Status)
			require.False(t, objects.hasObject(asset.StagingKey))
		})
	}
}

func TestCompleteAndAbortVerifyOwner(t *testing.T) {
	srv, assets, _ := newTestServer(t)
	asset := &store.Asset{ID: 123, UserID: 1001, Status: store.StatusCreated}
	assets.createAsset(asset)

	_, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 2002))
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	abortReq := new(mediav1.AbortUploadRequest)
	abortReq.SetUploadId(asset.ID)
	abortReq.SetUserId(2002)
	_, err = srv.AbortUpload(t.Context(), abortReq)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestAbortUploadIsIdempotentAndPreservesReadyAsset(t *testing.T) {
	srv, assets, _ := newTestServer(t)
	asset := &store.Asset{
		ID:         123,
		UserID:     1001,
		Kind:       store.KindUserAvatar,
		Status:     store.StatusCreated,
		StagingKey: "staging/123",
	}
	assets.createAsset(asset)
	req := new(mediav1.AbortUploadRequest)
	req.SetUploadId(asset.ID)
	req.SetUserId(1001)

	_, err := srv.AbortUpload(t.Context(), req)
	require.NoError(t, err)
	_, err = srv.AbortUpload(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, store.StatusAborted, asset.Status)

	ready := &store.Asset{ID: 124, UserID: 1001, Status: store.StatusReady}
	assets.createAsset(ready)
	req.SetUploadId(ready.ID)
	_, err = srv.AbortUpload(t.Context(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.Equal(t, store.StatusReady, ready.Status)
}

func TestCleanupExpiredRechecksStateUnderLock(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	now := time.Now().UnixMilli()
	expired := &store.Asset{
		ID:           1,
		UserID:       1001,
		Kind:         store.KindMessageAttachment,
		Status:       store.StatusCreated,
		PublishedKey: "private/1/original",
		ExpiresAt:    now - 1000,
	}
	ready := &store.Asset{
		ID:        2,
		UserID:    1001,
		Status:    store.StatusReady,
		ExpiresAt: now - 1000,
	}
	assets.createAsset(expired)
	assets.createAsset(ready)
	objects.setObject(expired.PublishedKey, "application/octet-stream", []byte("orphan"))

	require.NoError(t, srv.CleanupExpired(t.Context()))
	require.Equal(t, store.StatusExpired, expired.Status)
	require.False(t, objects.hasObject(expired.PublishedKey))
	require.Equal(t, store.StatusReady, ready.Status)
}

func newCreateRequest(
	kind mediav1.AssetKind,
	expectedSize int64,
	contentType string,
) *mediav1.CreateUploadRequest {
	req := new(mediav1.CreateUploadRequest)
	req.SetUserId(1001)
	req.SetKind(kind)
	req.SetExpectedSize(expectedSize)
	req.SetContentType(contentType)
	return req
}

func completeRequest(uploadID, userID int64) *mediav1.CompleteUploadRequest {
	req := new(mediav1.CompleteUploadRequest)
	req.SetUploadId(uploadID)
	req.SetUserId(userID)
	return req
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x % 255),
				G: uint8(y % 255),
				B: 100,
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func fmtID(id int64) string {
	return strconv.FormatInt(id, 10)
}
