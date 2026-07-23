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
		if current.CreatedByUserID == asset.CreatedByUserID &&
			current.Status == store.StatusCreated {
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

func (f *fakeStore) ListAssets(_ context.Context, ids []int64) ([]*store.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	assets := make([]*store.Asset, 0, len(ids))
	for _, id := range ids {
		if asset := f.assets[id]; asset != nil {
			assets = append(assets, asset)
		}
	}
	return assets, nil
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

func newTestServer(t *testing.T) (*MediaServer, *fakeStore, *fakeObjectStore) {
	t.Helper()
	assetStore := newFakeStore()
	objStore := newFakeObjectStore()
	node, err := sn.NewNode(1)
	require.NoError(t, err)

	mediaConfig := config.MediaConfig{
		UploadSessionTTLSeconds:      1800,
		PresignedURLTTLSeconds:       900,
		MaxUploadSizeBytes:           524288000,
		MaxActiveUploadsPerUser:      5,
		ImageProcessingTimeoutMs:     30000,
		MaxConcurrentImageProcessing: 2,
		MaxImageSizeBytes:            10 << 20,
		MaxImageDimension:            4096,
		MaxImagePixels:               4096 * 4096,
		AttachmentAccessMode:         config.AttachmentAccessPublic,
		AttachmentDownloadTTLSeconds: 3600,
	}
	processor := processing.NewProcessor(objStore, objStore, mediaConfig)
	svcCtx := &svc.ServiceContext{
		Cfg: config.Config{
			ObjectStore: config.ObjectStoreConfig{
				Backend:                 "r2",
				AttachmentPublicBaseURL: "https://cdn.example.com",
			},
			Media: mediaConfig,
		},
		Store:                 assetStore,
		Snowflake:             node,
		PublicObjectStore:     objStore,
		StagingObjectStore:    objStore,
		AttachmentObjectStore: objStore,
		Processor:             processor,
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
	require.Equal(t, map[string]string{
		"Content-Length": "1024",
		"Content-Type":   "image/png",
	}, resp.GetRequestHeaders())

	asset, err := assets.GetAsset(t.Context(), resp.GetUploadId())
	require.NoError(t, err)
	require.Equal(t, store.StatusCreated, asset.Status)
	require.Equal(t, int64(1001), asset.CreatedByUserID)
	require.Equal(t, int64(1001), asset.SubjectID)
	require.Equal(t, store.KindUserAvatar, asset.Kind)
	require.Equal(t, "r2", asset.StorageBackend)
	require.Equal(t, "staging/"+fmtID(resp.GetUploadId()), asset.StagingKey)
	require.Equal(t, asset.StagingKey, objects.lastPresignedKey)
	require.Equal(t, "image/png", objects.lastPresignedType)
	require.Equal(t, int64(1024), objects.lastPresignedLength)
	require.Equal(t, int64(900_000), asset.ExpiresAt-resp.GetExpiresAt())
}

func TestCreateAttachmentUploadUsesTokenizedFinalKey(t *testing.T) {
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
	require.Equal(t, int64(3001), asset.SubjectID)
	require.Equal(t, "report.pdf", asset.Filename)
	require.Len(t, asset.StorageToken, 22)
	require.Equal(t, "attachments/3001/"+fmtID(asset.ID)+"/"+asset.StorageToken+"/report.pdf", asset.PublishedKey)
	require.Equal(t, asset.PublishedKey, objects.lastPresignedKey)
}

func TestCreateUploadUsesPurposeSpecificObjectStores(t *testing.T) {
	assetStore := newFakeStore()
	stagingStore := newFakeObjectStore()
	publicStore := newFakeObjectStore()
	attachmentStore := newFakeObjectStore()
	node, err := sn.NewNode(1)
	require.NoError(t, err)
	mediaConfig := config.MediaConfig{
		UploadSessionTTLSeconds:      1800,
		PresignedURLTTLSeconds:       900,
		MaxUploadSizeBytes:           524288000,
		MaxActiveUploadsPerUser:      5,
		ImageProcessingTimeoutMs:     30000,
		MaxConcurrentImageProcessing: 2,
		MaxImageSizeBytes:            10 << 20,
		MaxImageDimension:            4096,
		MaxImagePixels:               4096 * 4096,
		AttachmentAccessMode:         config.AttachmentAccessPublic,
		AttachmentDownloadTTLSeconds: 3600,
	}
	srv := New(&svc.ServiceContext{
		Cfg: config.Config{
			ObjectStore: config.ObjectStoreConfig{
				Backend:                 "r2",
				AttachmentPublicBaseURL: "https://cdn.example.com",
			},
			Media: mediaConfig,
		},
		Store:                 assetStore,
		Snowflake:             node,
		PublicObjectStore:     publicStore,
		StagingObjectStore:    stagingStore,
		AttachmentObjectStore: attachmentStore,
		Processor:             processing.NewProcessor(stagingStore, publicStore, mediaConfig),
	})

	source := testPNG(t, 2, 1)
	imageResp, err := srv.CreateUpload(t.Context(), newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_USER_AVATAR,
		int64(len(source)),
		"image/png",
	))
	require.NoError(t, err)
	image, err := assetStore.GetAsset(t.Context(), imageResp.GetUploadId())
	require.NoError(t, err)
	require.Equal(t, image.StagingKey, stagingStore.lastPresignedKey)
	require.Empty(t, publicStore.lastPresignedKey)
	require.Empty(t, attachmentStore.lastPresignedKey)

	stagingStore.setObject(image.StagingKey, image.ContentType, source)
	_, err = srv.CompleteUpload(t.Context(), completeRequest(image.ID, 1001))
	require.NoError(t, err)
	require.False(t, stagingStore.hasObject(image.StagingKey))
	require.True(t, publicStore.hasObject(image.PublicKey()))

	attachmentResp, err := srv.CreateUpload(t.Context(), newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT,
		10,
		"application/octet-stream",
	))
	require.NoError(t, err)
	attachment, err := assetStore.GetAsset(t.Context(), attachmentResp.GetUploadId())
	require.NoError(t, err)
	require.Equal(t, attachment.PublishedKey, attachmentStore.lastPresignedKey)
}

func TestCreateGuildIconUploadUsesTypedSubject(t *testing.T) {
	srv, assets, _ := newTestServer(t)
	req := newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_GUILD_ICON,
		1024,
		"image/png",
	)

	resp, err := srv.CreateUpload(t.Context(), req)
	require.NoError(t, err)
	asset, err := assets.GetAsset(t.Context(), resp.GetUploadId())
	require.NoError(t, err)
	require.Equal(t, int64(1001), asset.CreatedByUserID)
	require.Equal(t, int64(2001), asset.SubjectID)
	require.Equal(t, store.KindGuildIcon, asset.Kind)
	require.Equal(
		t,
		"icons/2001/"+fmtID(asset.ID),
		asset.PublicKey(),
	)
}

func TestCreateUploadValidation(t *testing.T) {
	srv, _, _ := newTestServer(t)
	tests := []struct {
		name string
		req  *mediav1.CreateUploadRequest
		code codes.Code
	}{
		{name: "actor user id required", req: new(mediav1.CreateUploadRequest), code: codes.InvalidArgument},
		{
			name: "purpose required",
			req: func() *mediav1.CreateUploadRequest {
				req := new(mediav1.CreateUploadRequest)
				req.SetActorUserId(1001)
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "guild id required",
			req: func() *mediav1.CreateUploadRequest {
				req := newCreateRequest(
					mediav1.AssetKind_ASSET_KIND_GUILD_ICON,
					1024,
					"image/png",
				)
				req.SetGuildIcon(new(mediav1.GuildIconUploadPurpose))
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "channel id required",
			req: func() *mediav1.CreateUploadRequest {
				req := newCreateRequest(
					mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT,
					1024,
					"application/octet-stream",
				)
				req.SetMessageAttachment(new(mediav1.MessageAttachmentUploadPurpose))
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "attachment filename invalid",
			req: func() *mediav1.CreateUploadRequest {
				req := newCreateRequest(
					mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT,
					1024,
					"application/octet-stream",
				)
				req.GetMessageAttachment().SetFilename("../secret")
				return req
			}(),
			code: codes.InvalidArgument,
		},
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

func TestGetAssetReturnsCreatorAndSubject(t *testing.T) {
	srv, assets, _ := newTestServer(t)
	asset := &store.Asset{
		ID:              123,
		CreatedByUserID: 1001,
		SubjectID:       2001,
		Kind:            store.KindGuildIcon,
		Status:          store.StatusReady,
	}
	assets.createAsset(asset)

	req := new(mediav1.GetAssetRequest)
	req.SetAssetId(asset.ID)
	resp, err := srv.GetAsset(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, asset.CreatedByUserID, resp.GetAsset().GetCreatedByUserId())
	require.Equal(t, asset.SubjectID, resp.GetAsset().GetSubjectId())
	require.Equal(
		t,
		mediav1.AssetKind_ASSET_KIND_GUILD_ICON,
		resp.GetAsset().GetKind(),
	)
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
	require.Equal(t, "image/png", resp.GetMetadata().GetContentType())
	require.Equal(t, int32(96), resp.GetMetadata().GetWidth())
	require.Equal(t, int32(48), resp.GetMetadata().GetHeight())
	require.False(t, objects.hasObject(asset.StagingKey))
	require.True(t, objects.hasObject("avatars/1001/"+fmtID(asset.ID)))

	retry, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
	require.NoError(t, err)
	require.Equal(t, resp.GetAssetId(), retry.GetAssetId())
	require.Equal(t, resp.GetMetadata().GetContentType(), retry.GetMetadata().GetContentType())
}

func TestCompleteUploadResumesCompleting(t *testing.T) {
	for _, initialStatus := range []store.Status{store.StatusCompleting} {
		t.Run(string(initialStatus), func(t *testing.T) {
			srv, assets, objects := newTestServer(t)
			source := testPNG(t, 20, 10)
			asset := &store.Asset{
				ID:              123,
				CreatedByUserID: 1001,
				SubjectID:       1001,
				Kind:            store.KindUserAvatar,
				Status:          initialStatus,
				StagingKey:      "staging/123",
				ExpectedSize:    int64(len(source)),
				ActualSize:      int64(len(source)),
				ContentType:     "image/png",
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
	require.Equal(t, "report.pdf", resp.GetMetadata().GetFilename())
	require.Contains(t, resp.GetMetadata().GetUrl(), "/report.pdf")
	require.Zero(t, resp.GetMetadata().GetUrlExpiresAt())
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
				ID:              123,
				CreatedByUserID: 1001,
				SubjectID:       1001,
				Kind:            store.KindUserAvatar,
				Status:          store.StatusCreated,
				StagingKey:      "staging/123",
				ExpectedSize:    10,
				ContentType:     "image/png",
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
	asset := &store.Asset{ID: 123, CreatedByUserID: 1001, Status: store.StatusCreated}
	assets.createAsset(asset)

	_, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 2002))
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	abortReq := new(mediav1.AbortUploadRequest)
	abortReq.SetUploadId(asset.ID)
	abortReq.SetActorUserId(2002)
	_, err = srv.AbortUpload(t.Context(), abortReq)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestAbortUploadIsIdempotentAndPreservesReadyAsset(t *testing.T) {
	srv, assets, _ := newTestServer(t)
	asset := &store.Asset{
		ID:              123,
		CreatedByUserID: 1001,
		SubjectID:       1001,
		Kind:            store.KindUserAvatar,
		Status:          store.StatusCreated,
		StagingKey:      "staging/123",
	}
	assets.createAsset(asset)
	req := new(mediav1.AbortUploadRequest)
	req.SetUploadId(asset.ID)
	req.SetActorUserId(1001)

	_, err := srv.AbortUpload(t.Context(), req)
	require.NoError(t, err)
	_, err = srv.AbortUpload(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, store.StatusAborted, asset.Status)

	ready := &store.Asset{ID: 124, CreatedByUserID: 1001, Status: store.StatusReady}
	assets.createAsset(ready)
	req.SetUploadId(ready.ID)
	_, err = srv.AbortUpload(t.Context(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.Equal(t, store.StatusReady, ready.Status)
}

func TestBatchGetAssetURLsUsesConfiguredAccessMode(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	attachment := &store.Asset{
		ID:             123,
		Kind:           store.KindMessageAttachment,
		Status:         store.StatusReady,
		PublishedKey:   "attachments/10/123/token/report.pdf",
		StorageBackend: "r2",
	}
	assets.createAsset(attachment)
	req := new(mediav1.BatchGetAssetURLsRequest)
	req.SetAssetIds([]int64{attachment.ID, attachment.ID})

	resp, err := srv.BatchGetAssetURLs(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetAssets(), 1)
	require.Equal(t, "https://cdn.example.com/attachments/10/123/token/report.pdf", resp.GetAssets()[0].GetUrl())
	require.Zero(t, resp.GetAssets()[0].GetExpiresAt())
	require.Empty(t, objects.lastDownloadKey)

	image := &store.Asset{
		ID:           124,
		Kind:         store.KindUserAvatar,
		Status:       store.StatusReady,
		PublishedKey: "avatars/10/124",
	}
	assets.createAsset(image)
	req.SetAssetIds([]int64{image.ID})
	_, err = srv.BatchGetAssetURLs(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	srv.svcCtx.Cfg.Media.AttachmentAccessMode = config.AttachmentAccessPresigned
	req.SetAssetIds([]int64{attachment.ID})
	resp, err = srv.BatchGetAssetURLs(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "https://s3.example.com/"+attachment.PublishedKey, resp.GetAssets()[0].GetUrl())
	require.Equal(t, attachment.PublishedKey, objects.lastDownloadKey)
	require.Greater(t, resp.GetAssets()[0].GetExpiresAt(), time.Now().UnixMilli())
}

func TestCleanupExpiredRechecksStateUnderLock(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	now := time.Now().UnixMilli()
	expired := &store.Asset{
		ID:              1,
		CreatedByUserID: 1001,
		SubjectID:       3001,
		Kind:            store.KindMessageAttachment,
		Status:          store.StatusCreated,
		PublishedKey:    "attachments/3001/1/token/file.bin",
		ExpiresAt:       now - 1000,
	}
	ready := &store.Asset{
		ID:              2,
		CreatedByUserID: 1001,
		Status:          store.StatusReady,
		ExpiresAt:       now - 1000,
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
	req.SetActorUserId(1001)
	req.SetExpectedSize(expectedSize)
	req.SetContentType(contentType)
	switch kind {
	case mediav1.AssetKind_ASSET_KIND_USER_AVATAR:
		req.SetUserAvatar(new(mediav1.UserAvatarUploadPurpose))
	case mediav1.AssetKind_ASSET_KIND_GUILD_ICON:
		purpose := new(mediav1.GuildIconUploadPurpose)
		purpose.SetGuildId(2001)
		req.SetGuildIcon(purpose)
	case mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT:
		purpose := new(mediav1.MessageAttachmentUploadPurpose)
		purpose.SetChannelId(3001)
		purpose.SetFilename("report.pdf")
		req.SetMessageAttachment(purpose)
	}
	return req
}

func completeRequest(uploadID, actorUserID int64) *mediav1.CompleteUploadRequest {
	req := new(mediav1.CompleteUploadRequest)
	req.SetUploadId(uploadID)
	req.SetActorUserId(actorUserID)
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
