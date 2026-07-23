package server

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"strings"
	"time"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/services/media/v1/internal/objectstore"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

var allowedImageContentTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

func (s *MediaServer) CreateUpload(
	ctx context.Context,
	req *mediav1.CreateUploadRequest,
) (*mediav1.CreateUploadResponse, error) {
	userID := req.GetUserId()
	if userID <= 0 {
		return nil, errUserIDRequired
	}
	kind, ok := kindFromProto(req.GetKind())
	if !ok {
		return nil, errKindInvalid
	}
	expectedSize := req.GetExpectedSize()
	if expectedSize <= 0 {
		return nil, errSizeRequired
	}
	if expectedSize > s.svcCtx.Cfg.Media.MaxUploadSize() {
		return nil, errSizeExceeded
	}
	contentType, err := normalizeContentType(req.GetContentType())
	if err != nil {
		return nil, err
	}
	if kind.IsImage() {
		if _, ok := allowedImageContentTypes[contentType]; !ok {
			return nil, errContentTypeInvalid
		}
		if expectedSize > s.svcCtx.Cfg.Media.MaxImageSize() {
			return nil, errSizeExceeded
		}
	}

	id := s.svcCtx.Snowflake.Generate().Int64()
	now := time.Now().UnixMilli()
	uploadTTL := s.svcCtx.Cfg.Media.UploadSessionTTL()
	presignedTTL := s.svcCtx.Cfg.Media.PresignedURLTTL()
	asset := &store.Asset{
		ID:             id,
		UserID:         userID,
		Kind:           kind,
		Status:         store.StatusCreated,
		StorageBackend: s.storageBackend(),
		ExpectedSize:   expectedSize,
		ContentType:    contentType,
		ExpiresAt:      now + uploadTTL*1000,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if kind.IsImage() {
		asset.StagingKey = fmt.Sprintf("staging/%d", id)
	} else {
		asset.PublishedKey = fmt.Sprintf("private/%d/original", id)
	}

	presignedURL, err := s.svcCtx.ObjectStore.CreatePresignedPutURL(
		ctx,
		uploadObjectKey(asset),
		contentType,
		expectedSize,
		presignedTTL,
	)
	if err != nil {
		return nil, fmt.Errorf("create presigned url: %w", err)
	}
	if err := s.svcCtx.Store.CreateAssetWithQuota(
		ctx,
		asset,
		s.svcCtx.Cfg.Media.MaxActiveUploads(),
	); err != nil {
		if errors.Is(err, store.ErrActiveUploadLimit) {
			return nil, errUploadLimit
		}
		return nil, fmt.Errorf("create asset: %w", err)
	}

	resp := new(mediav1.CreateUploadResponse)
	resp.SetUploadId(id)
	resp.SetPresignedUrl(presignedURL)
	resp.SetExpiresAt(asset.ExpiresAt)
	return resp, nil
}

func (s *MediaServer) CompleteUpload(
	ctx context.Context,
	req *mediav1.CompleteUploadRequest,
) (*mediav1.CompleteUploadResponse, error) {
	userID := req.GetUserId()
	if userID <= 0 {
		return nil, errUserIDRequired
	}
	uploadID := req.GetUploadId()
	lockedStore, unlock, err := s.svcCtx.Store.AcquireAssetLock(ctx, uploadID)
	if err != nil {
		return nil, fmt.Errorf("lock asset: %w", err)
	}
	defer unlock()

	asset, err := s.getUpload(ctx, lockedStore, uploadID)
	if err != nil {
		return nil, err
	}
	if userID != asset.UserID {
		return nil, errWrongOwner
	}
	return s.completeLocked(ctx, lockedStore, asset)
}

func (s *MediaServer) completeLocked(
	ctx context.Context,
	assetStore store.AssetStore,
	asset *store.Asset,
) (*mediav1.CompleteUploadResponse, error) {
	switch asset.Status {
	case store.StatusReady:
		return s.buildCompleteResponse(asset)
	case store.StatusFailed:
		return nil, errProcessingFailed
	case store.StatusAborted:
		return nil, errAlreadyAborted
	case store.StatusExpired:
		return nil, errUploadNotFound
	}

	if asset.Status == store.StatusCreated {
		if asset.ExpiresAt > 0 && asset.ExpiresAt <= time.Now().UnixMilli() {
			asset.Status = store.StatusExpired
			if err := assetStore.UpdateAsset(ctx, asset); err != nil {
				return nil, fmt.Errorf("expire upload: %w", err)
			}
			s.deleteUploadObject(asset)
			return nil, errUploadNotFound
		}
		info, err := s.statUploadedObject(ctx, assetStore, asset)
		if err != nil {
			return nil, err
		}
		asset.Status = store.StatusCompleting
		if err := assetStore.UpdateAsset(ctx, asset); err != nil {
			return nil, fmt.Errorf("update asset to completing: %w", err)
		}
		if err := s.markUploaded(ctx, assetStore, asset, info); err != nil {
			return nil, err
		}
	}

	if asset.Status == store.StatusCompleting {
		info, err := s.statUploadedObject(ctx, assetStore, asset)
		if err != nil {
			return nil, err
		}
		if err := s.markUploaded(ctx, assetStore, asset, info); err != nil {
			return nil, err
		}
	}

	switch asset.Status {
	case store.StatusUploaded, store.StatusProcessing:
		if asset.Kind.IsImage() {
			return s.processImage(ctx, assetStore, asset)
		}
		asset.Status = store.StatusReady
		if err := assetStore.UpdateAsset(ctx, asset); err != nil {
			return nil, fmt.Errorf("update asset to ready: %w", err)
		}
		return s.buildCompleteResponse(asset)
	default:
		return nil, errNotUploaded
	}
}

func (s *MediaServer) statUploadedObject(
	ctx context.Context,
	assetStore store.AssetStore,
	asset *store.Asset,
) (*objectstore.ObjectInfo, error) {
	info, err := s.svcCtx.ObjectStore.StatObject(ctx, uploadObjectKey(asset))
	if err != nil {
		if errors.Is(err, objectstore.ErrObjectNotFound) {
			return nil, errNotUploaded
		}
		return nil, errObjectStoreDown
	}
	if info.Size != asset.ExpectedSize {
		if err := s.failUpload(ctx, assetStore, asset, fmt.Sprintf(
			"uploaded size %d does not match expected size %d",
			info.Size,
			asset.ExpectedSize,
		)); err != nil {
			return nil, err
		}
		return nil, errSizeMismatch
	}
	actualContentType, err := normalizeContentType(info.ContentType)
	if err != nil || actualContentType != asset.ContentType {
		if err := s.failUpload(ctx, assetStore, asset, fmt.Sprintf(
			"uploaded content type %q does not match expected content type %q",
			info.ContentType,
			asset.ContentType,
		)); err != nil {
			return nil, err
		}
		return nil, errContentTypeMismatch
	}
	return info, nil
}

func (s *MediaServer) markUploaded(
	ctx context.Context,
	assetStore store.AssetStore,
	asset *store.Asset,
	info *objectstore.ObjectInfo,
) error {
	asset.ActualSize = info.Size
	asset.Status = store.StatusUploaded
	if err := assetStore.UpdateAsset(ctx, asset); err != nil {
		return fmt.Errorf("update asset to uploaded: %w", err)
	}
	return nil
}

func (s *MediaServer) processImage(
	ctx context.Context,
	assetStore store.AssetStore,
	asset *store.Asset,
) (*mediav1.CompleteUploadResponse, error) {
	if asset.Status == store.StatusUploaded {
		asset.Status = store.StatusProcessing
		if err := assetStore.UpdateAsset(ctx, asset); err != nil {
			return nil, fmt.Errorf("update asset to processing: %w", err)
		}
	}

	result, err := s.svcCtx.Processor.Process(ctx, asset)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, errProcessingInterrupted
		}
		asset.Status = store.StatusFailed
		asset.ErrorMessage = err.Error()
		if updateErr := assetStore.UpdateAsset(ctx, asset); updateErr != nil {
			return nil, fmt.Errorf("record processing failure: %w", updateErr)
		}
		s.deleteUploadObject(asset)
		return nil, errProcessingFailed
	}

	asset.PublishedKey = asset.PublicPrefix()
	asset.Width = result.Width
	asset.Height = result.Height
	asset.SetVariants(result.Variants)
	asset.ContentType = "image/webp"
	asset.Status = store.StatusReady
	asset.ErrorMessage = ""
	if err := assetStore.UpdateAsset(ctx, asset); err != nil {
		return nil, fmt.Errorf("update asset to ready: %w", err)
	}
	s.deleteUploadObject(asset)
	return s.buildCompleteResponse(asset)
}

func (s *MediaServer) failUpload(
	ctx context.Context,
	assetStore store.AssetStore,
	asset *store.Asset,
	message string,
) error {
	asset.Status = store.StatusFailed
	asset.ErrorMessage = message
	if err := assetStore.UpdateAsset(ctx, asset); err != nil {
		return fmt.Errorf("record invalid upload: %w", err)
	}
	s.deleteUploadObject(asset)
	return nil
}

func (s *MediaServer) buildCompleteResponse(
	asset *store.Asset,
) (*mediav1.CompleteUploadResponse, error) {
	resp := new(mediav1.CompleteUploadResponse)
	resp.SetAssetId(asset.ID)

	metadata := new(mediav1.AssetMetadata)
	metadata.SetSize(asset.ActualSize)
	metadata.SetContentType(asset.ContentType)
	metadata.SetWidth(asset.Width)
	metadata.SetHeight(asset.Height)
	resp.SetMetadata(metadata)
	resp.SetVariants(variantsToProto(asset.Variants()))
	return resp, nil
}

func (s *MediaServer) AbortUpload(
	ctx context.Context,
	req *mediav1.AbortUploadRequest,
) (*mediav1.AbortUploadResponse, error) {
	userID := req.GetUserId()
	if userID <= 0 {
		return nil, errUserIDRequired
	}
	uploadID := req.GetUploadId()
	lockedStore, unlock, err := s.svcCtx.Store.AcquireAssetLock(ctx, uploadID)
	if err != nil {
		return nil, fmt.Errorf("lock asset: %w", err)
	}
	defer unlock()

	asset, err := s.getUpload(ctx, lockedStore, uploadID)
	if err != nil {
		return nil, err
	}
	if userID != asset.UserID {
		return nil, errWrongOwner
	}
	switch asset.Status {
	case store.StatusAborted:
		return new(mediav1.AbortUploadResponse), nil
	case store.StatusReady:
		return nil, errAlreadyCompleted
	case store.StatusFailed, store.StatusExpired:
		return nil, errAlreadyAborted
	}

	asset.Status = store.StatusAborted
	if err := lockedStore.UpdateAsset(ctx, asset); err != nil {
		return nil, fmt.Errorf("update asset to aborted: %w", err)
	}
	s.deleteUploadObject(asset)
	return new(mediav1.AbortUploadResponse), nil
}

func (s *MediaServer) GetAsset(
	ctx context.Context,
	req *mediav1.GetAssetRequest,
) (*mediav1.GetAssetResponse, error) {
	asset, err := s.svcCtx.Store.GetAsset(ctx, req.GetAssetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, errAssetNotFound
		}
		return nil, fmt.Errorf("get asset: %w", err)
	}

	resp := new(mediav1.GetAssetResponse)
	value := new(mediav1.Asset)
	value.SetId(asset.ID)
	value.SetOwnerUserId(asset.UserID)
	value.SetKind(kindToProto(asset.Kind))
	value.SetStatus(assetStatusToProto(asset.Status))
	value.SetStorageBackend(asset.StorageBackend)
	value.SetContentType(asset.ContentType)
	value.SetSize(asset.ActualSize)
	value.SetWidth(asset.Width)
	value.SetHeight(asset.Height)
	value.SetVariants(variantsToProto(asset.Variants()))
	value.SetCreatedAt(asset.CreatedAt)
	value.SetUpdatedAt(asset.UpdatedAt)
	resp.SetAsset(value)
	return resp, nil
}

func (s *MediaServer) GetAssetURL(
	ctx context.Context,
	req *mediav1.GetAssetURLRequest,
) (*mediav1.GetAssetURLResponse, error) {
	asset, err := s.svcCtx.Store.GetAsset(ctx, req.GetAssetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, errAssetNotFound
		}
		return nil, fmt.Errorf("get asset: %w", err)
	}
	if asset.Status != store.StatusReady {
		return nil, errAssetNotReady
	}

	key, err := assetURLKey(asset, req.GetPurpose())
	if err != nil {
		return nil, err
	}
	expiresIn := req.GetExpiresInSeconds()
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	url, err := s.svcCtx.ObjectStore.CreatePresignedGetURL(ctx, key, expiresIn)
	if err != nil {
		return nil, fmt.Errorf("create presigned get url: %w", err)
	}

	resp := new(mediav1.GetAssetURLResponse)
	resp.SetUrl(url)
	resp.SetExpiresAt(time.Now().UnixMilli() + expiresIn*1000)
	return resp, nil
}

func assetURLKey(asset *store.Asset, purpose mediav1.AssetURLPurpose) (string, error) {
	variants := asset.Variants()
	if asset.Kind.IsImage() {
		if len(variants) == 0 {
			return "", errAssetNotReady
		}
		switch purpose {
		case mediav1.AssetURLPurpose_ASSET_URL_PURPOSE_UNSPECIFIED,
			mediav1.AssetURLPurpose_ASSET_URL_PURPOSE_PUBLIC:
			return variants[0].Key, nil
		case mediav1.AssetURLPurpose_ASSET_URL_PURPOSE_SIGNED_DOWNLOAD:
			return variants[len(variants)-1].Key, nil
		default:
			return "", errURLPurposeInvalid
		}
	}
	if purpose != mediav1.AssetURLPurpose_ASSET_URL_PURPOSE_SIGNED_DOWNLOAD {
		return "", errURLPurposeInvalid
	}
	return asset.PublishedKey, nil
}

func variantsToProto(variants []store.Variant) []*mediav1.AssetVariant {
	result := make([]*mediav1.AssetVariant, 0, len(variants))
	for _, variant := range variants {
		value := new(mediav1.AssetVariant)
		value.SetMaxDimension(variant.MaxDimension)
		value.SetWidth(variant.Width)
		value.SetHeight(variant.Height)
		value.SetSize(variant.Size)
		result = append(result, value)
	}
	return result
}

func kindFromProto(kind mediav1.AssetKind) (store.Kind, bool) {
	switch kind {
	case mediav1.AssetKind_ASSET_KIND_USER_AVATAR:
		return store.KindUserAvatar, true
	case mediav1.AssetKind_ASSET_KIND_GUILD_ICON:
		return store.KindGuildIcon, true
	case mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT:
		return store.KindMessageAttachment, true
	default:
		return "", false
	}
}

func kindToProto(kind store.Kind) mediav1.AssetKind {
	switch kind {
	case store.KindUserAvatar:
		return mediav1.AssetKind_ASSET_KIND_USER_AVATAR
	case store.KindGuildIcon:
		return mediav1.AssetKind_ASSET_KIND_GUILD_ICON
	case store.KindMessageAttachment:
		return mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT
	default:
		return mediav1.AssetKind_ASSET_KIND_UNSPECIFIED
	}
}

func assetStatusToProto(statusValue store.Status) mediav1.AssetStatus {
	switch statusValue {
	case store.StatusCreated:
		return mediav1.AssetStatus_ASSET_STATUS_CREATED
	case store.StatusCompleting:
		return mediav1.AssetStatus_ASSET_STATUS_COMPLETING
	case store.StatusUploaded:
		return mediav1.AssetStatus_ASSET_STATUS_UPLOADED
	case store.StatusProcessing:
		return mediav1.AssetStatus_ASSET_STATUS_PROCESSING
	case store.StatusReady:
		return mediav1.AssetStatus_ASSET_STATUS_READY
	case store.StatusFailed:
		return mediav1.AssetStatus_ASSET_STATUS_FAILED
	case store.StatusAborted:
		return mediav1.AssetStatus_ASSET_STATUS_ABORTED
	case store.StatusExpired:
		return mediav1.AssetStatus_ASSET_STATUS_EXPIRED
	default:
		return mediav1.AssetStatus_ASSET_STATUS_UNSPECIFIED
	}
}

func (s *MediaServer) CleanupExpired(ctx context.Context) error {
	assets, err := s.svcCtx.Store.ListExpiredUploads(ctx, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("list expired uploads: %w", err)
	}
	for _, candidate := range assets {
		lockedStore, unlock, err := s.svcCtx.Store.AcquireAssetLock(ctx, candidate.ID)
		if err != nil {
			return fmt.Errorf("lock expired upload %d: %w", candidate.ID, err)
		}
		asset, getErr := lockedStore.GetAsset(ctx, candidate.ID)
		if getErr == nil &&
			asset.Status == store.StatusCreated &&
			asset.ExpiresAt > 0 &&
			asset.ExpiresAt <= time.Now().UnixMilli() {
			asset.Status = store.StatusExpired
			if updateErr := lockedStore.UpdateAsset(ctx, asset); updateErr != nil {
				unlock()
				return fmt.Errorf("expire upload %d: %w", candidate.ID, updateErr)
			}
			s.deleteUploadObject(asset)
		}
		unlock()
		if getErr != nil && !errors.Is(getErr, store.ErrNotFound) {
			return fmt.Errorf("reload expired upload %d: %w", candidate.ID, getErr)
		}
	}
	return nil
}

func (s *MediaServer) getUpload(
	ctx context.Context,
	assetStore store.AssetStore,
	uploadID int64,
) (*store.Asset, error) {
	asset, err := assetStore.GetAsset(ctx, uploadID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, errUploadNotFound
		}
		return nil, fmt.Errorf("get asset: %w", err)
	}
	return asset, nil
}

func (s *MediaServer) deleteUploadObject(asset *store.Asset) {
	deleteCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.svcCtx.ObjectStore.DeleteObject(deleteCtx, uploadObjectKey(asset))
}

func (s *MediaServer) storageBackend() string {
	if backend := strings.TrimSpace(s.svcCtx.Cfg.ObjectStore.Backend); backend != "" {
		return backend
	}
	return "s3"
}

func uploadObjectKey(asset *store.Asset) string {
	if asset.StagingKey != "" {
		return asset.StagingKey
	}
	return asset.PublishedKey
}

func normalizeContentType(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errContentTypeRequired
	}
	trimmed := strings.TrimSpace(value)
	mediaType, params, err := mime.ParseMediaType(trimmed)
	mediaType = strings.ToLower(mediaType)
	if err != nil || mediaType == "" || len(params) != 0 || trimmed != mediaType {
		return "", errContentTypeInvalid
	}
	return mediaType, nil
}
