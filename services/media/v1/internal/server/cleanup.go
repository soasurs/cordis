package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/soasurs/cordis/services/media/v1/internal/objectstore"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

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
