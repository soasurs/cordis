package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

func (s *messageServer) CreateAttachmentUpload(
	ctx context.Context,
	req *messagev1.CreateAttachmentUploadRequest,
) (*messagev1.CreateAttachmentUploadResponse, error) {
	if _, err := s.requireChannelPermission(
		ctx,
		req.GetChannelId(),
		req.GetActorUserId(),
		permissionSendMessages,
	); err != nil {
		return nil, err
	}
	mediaReq := new(mediav1.CreateUploadRequest)
	mediaReq.SetActorUserId(req.GetActorUserId())
	mediaReq.SetExpectedSize(req.GetExpectedSize())
	mediaReq.SetContentType(req.GetContentType())
	purpose := new(mediav1.MessageAttachmentUploadPurpose)
	purpose.SetChannelId(req.GetChannelId())
	purpose.SetFilename(req.GetFilename())
	mediaReq.SetMessageAttachment(purpose)
	mediaResp, err := s.svcCtx.MediaClient.CreateUpload(ctx, mediaReq)
	if err != nil {
		return nil, err
	}
	resp := new(messagev1.CreateAttachmentUploadResponse)
	resp.SetUploadId(mediaResp.GetUploadId())
	resp.SetPresignedUrl(mediaResp.GetPresignedUrl())
	resp.SetExpiresAt(mediaResp.GetExpiresAt())
	resp.SetRequestHeaders(mediaResp.GetRequestHeaders())
	return resp, nil
}

func (s *messageServer) CompleteAttachmentUpload(
	ctx context.Context,
	req *messagev1.CompleteAttachmentUploadRequest,
) (*messagev1.CompleteAttachmentUploadResponse, error) {
	if _, err := s.requireChannelPermission(
		ctx,
		req.GetChannelId(),
		req.GetActorUserId(),
		permissionSendMessages,
	); err != nil {
		return nil, err
	}
	asset, err := s.getAttachmentAsset(
		ctx,
		req.GetChannelId(),
		req.GetActorUserId(),
		req.GetUploadId(),
		false,
	)
	if err != nil {
		return nil, err
	}
	mediaReq := new(mediav1.CompleteUploadRequest)
	mediaReq.SetActorUserId(req.GetActorUserId())
	mediaReq.SetUploadId(req.GetUploadId())
	mediaResp, err := s.svcCtx.MediaClient.CompleteUpload(ctx, mediaReq)
	if err != nil {
		return nil, err
	}
	if mediaResp.GetAssetId() != asset.GetId() {
		return nil, status.Error(codes.Internal, "media returned an unexpected asset id")
	}
	attachment := new(messagev1.Attachment)
	attachment.SetAssetId(mediaResp.GetAssetId())
	attachment.SetFilename(mediaResp.GetMetadata().GetFilename())
	attachment.SetSize(mediaResp.GetMetadata().GetSize())
	attachment.SetContentType(mediaResp.GetMetadata().GetContentType())
	attachment.SetWidth(mediaResp.GetMetadata().GetWidth())
	attachment.SetHeight(mediaResp.GetMetadata().GetHeight())
	attachment.SetUrl(mediaResp.GetMetadata().GetUrl())
	attachment.SetUrlExpiresAt(mediaResp.GetMetadata().GetUrlExpiresAt())
	resp := new(messagev1.CompleteAttachmentUploadResponse)
	resp.SetAttachment(attachment)
	return resp, nil
}

func (s *messageServer) AbortAttachmentUpload(
	ctx context.Context,
	req *messagev1.AbortAttachmentUploadRequest,
) (*messagev1.AbortAttachmentUploadResponse, error) {
	if _, err := s.requireChannelPermission(
		ctx,
		req.GetChannelId(),
		req.GetActorUserId(),
		permissionSendMessages,
	); err != nil {
		return nil, err
	}
	if _, err := s.getAttachmentAsset(
		ctx,
		req.GetChannelId(),
		req.GetActorUserId(),
		req.GetUploadId(),
		false,
	); err != nil {
		return nil, err
	}
	mediaReq := new(mediav1.AbortUploadRequest)
	mediaReq.SetActorUserId(req.GetActorUserId())
	mediaReq.SetUploadId(req.GetUploadId())
	if _, err := s.svcCtx.MediaClient.AbortUpload(ctx, mediaReq); err != nil {
		return nil, err
	}
	return new(messagev1.AbortAttachmentUploadResponse), nil
}

func (s *messageServer) resolveAttachments(
	ctx context.Context,
	channelID, actorUserID int64,
	values []*messagev1.Attachment,
) ([]model.Attachment, error) {
	attachments := make([]model.Attachment, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if value == nil || value.GetAssetId() <= 0 {
			return nil, invalidRequest("attachment asset id is required")
		}
		if _, ok := seen[value.GetAssetId()]; ok {
			return nil, invalidRequest("attachment asset ids must be unique")
		}
		seen[value.GetAssetId()] = struct{}{}
		asset, err := s.getAttachmentAsset(
			ctx,
			channelID,
			actorUserID,
			value.GetAssetId(),
			true,
		)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, model.Attachment{
			AssetID:      asset.GetId(),
			Filename:     asset.GetFilename(),
			Size:         asset.GetSize(),
			ContentType:  asset.GetContentType(),
			Width:        asset.GetWidth(),
			Height:       asset.GetHeight(),
			URL:          asset.GetUrl(),
			URLExpiresAt: asset.GetUrlExpiresAt(),
		})
	}
	return attachments, nil
}

func (s *messageServer) getAttachmentAsset(
	ctx context.Context,
	channelID, actorUserID, assetID int64,
	requireReady bool,
) (*mediav1.Asset, error) {
	if channelID <= 0 {
		return nil, invalidRequest("channel id is required")
	}
	if actorUserID <= 0 {
		return nil, invalidRequest("actor user id is required")
	}
	if assetID <= 0 {
		return nil, invalidRequest("asset id is required")
	}
	mediaReq := new(mediav1.GetAssetRequest)
	mediaReq.SetAssetId(assetID)
	mediaResp, err := s.svcCtx.MediaClient.GetAsset(ctx, mediaReq)
	if err != nil {
		return nil, err
	}
	asset := mediaResp.GetAsset()
	if asset.GetKind() != mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT ||
		asset.GetCreatedByUserId() != actorUserID ||
		asset.GetSubjectId() != channelID {
		return nil, status.Error(codes.PermissionDenied, "asset is not this channel upload")
	}
	if requireReady && asset.GetStatus() != mediav1.AssetStatus_ASSET_STATUS_READY {
		return nil, status.Error(codes.FailedPrecondition, "attachment asset is not ready")
	}
	return asset, nil
}

func (s *messageServer) hydrateAttachmentURLs(
	ctx context.Context,
	messages ...*model.Message,
) error {
	ids := make([]int64, 0)
	seen := make(map[int64]struct{})
	for _, message := range messages {
		for _, attachment := range message.Attachments {
			if _, ok := seen[attachment.AssetID]; ok {
				continue
			}
			seen[attachment.AssetID] = struct{}{}
			ids = append(ids, attachment.AssetID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	req := new(mediav1.BatchGetAssetURLsRequest)
	req.SetAssetIds(ids)
	resp, err := s.svcCtx.MediaClient.BatchGetAssetURLs(ctx, req)
	if err != nil {
		return err
	}
	urls := make(map[int64]*mediav1.AssetURL, len(resp.GetAssets()))
	for _, value := range resp.GetAssets() {
		if value == nil || value.GetAssetId() <= 0 || value.GetUrl() == "" {
			return status.Error(codes.Internal, "media returned an invalid attachment url")
		}
		if _, ok := seen[value.GetAssetId()]; !ok {
			return status.Error(codes.Internal, "media returned an unexpected attachment url")
		}
		if _, ok := urls[value.GetAssetId()]; ok {
			return status.Error(codes.Internal, "media returned a duplicate attachment url")
		}
		urls[value.GetAssetId()] = value
	}
	if len(urls) != len(ids) {
		return status.Error(codes.Internal, "media did not return all attachment urls")
	}
	for _, message := range messages {
		for index := range message.Attachments {
			value := urls[message.Attachments[index].AssetID]
			message.Attachments[index].URL = value.GetUrl()
			message.Attachments[index].URLExpiresAt = value.GetExpiresAt()
		}
	}
	return nil
}

func copyAttachmentURLs(target, source []model.Attachment) {
	urls := make(map[int64]model.Attachment, len(source))
	for _, attachment := range source {
		urls[attachment.AssetID] = attachment
	}
	for index := range target {
		sourceAttachment, ok := urls[target[index].AssetID]
		if !ok {
			continue
		}
		target[index].URL = sourceAttachment.URL
		target[index].URLExpiresAt = sourceAttachment.URLExpiresAt
	}
}
