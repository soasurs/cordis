package server

import (
	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	mediav1 "github.com/soasurs/cordis/gen/media/v1"
)

func uploadStatusToAPI(status mediav1.AssetStatus) apiv1.UploadStatus {
	switch status {
	case mediav1.AssetStatus_ASSET_STATUS_CREATED:
		return apiv1.UploadStatus_UPLOAD_STATUS_CREATED
	case mediav1.AssetStatus_ASSET_STATUS_COMPLETING:
		return apiv1.UploadStatus_UPLOAD_STATUS_COMPLETING
	case mediav1.AssetStatus_ASSET_STATUS_READY:
		return apiv1.UploadStatus_UPLOAD_STATUS_READY
	case mediav1.AssetStatus_ASSET_STATUS_FAILED:
		return apiv1.UploadStatus_UPLOAD_STATUS_FAILED
	case mediav1.AssetStatus_ASSET_STATUS_ABORTED:
		return apiv1.UploadStatus_UPLOAD_STATUS_ABORTED
	case mediav1.AssetStatus_ASSET_STATUS_EXPIRED:
		return apiv1.UploadStatus_UPLOAD_STATUS_EXPIRED
	default:
		return apiv1.UploadStatus_UPLOAD_STATUS_UNSPECIFIED
	}
}
