package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	mediav1 "github.com/soasurs/cordis/gen/media/v1"
)

func TestUploadStatusToAPI(t *testing.T) {
	tests := []struct {
		name     string
		internal mediav1.AssetStatus
		public   apiv1.UploadStatus
	}{
		{
			name:     "unspecified",
			internal: mediav1.AssetStatus_ASSET_STATUS_UNSPECIFIED,
			public:   apiv1.UploadStatus_UPLOAD_STATUS_UNSPECIFIED,
		},
		{
			name:     "created",
			internal: mediav1.AssetStatus_ASSET_STATUS_CREATED,
			public:   apiv1.UploadStatus_UPLOAD_STATUS_CREATED,
		},
		{
			name:     "completing",
			internal: mediav1.AssetStatus_ASSET_STATUS_COMPLETING,
			public:   apiv1.UploadStatus_UPLOAD_STATUS_COMPLETING,
		},
		{
			name:     "ready",
			internal: mediav1.AssetStatus_ASSET_STATUS_READY,
			public:   apiv1.UploadStatus_UPLOAD_STATUS_READY,
		},
		{
			name:     "failed",
			internal: mediav1.AssetStatus_ASSET_STATUS_FAILED,
			public:   apiv1.UploadStatus_UPLOAD_STATUS_FAILED,
		},
		{
			name:     "aborted",
			internal: mediav1.AssetStatus_ASSET_STATUS_ABORTED,
			public:   apiv1.UploadStatus_UPLOAD_STATUS_ABORTED,
		},
		{
			name:     "expired",
			internal: mediav1.AssetStatus_ASSET_STATUS_EXPIRED,
			public:   apiv1.UploadStatus_UPLOAD_STATUS_EXPIRED,
		},
		{
			name:     "unknown",
			internal: mediav1.AssetStatus(99),
			public:   apiv1.UploadStatus_UPLOAD_STATUS_UNSPECIFIED,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.public, uploadStatusToAPI(test.internal))
		})
	}
}
