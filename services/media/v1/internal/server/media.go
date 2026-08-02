package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/services/media/v1/internal/objectstore"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

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
