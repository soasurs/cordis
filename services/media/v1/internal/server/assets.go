package server

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"strings"
	"time"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/services/media/v1/config"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

const maxBatchAssetURLs = 1000

// GetAsset returns one asset, adding a download URL for ready message
// attachments.
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

// BatchGetAssetURLs returns download URLs for up to maxBatchAssetURLs ready
// message attachments, deduplicating the requested IDs.
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
