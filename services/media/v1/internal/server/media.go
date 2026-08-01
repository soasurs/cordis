package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	neturl "net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/media/v1/config"
	"github.com/soasurs/cordis/services/media/v1/internal/objectstore"
	"github.com/soasurs/cordis/services/media/v1/internal/processing"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

const maxBatchAssetURLs = 1000

func (s *MediaServer) CreateUpload(
	ctx context.Context,
	req *mediav1.CreateUploadRequest,
) (*mediav1.CreateUploadResponse, error) {
	actorUserID := req.GetActorUserId()
	if actorUserID <= 0 {
		return nil, errActorUserIDRequired
	}
	kind, subjectID, filename, err := uploadPurpose(req, actorUserID)
	if err != nil {
		return nil, err
	}
	expectedSize := req.GetExpectedSize()
	if expectedSize <= 0 {
		return nil, errSizeRequired
	}
	if expectedSize > s.svcCtx.Cfg.Media.MaxUploadSize() {
		if kind.IsImage() {
			return nil, imageTooLargeError(kind)
		}
		return nil, errSizeExceeded
	}
	contentType, err := normalizeContentType(req.GetContentType())
	if err != nil {
		if kind.IsImage() {
			return nil, imageContentTypeInvalidError(kind)
		}
		return nil, err
	}
	if kind.IsImage() {
		constraints := imageConstraintsForKind(s.svcCtx.Cfg.Media, kind)
		if !constraints.AllowsContentType(contentType) {
			return nil, imageContentTypeInvalidError(kind)
		}
		if expectedSize > constraints.MaxSizeBytes {
			return nil, imageTooLargeError(kind)
		}
	}
	if kind == store.KindMessageAttachment {
		filename, err = validateAttachmentFilename(filename)
		if err != nil {
			return nil, err
		}
	}

	var requestHash []byte
	if req.HasIdempotencyKey() {
		if err := validateIdempotencyKey(req.GetIdempotencyKey(), s.svcCtx.Cfg.Idempotency.KeyLength()); err != nil {
			return nil, err
		}
		requestHash, err = createUploadRequestHash(
			actorUserID,
			kind,
			subjectID,
			expectedSize,
			contentType,
			filename,
		)
		if err != nil {
			return nil, err
		}
	}

	id := s.svcCtx.Snowflake.Generate().Int64()
	now := time.Now().UnixMilli()
	uploadTTL := s.svcCtx.Cfg.Media.UploadSessionTTL()
	presignedTTL := s.svcCtx.Cfg.Media.PresignedURLTTL()

	var created *store.Asset
	var idempotentReplay bool
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		if req.HasIdempotencyKey() {
			operation, err := uploadOperation(kind)
			if err != nil {
				return err
			}
			claim, err := txStore.ClaimMediaIdempotency(ctx, store.ClaimMediaIdempotencyParams{
				ActorUserID:    actorUserID,
				Operation:      operation,
				IdempotencyKey: req.GetIdempotencyKey(),
				RequestHash:    requestHash,
				AssetID:        id,
				CreatedAt:      now,
				ExpiresAt:      idempotencyExpiry(now, s.svcCtx.Cfg),
			})
			if err != nil {
				return err
			}
			if !bytes.Equal(claim.RequestHash, requestHash) {
				return idempotencyKeyReused()
			}
			if !claim.Claimed {
				existing, err := txStore.GetAsset(ctx, claim.AssetID)
				if err != nil {
					if errors.Is(err, store.ErrNotFound) {
						return errUploadNotFound
					}
					return err
				}
				created = existing
				idempotentReplay = true
				return nil
			}
		}

		asset, err := newUploadAsset(
			id,
			actorUserID,
			kind,
			subjectID,
			expectedSize,
			contentType,
			filename,
			now,
			uploadTTL,
			s.storageBackend(),
		)
		if err != nil {
			return err
		}
		if err := txStore.CreateAssetWithQuota(
			ctx,
			asset,
			s.svcCtx.Cfg.Media.MaxActiveUploads(),
		); err != nil {
			return err
		}
		created = asset
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrActiveUploadLimit) {
			return nil, errUploadLimit
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.Unavailable, "idempotency claim contention, retry request")
		}
		return nil, err
	}

	resp := new(mediav1.CreateUploadResponse)
	resp.SetUploadId(created.ID)
	resp.SetStatus(assetStatusToProto(created.Status))
	resp.SetIdempotentReplay(idempotentReplay)
	if idempotentReplay && created.Status != store.StatusCreated {
		return resp, nil
	}
	presignedRequest, err := s.uploadObjectStore(created).CreatePresignedPutRequest(
		ctx,
		uploadObjectKey(created),
		created.ContentType,
		created.ExpectedSize,
		presignedTTL,
	)
	if err != nil {
		return nil, fmt.Errorf("create presigned url: %w", err)
	}
	resp.SetPresignedUrl(presignedRequest.URL)
	resp.SetExpiresAt(now + presignedTTL*1000)
	resp.SetRequestHeaders(presignedRequest.RequestHeaders)
	return resp, nil
}

func newUploadAsset(
	id, actorUserID int64,
	kind store.Kind,
	subjectID int64,
	expectedSize int64,
	contentType, filename string,
	now, uploadTTL int64,
	storageBackend string,
) (*store.Asset, error) {
	var storageToken string
	if kind == store.KindMessageAttachment {
		var err error
		storageToken, err = newStorageToken()
		if err != nil {
			return nil, fmt.Errorf("generate attachment storage token: %w", err)
		}
	}
	asset := &store.Asset{
		ID:              id,
		CreatedByUserID: actorUserID,
		SubjectID:       subjectID,
		Kind:            kind,
		Status:          store.StatusCreated,
		StorageBackend:  storageBackend,
		ExpectedSize:    expectedSize,
		ContentType:     contentType,
		Filename:        filename,
		StorageToken:    storageToken,
		ExpiresAt:       now + uploadTTL*1000,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if kind.IsImage() {
		asset.StagingKey = fmt.Sprintf("staging/%d", id)
	} else {
		asset.PublishedKey = fmt.Sprintf(
			"attachments/%d/%d/%s/%s",
			subjectID,
			id,
			storageToken,
			filename,
		)
	}
	return asset, nil
}

func (s *MediaServer) CompleteUpload(
	ctx context.Context,
	req *mediav1.CompleteUploadRequest,
) (*mediav1.CompleteUploadResponse, error) {
	actorUserID := req.GetActorUserId()
	if actorUserID <= 0 {
		return nil, errActorUserIDRequired
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
	if actorUserID != asset.CreatedByUserID {
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
		return s.buildCompleteResponse(ctx, asset)
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
		asset.ActualSize = info.Size
	}

	if asset.Status == store.StatusCompleting {
		info, err := s.statUploadedObject(ctx, assetStore, asset)
		if err != nil {
			return nil, err
		}
		asset.ActualSize = info.Size
		if asset.Kind.IsImage() {
			return s.publishImage(ctx, assetStore, asset)
		}
		if asset.Kind == store.KindMessageAttachment {
			if err := s.inspectAttachmentImage(ctx, asset); err != nil {
				return nil, err
			}
		}
		asset.Status = store.StatusReady
		if err := assetStore.UpdateAsset(ctx, asset); err != nil {
			return nil, fmt.Errorf("update asset to ready: %w", err)
		}
		return s.buildCompleteResponse(ctx, asset)
	}
	return nil, errNotUploaded
}

func (s *MediaServer) statUploadedObject(
	ctx context.Context,
	assetStore store.AssetStore,
	asset *store.Asset,
) (*objectstore.ObjectInfo, error) {
	info, err := s.uploadObjectStore(asset).StatObject(ctx, uploadObjectKey(asset))
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

func (s *MediaServer) publishImage(
	ctx context.Context,
	assetStore store.AssetStore,
	asset *store.Asset,
) (*mediav1.CompleteUploadResponse, error) {
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
		if mapped := mapAvatarProcessingError(asset.Kind, err); mapped != nil {
			return nil, mapped
		}
		return nil, errProcessingFailed
	}

	asset.PublishedKey = result.PublishedKey
	asset.Width = result.Width
	asset.Height = result.Height
	asset.Blurhash = result.Blurhash
	asset.Status = store.StatusReady
	asset.ErrorMessage = ""
	if err := assetStore.UpdateAsset(ctx, asset); err != nil {
		return nil, fmt.Errorf("update asset to ready: %w", err)
	}
	s.deleteUploadObject(asset)
	return s.buildCompleteResponse(ctx, asset)
}

func (s *MediaServer) inspectAttachmentImage(ctx context.Context, asset *store.Asset) error {
	constraints := s.svcCtx.Cfg.Media.AttachmentImageInspectionConstraints()
	if !constraints.AllowsContentType(asset.ContentType) {
		return nil
	}
	inspected, err := s.svcCtx.Processor.InspectAttachmentImage(
		ctx,
		asset.ContentType,
		asset.ActualSize,
		func(ctx context.Context) (io.ReadCloser, int64, error) {
			reader, info, err := s.uploadObjectStore(asset).GetObject(ctx, uploadObjectKey(asset))
			if err != nil {
				return nil, 0, err
			}
			return reader, info.Size, nil
		},
	)
	if err != nil {
		if errors.Is(err, objectstore.ErrObjectNotFound) {
			return errNotUploaded
		}
		return errObjectStoreDown
	}
	asset.Width = inspected.Width
	asset.Height = inspected.Height
	asset.Blurhash = inspected.Blurhash
	return nil
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
	ctx context.Context,
	asset *store.Asset,
) (*mediav1.CompleteUploadResponse, error) {
	resp := new(mediav1.CompleteUploadResponse)
	resp.SetAssetId(asset.ID)

	metadata := new(mediav1.AssetMetadata)
	metadata.SetSize(asset.ActualSize)
	metadata.SetContentType(asset.ContentType)
	metadata.SetWidth(asset.Width)
	metadata.SetHeight(asset.Height)
	metadata.SetBlurhash(asset.Blurhash)
	metadata.SetFilename(asset.Filename)
	if asset.Kind == store.KindMessageAttachment {
		downloadURL, expiresAt, err := s.attachmentURL(ctx, asset)
		if err != nil {
			return nil, err
		}
		metadata.SetUrl(downloadURL)
		metadata.SetUrlExpiresAt(expiresAt)
	}
	resp.SetMetadata(metadata)
	return resp, nil
}

func (s *MediaServer) AbortUpload(
	ctx context.Context,
	req *mediav1.AbortUploadRequest,
) (*mediav1.AbortUploadResponse, error) {
	actorUserID := req.GetActorUserId()
	if actorUserID <= 0 {
		return nil, errActorUserIDRequired
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
	if actorUserID != asset.CreatedByUserID {
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
	value.SetCreatedByUserId(asset.CreatedByUserID)
	value.SetKind(kindToProto(asset.Kind))
	value.SetStatus(assetStatusToProto(asset.Status))
	value.SetStorageBackend(asset.StorageBackend)
	value.SetContentType(asset.ContentType)
	value.SetSize(asset.ActualSize)
	value.SetWidth(asset.Width)
	value.SetHeight(asset.Height)
	value.SetBlurhash(asset.Blurhash)
	value.SetCreatedAt(asset.CreatedAt)
	value.SetUpdatedAt(asset.UpdatedAt)
	value.SetSubjectId(asset.SubjectID)
	value.SetFilename(asset.Filename)
	if asset.Status == store.StatusReady && asset.Kind == store.KindMessageAttachment {
		downloadURL, expiresAt, err := s.attachmentURL(ctx, asset)
		if err != nil {
			return nil, err
		}
		value.SetUrl(downloadURL)
		value.SetUrlExpiresAt(expiresAt)
	}
	resp.SetAsset(value)
	return resp, nil
}

func (s *MediaServer) GetImageUploadConstraints(
	_ context.Context,
	req *mediav1.GetImageUploadConstraintsRequest,
) (*mediav1.GetImageUploadConstraintsResponse, error) {
	purpose, err := imagePurposeFromRequest(req)
	if err != nil {
		return nil, err
	}
	profile := s.svcCtx.Cfg.Media.ImageConstraintsFor(purpose)
	constraints := new(mediav1.ImageUploadConstraints)
	constraints.SetMaxFileSizeBytes(profile.MaxSizeBytes)
	constraints.SetMaxWidth(profile.MaxDimension)
	constraints.SetMaxHeight(profile.MaxDimension)
	constraints.SetMaxPixels(profile.MaxPixels)
	constraints.SetAllowedContentTypes(append([]string(nil), profile.AllowedContentTypes...))
	resp := new(mediav1.GetImageUploadConstraintsResponse)
	resp.SetConstraints(constraints)
	return resp, nil
}

func (s *MediaServer) BatchGetAssetURLs(
	ctx context.Context,
	req *mediav1.BatchGetAssetURLsRequest,
) (*mediav1.BatchGetAssetURLsResponse, error) {
	ids := req.GetAssetIds()
	if len(ids) > maxBatchAssetURLs {
		return nil, errTooManyAssets
	}
	uniqueIDs := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, errAssetNotFound
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	assets, err := s.svcCtx.Store.ListAssets(ctx, uniqueIDs)
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	assetsByID := make(map[int64]*store.Asset, len(assets))
	for _, asset := range assets {
		assetsByID[asset.ID] = asset
	}
	values := make([]*mediav1.AssetURL, 0, len(uniqueIDs))
	for _, id := range uniqueIDs {
		asset := assetsByID[id]
		if asset == nil {
			return nil, errAssetNotFound
		}
		if asset.Status != store.StatusReady {
			return nil, errAssetNotReady
		}
		if asset.Kind != store.KindMessageAttachment || asset.PublishedKey == "" {
			return nil, errAssetNotDownloadable
		}
		downloadURL, expiresAt, err := s.attachmentURL(ctx, asset)
		if err != nil {
			return nil, err
		}
		value := new(mediav1.AssetURL)
		value.SetAssetId(id)
		value.SetUrl(downloadURL)
		value.SetExpiresAt(expiresAt)
		values = append(values, value)
	}
	resp := new(mediav1.BatchGetAssetURLsResponse)
	resp.SetAssets(values)
	return resp, nil
}

func (s *MediaServer) attachmentURL(
	ctx context.Context,
	asset *store.Asset,
) (string, int64, error) {
	if asset.Status != store.StatusReady {
		return "", 0, errAssetNotReady
	}
	if asset.Kind != store.KindMessageAttachment || asset.PublishedKey == "" {
		return "", 0, errAssetNotDownloadable
	}
	if s.svcCtx.Cfg.Media.AttachmentAccess() == config.AttachmentAccessPublic {
		baseURL, err := neturl.Parse(s.svcCtx.Cfg.ObjectStore.AttachmentPublicBaseURL)
		if err != nil {
			return "", 0, fmt.Errorf("build public attachment url: %w", err)
		}
		baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/" + asset.PublishedKey
		baseURL.RawPath = ""
		return baseURL.String(), 0, nil
	}
	expiresIn := s.svcCtx.Cfg.Media.AttachmentDownloadTTL()
	value, err := s.svcCtx.AttachmentObjectStore.CreatePresignedGetURL(
		ctx,
		asset.PublishedKey,
		expiresIn,
	)
	if err != nil {
		return "", 0, fmt.Errorf("create presigned get url: %w", err)
	}
	return value, time.Now().UnixMilli() + expiresIn*1000, nil
}

func uploadPurpose(
	req *mediav1.CreateUploadRequest,
	actorUserID int64,
) (store.Kind, int64, string, error) {
	switch {
	case req.HasUserAvatar():
		return store.KindUserAvatar, actorUserID, "", nil
	case req.HasGuildIcon():
		guildID := req.GetGuildIcon().GetGuildId()
		if guildID <= 0 {
			return "", 0, "", errGuildIDRequired
		}
		return store.KindGuildIcon, guildID, "", nil
	case req.HasMessageAttachment():
		purpose := req.GetMessageAttachment()
		channelID := purpose.GetChannelId()
		if channelID <= 0 {
			return "", 0, "", errChannelIDRequired
		}
		return store.KindMessageAttachment, channelID, purpose.GetFilename(), nil
	default:
		return "", 0, "", errPurposeRequired
	}
}

func imagePurposeFromRequest(req *mediav1.GetImageUploadConstraintsRequest) (config.ImagePurpose, error) {
	switch {
	case req.HasUserAvatar():
		return config.ImagePurposeUserAvatar, nil
	case req.HasGuildIcon():
		return config.ImagePurposeGuildIcon, nil
	default:
		return "", errPurposeRequired
	}
}

func imageConstraintsForKind(cfg config.MediaConfig, kind store.Kind) config.ImageConstraintProfile {
	switch kind {
	case store.KindUserAvatar:
		return cfg.ImageConstraintsFor(config.ImagePurposeUserAvatar)
	case store.KindGuildIcon:
		return cfg.ImageConstraintsFor(config.ImagePurposeGuildIcon)
	default:
		return cfg.ImageConstraintsFor("")
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
	_ = s.uploadObjectStore(asset).DeleteObject(deleteCtx, uploadObjectKey(asset))
}

func (s *MediaServer) uploadObjectStore(asset *store.Asset) objectstore.ObjectStore {
	if asset.Kind == store.KindMessageAttachment {
		return s.svcCtx.AttachmentObjectStore
	}
	return s.svcCtx.StagingObjectStore
}

func (s *MediaServer) storageBackend() string {
	if backend := strings.TrimSpace(s.svcCtx.Cfg.ObjectStore.Backend); backend != "" {
		return backend
	}
	return "s3"
}

func imageTooLargeError(kind store.Kind) error {
	if kind == store.KindUserAvatar {
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaAvatarFileTooLarge,
			"avatar file is too large",
		)
	}
	return errSizeExceeded
}

func imageContentTypeInvalidError(kind store.Kind) error {
	if kind == store.KindUserAvatar {
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaAvatarContentTypeInvalid,
			"avatar content type is invalid",
		)
	}
	return errContentTypeInvalid
}

func mapAvatarProcessingError(kind store.Kind, err error) error {
	if kind != store.KindUserAvatar {
		return nil
	}
	switch {
	case errors.Is(err, processing.ErrImageTooLarge):
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaAvatarFileTooLarge,
			"avatar file is too large",
		)
	case errors.Is(err, processing.ErrImageContentTypeInvalid):
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaAvatarContentTypeInvalid,
			"avatar content type is invalid",
		)
	case errors.Is(err, processing.ErrImageDimensionsExceeded):
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaAvatarDimensionsExceeded,
			"avatar dimensions exceed limit",
		)
	case errors.Is(err, processing.ErrImagePixelsExceeded):
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaAvatarPixelsExceeded,
			"avatar pixel count exceeds limit",
		)
	default:
		return nil
	}
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

func validateAttachmentFilename(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) ||
		len(value) > 255 || value == "." || value == ".." ||
		strings.ContainsAny(value, `/\`) {
		return "", errFilenameInvalid
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", errFilenameInvalid
		}
	}
	return value, nil
}

func newStorageToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}
