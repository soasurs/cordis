package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

func (s *userServer) GetAvatarUploadConstraints(
	ctx context.Context,
	_ *userv1.GetAvatarUploadConstraintsRequest,
) (*userv1.GetAvatarUploadConstraintsResponse, error) {
	mediaReq := new(mediav1.GetImageUploadConstraintsRequest)
	mediaReq.SetUserAvatar(new(mediav1.UserAvatarUploadPurpose))
	mediaResp, err := s.svcCtx.MediaClient.GetImageUploadConstraints(ctx, mediaReq)
	if err != nil {
		return nil, err
	}
	mediaConstraints := mediaResp.GetConstraints()
	constraints := new(userv1.AvatarUploadConstraints)
	constraints.SetMaxFileSizeBytes(mediaConstraints.GetMaxFileSizeBytes())
	constraints.SetMaxWidth(mediaConstraints.GetMaxWidth())
	constraints.SetMaxHeight(mediaConstraints.GetMaxHeight())
	constraints.SetMaxPixels(mediaConstraints.GetMaxPixels())
	constraints.SetAllowedContentTypes(append([]string(nil), mediaConstraints.GetAllowedContentTypes()...))
	resp := new(userv1.GetAvatarUploadConstraintsResponse)
	resp.SetConstraints(constraints)
	return resp, nil
}

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
	resp.SetRequestHeaders(mediaResp.GetRequestHeaders())
	return resp, nil
}

func (s *userServer) CompleteAvatarUpload(
	ctx context.Context,
	req *userv1.CompleteAvatarUploadRequest,
) (*userv1.CompleteAvatarUploadResponse, error) {
	assetID, err := s.mountAvatarAsset(ctx, req.GetUserId(), req.GetUploadId())
	if err != nil {
		return nil, err
	}
	profile, err := s.svcCtx.Store.UpdateUserAvatar(ctx, req.GetUserId(), assetID)
	if err != nil {
		return nil, mapStoreError(err)
	}
	s.publishUserProfileUpdated(ctx, profile)
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

// mountAvatarAsset validates ownership and completes an unpublished upload when
// needed, returning the ready asset ID to associate with the profile.
func (s *userServer) mountAvatarAsset(ctx context.Context, userID, assetID int64) (int64, error) {
	asset, err := s.getAvatarUpload(ctx, userID, assetID)
	if err != nil {
		return 0, err
	}
	switch asset.GetStatus() {
	case mediav1.AssetStatus_ASSET_STATUS_READY:
		return asset.GetId(), nil
	case mediav1.AssetStatus_ASSET_STATUS_CREATED, mediav1.AssetStatus_ASSET_STATUS_COMPLETING:
		mediaReq := new(mediav1.CompleteUploadRequest)
		mediaReq.SetActorUserId(userID)
		mediaReq.SetUploadId(assetID)
		mediaResp, err := s.svcCtx.MediaClient.CompleteUpload(ctx, mediaReq)
		if err != nil {
			return 0, err
		}
		if mediaResp.GetAssetId() != asset.GetId() {
			return 0, status.Error(codes.Internal, "media returned an unexpected asset id")
		}
		return mediaResp.GetAssetId(), nil
	default:
		return 0, status.Error(codes.FailedPrecondition, "avatar asset is not ready to mount")
	}
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
