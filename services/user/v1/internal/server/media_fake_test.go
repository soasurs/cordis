package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
)

type fakeMediaClient struct {
	mediav1.MediaServiceClient
	asset           *mediav1.Asset
	createRequest   *mediav1.CreateUploadRequest
	completeRequest *mediav1.CompleteUploadRequest
	abortRequest    *mediav1.AbortUploadRequest
}

func (f *fakeMediaClient) CreateUpload(
	_ context.Context,
	req *mediav1.CreateUploadRequest,
	_ ...grpc.CallOption,
) (*mediav1.CreateUploadResponse, error) {
	f.createRequest = req
	resp := new(mediav1.CreateUploadResponse)
	resp.SetUploadId(7001)
	resp.SetPresignedUrl("https://upload.example/7001")
	resp.SetExpiresAt(9001)
	resp.SetRequestHeaders(map[string]string{"Content-Type": "image/png"})
	resp.SetStatus(mediav1.AssetStatus_ASSET_STATUS_CREATED)
	resp.SetIdempotentReplay(false)
	return resp, nil
}

func (f *fakeMediaClient) GetAsset(
	_ context.Context,
	_ *mediav1.GetAssetRequest,
	_ ...grpc.CallOption,
) (*mediav1.GetAssetResponse, error) {
	resp := new(mediav1.GetAssetResponse)
	resp.SetAsset(f.asset)
	return resp, nil
}

func (f *fakeMediaClient) GetImageUploadConstraints(
	_ context.Context,
	req *mediav1.GetImageUploadConstraintsRequest,
	_ ...grpc.CallOption,
) (*mediav1.GetImageUploadConstraintsResponse, error) {
	if !req.HasUserAvatar() && !req.HasGuildIcon() {
		return nil, status.Error(codes.InvalidArgument, "purpose is required")
	}
	constraints := new(mediav1.ImageUploadConstraints)
	constraints.SetMaxFileSizeBytes(10485760)
	constraints.SetMaxWidth(4096)
	constraints.SetMaxHeight(4096)
	constraints.SetMaxPixels(16777216)
	constraints.SetAllowedContentTypes([]string{"image/jpeg", "image/png", "image/webp"})
	resp := new(mediav1.GetImageUploadConstraintsResponse)
	resp.SetConstraints(constraints)
	return resp, nil
}

func (f *fakeMediaClient) CompleteUpload(
	_ context.Context,
	req *mediav1.CompleteUploadRequest,
	_ ...grpc.CallOption,
) (*mediav1.CompleteUploadResponse, error) {
	f.completeRequest = req
	resp := new(mediav1.CompleteUploadResponse)
	resp.SetAssetId(req.GetUploadId())
	return resp, nil
}

func (f *fakeMediaClient) AbortUpload(
	_ context.Context,
	req *mediav1.AbortUploadRequest,
	_ ...grpc.CallOption,
) (*mediav1.AbortUploadResponse, error) {
	f.abortRequest = req
	return new(mediav1.AbortUploadResponse), nil
}
