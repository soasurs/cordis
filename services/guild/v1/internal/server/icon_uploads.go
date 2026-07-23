package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func (s *guildServer) CreateGuildIconUpload(
	ctx context.Context,
	req *guildv1.CreateGuildIconUploadRequest,
) (*guildv1.CreateGuildIconUploadResponse, error) {
	if err := s.requireManageGuild(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	mediaReq := new(mediav1.CreateUploadRequest)
	mediaReq.SetActorUserId(req.GetActorUserId())
	mediaReq.SetExpectedSize(req.GetExpectedSize())
	mediaReq.SetContentType(req.GetContentType())
	purpose := new(mediav1.GuildIconUploadPurpose)
	purpose.SetGuildId(req.GetGuildId())
	mediaReq.SetGuildIcon(purpose)
	mediaResp, err := s.svcCtx.MediaClient.CreateUpload(ctx, mediaReq)
	if err != nil {
		return nil, err
	}
	resp := new(guildv1.CreateGuildIconUploadResponse)
	resp.SetUploadId(mediaResp.GetUploadId())
	resp.SetPresignedUrl(mediaResp.GetPresignedUrl())
	resp.SetExpiresAt(mediaResp.GetExpiresAt())
	resp.SetRequestHeaders(mediaResp.GetRequestHeaders())
	return resp, nil
}

func (s *guildServer) CompleteGuildIconUpload(
	ctx context.Context,
	req *guildv1.CompleteGuildIconUploadRequest,
) (*guildv1.CompleteGuildIconUploadResponse, error) {
	asset, err := s.getGuildIconUpload(
		ctx,
		req.GetGuildId(),
		req.GetActorUserId(),
		req.GetUploadId(),
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

	var updated *model.Guild
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		authority, err := loadMemberAuthority(
			ctx,
			txStore,
			req.GetGuildId(),
			req.GetActorUserId(),
		)
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageGuild) {
			return permissionDenied()
		}
		updated, err = txStore.UpdateGuildIcon(ctx, req.GetGuildId(), mediaResp.GetAssetId())
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newGuildUpdatedEvent(updated, s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.CompleteGuildIconUploadResponse)
	resp.SetGuild(guildToProto(updated))
	return resp, nil
}

func (s *guildServer) AbortGuildIconUpload(
	ctx context.Context,
	req *guildv1.AbortGuildIconUploadRequest,
) (*guildv1.AbortGuildIconUploadResponse, error) {
	if err := s.requireManageGuild(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if _, err := s.getGuildIconUpload(
		ctx,
		req.GetGuildId(),
		req.GetActorUserId(),
		req.GetUploadId(),
	); err != nil {
		return nil, err
	}
	mediaReq := new(mediav1.AbortUploadRequest)
	mediaReq.SetActorUserId(req.GetActorUserId())
	mediaReq.SetUploadId(req.GetUploadId())
	if _, err := s.svcCtx.MediaClient.AbortUpload(ctx, mediaReq); err != nil {
		return nil, err
	}
	return new(guildv1.AbortGuildIconUploadResponse), nil
}

func (s *guildServer) requireManageGuild(ctx context.Context, guildID, actorUserID int64) error {
	if guildID <= 0 {
		return invalidRequest("guild id is required")
	}
	if actorUserID <= 0 {
		return invalidRequest("actor user id is required")
	}
	return s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		authority, err := loadMemberAuthority(ctx, txStore, guildID, actorUserID)
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageGuild) {
			return permissionDenied()
		}
		return nil
	})
}

func (s *guildServer) getGuildIconUpload(
	ctx context.Context,
	guildID, actorUserID, uploadID int64,
) (*mediav1.Asset, error) {
	if uploadID <= 0 {
		return nil, invalidRequest("upload id is required")
	}
	mediaReq := new(mediav1.GetAssetRequest)
	mediaReq.SetAssetId(uploadID)
	mediaResp, err := s.svcCtx.MediaClient.GetAsset(ctx, mediaReq)
	if err != nil {
		return nil, err
	}
	asset := mediaResp.GetAsset()
	if asset.GetKind() != mediav1.AssetKind_ASSET_KIND_GUILD_ICON ||
		asset.GetCreatedByUserId() != actorUserID ||
		asset.GetSubjectId() != guildID {
		return nil, status.Error(codes.PermissionDenied, "upload is not this Guild's icon")
	}
	return asset, nil
}
