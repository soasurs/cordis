package server

import (
	"context"
	"strings"

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
	mediaReq.SetMessageAttachment(purpose)
	mediaResp, err := s.svcCtx.MediaClient.CreateUpload(ctx, mediaReq)
	if err != nil {
		return nil, err
	}
	resp := new(messagev1.CreateAttachmentUploadResponse)
	resp.SetUploadId(mediaResp.GetUploadId())
	resp.SetPresignedUrl(mediaResp.GetPresignedUrl())
	resp.SetExpiresAt(mediaResp.GetExpiresAt())
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
	if strings.TrimSpace(req.GetFilename()) == "" {
		return nil, invalidRequest("filename is required")
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
	attachment.SetFilename(req.GetFilename())
	attachment.SetSize(mediaResp.GetMetadata().GetSize())
	attachment.SetContentType(mediaResp.GetMetadata().GetContentType())
	attachment.SetWidth(mediaResp.GetMetadata().GetWidth())
	attachment.SetHeight(mediaResp.GetMetadata().GetHeight())
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

func (s *messageServer) GetAttachmentDownloadURL(
	ctx context.Context,
	req *messagev1.GetAttachmentDownloadURLRequest,
) (*messagev1.GetAttachmentDownloadURLResponse, error) {
	message, err := s.svcCtx.Store.GetMessage(ctx, req.GetMessageId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	if _, err := s.requireChannelPermission(
		ctx,
		message.ChannelID,
		req.GetUserId(),
		permissionViewChannel,
	); err != nil {
		return nil, err
	}
	found := false
	for _, attachment := range message.Attachments {
		if attachment.AssetID == req.GetAssetId() {
			found = true
			break
		}
	}
	if !found {
		return nil, notFound()
	}
	mediaReq := new(mediav1.GetAssetDownloadURLRequest)
	mediaReq.SetAssetId(req.GetAssetId())
	mediaResp, err := s.svcCtx.MediaClient.GetAssetDownloadURL(ctx, mediaReq)
	if err != nil {
		return nil, err
	}
	resp := new(messagev1.GetAttachmentDownloadURLResponse)
	resp.SetUrl(mediaResp.GetUrl())
	resp.SetExpiresAt(mediaResp.GetExpiresAt())
	return resp, nil
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
		if strings.TrimSpace(value.GetFilename()) == "" {
			return nil, invalidRequest("attachment filename is required")
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
			AssetID:     asset.GetId(),
			Filename:    value.GetFilename(),
			Size:        asset.GetSize(),
			ContentType: asset.GetContentType(),
			Width:       asset.GetWidth(),
			Height:      asset.GetHeight(),
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
