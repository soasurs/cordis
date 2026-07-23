package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

func (s *userServer) CreateAvatarUpload(
	ctx context.Context,
	req *userv1.CreateAvatarUploadRequest,
) (*userv1.CreateAvatarUploadResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, errUserIDRequired
	}
	mediaReq := new(mediav1.CreateUploadRequest)
	mediaReq.SetActorUserId(req.GetUserId())
	mediaReq.SetExpectedSize(req.GetExpectedSize())
	mediaReq.SetContentType(req.GetContentType())
	mediaReq.SetUserAvatar(new(mediav1.UserAvatarUploadPurpose))
	mediaResp, err := s.svcCtx.MediaClient.CreateUpload(ctx, mediaReq)
	if err != nil {
		return nil, err
	}
	resp := new(userv1.CreateAvatarUploadResponse)
	resp.SetUploadId(mediaResp.GetUploadId())
	resp.SetPresignedUrl(mediaResp.GetPresignedUrl())
	resp.SetExpiresAt(mediaResp.GetExpiresAt())
	return resp, nil
}

func (s *userServer) CompleteAvatarUpload(
	ctx context.Context,
	req *userv1.CompleteAvatarUploadRequest,
) (*userv1.CompleteAvatarUploadResponse, error) {
	asset, err := s.getAvatarUpload(ctx, req.GetUserId(), req.GetUploadId())
	if err != nil {
		return nil, err
	}
	mediaReq := new(mediav1.CompleteUploadRequest)
	mediaReq.SetActorUserId(req.GetUserId())
	mediaReq.SetUploadId(req.GetUploadId())
	mediaResp, err := s.svcCtx.MediaClient.CompleteUpload(ctx, mediaReq)
	if err != nil {
		return nil, err
	}
	if mediaResp.GetAssetId() != asset.GetId() {
		return nil, status.Error(codes.Internal, "media returned an unexpected asset id")
	}
	profile, err := s.svcCtx.Store.UpdateUserAvatar(ctx, req.GetUserId(), mediaResp.GetAssetId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(userv1.CompleteAvatarUploadResponse)
	resp.SetProfile(userProfileToProto(profile))
	return resp, nil
}

func (s *userServer) AbortAvatarUpload(
	ctx context.Context,
	req *userv1.AbortAvatarUploadRequest,
) (*userv1.AbortAvatarUploadResponse, error) {
	if _, err := s.getAvatarUpload(ctx, req.GetUserId(), req.GetUploadId()); err != nil {
		return nil, err
	}
	mediaReq := new(mediav1.AbortUploadRequest)
	mediaReq.SetActorUserId(req.GetUserId())
	mediaReq.SetUploadId(req.GetUploadId())
	if _, err := s.svcCtx.MediaClient.AbortUpload(ctx, mediaReq); err != nil {
		return nil, err
	}
	return new(userv1.AbortAvatarUploadResponse), nil
}

func (s *userServer) getAvatarUpload(
	ctx context.Context,
	userID, uploadID int64,
) (*mediav1.Asset, error) {
	if userID <= 0 {
		return nil, errUserIDRequired
	}
	if uploadID <= 0 {
		return nil, status.Error(codes.InvalidArgument, "upload id is required")
	}
	mediaReq := new(mediav1.GetAssetRequest)
	mediaReq.SetAssetId(uploadID)
	mediaResp, err := s.svcCtx.MediaClient.GetAsset(ctx, mediaReq)
	if err != nil {
		return nil, err
	}
	asset := mediaResp.GetAsset()
	if asset.GetKind() != mediav1.AssetKind_ASSET_KIND_USER_AVATAR ||
		asset.GetCreatedByUserId() != userID ||
		asset.GetSubjectId() != userID {
		return nil, status.Error(codes.PermissionDenied, "upload is not this user's avatar")
	}
	return asset, nil
}
