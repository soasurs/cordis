package server

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"testing"

	sn "github.com/bwmarrin/snowflake"
	"github.com/stretchr/testify/require"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/services/media/v1/config"
	"github.com/soasurs/cordis/services/media/v1/internal/processing"
	"github.com/soasurs/cordis/services/media/v1/internal/svc"
)

func testImageConstraints(maxSize int64, maxDim int32, maxPixels int64) config.ImageConstraintProfile {
	return config.ImageConstraintProfile{
		MaxSizeBytes: maxSize,
		MaxDimension: maxDim,
		MaxPixels:    maxPixels,
		AllowedContentTypes: []string{
			"image/jpeg",
			"image/png",
			"image/webp",
		},
	}
}

func newTestServer(t *testing.T) (*MediaServer, *fakeStore, *fakeObjectStore) {
	t.Helper()
	assetStore := newFakeStore()
	objStore := newFakeObjectStore()
	node, err := sn.NewNode(1)
	require.NoError(t, err)

	mediaConfig := config.MediaConfig{
		UploadSessionTTLSeconds:      1800,
		PresignedURLTTLSeconds:       900,
		MaxUploadSizeBytes:           524288000,
		MaxActiveUploadsPerUser:      5,
		ImageProcessingTimeoutMs:     30000,
		MaxConcurrentImageProcessing: 2,
		ImageConstraints: config.ImageConstraintsConfig{
			UserAvatar: testImageConstraints(10<<20, 4096, 4096*4096),
			GuildIcon:  testImageConstraints(10<<20, 4096, 4096*4096),
		},
		AttachmentImageInspection:    config.AttachmentImageInspectionProfile(testImageConstraints(10<<20, 4096, 4096*4096)),
		AttachmentAccessMode:         config.AttachmentAccessPublic,
		AttachmentDownloadTTLSeconds: 3600,
	}
	processor := processing.NewProcessor(objStore, objStore, mediaConfig)
	svcCtx := &svc.ServiceContext{
		Cfg: config.Config{
			ObjectStore: config.ObjectStoreConfig{
				Backend:                 "r2",
				AttachmentPublicBaseURL: "https://cdn.example.com",
			},
			Media: mediaConfig,
		},
		Store:                 assetStore,
		Snowflake:             node,
		PublicObjectStore:     objStore,
		StagingObjectStore:    objStore,
		AttachmentObjectStore: objStore,
		Processor:             processor,
	}
	return New(svcCtx), assetStore, objStore
}

func newCreateRequest(
	kind mediav1.AssetKind,
	expectedSize int64,
	contentType string,
) *mediav1.CreateUploadRequest {
	req := new(mediav1.CreateUploadRequest)
	req.SetActorUserId(1001)
	req.SetExpectedSize(expectedSize)
	req.SetContentType(contentType)
	switch kind {
	case mediav1.AssetKind_ASSET_KIND_USER_AVATAR:
		req.SetUserAvatar(new(mediav1.UserAvatarUploadPurpose))
	case mediav1.AssetKind_ASSET_KIND_GUILD_ICON:
		purpose := new(mediav1.GuildIconUploadPurpose)
		purpose.SetGuildId(2001)
		req.SetGuildIcon(purpose)
	case mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT:
		purpose := new(mediav1.MessageAttachmentUploadPurpose)
		purpose.SetChannelId(3001)
		purpose.SetFilename("report.pdf")
		req.SetMessageAttachment(purpose)
	}
	return req
}

func completeRequest(uploadID, actorUserID int64) *mediav1.CompleteUploadRequest {
	req := new(mediav1.CompleteUploadRequest)
	req.SetUploadId(uploadID)
	req.SetActorUserId(actorUserID)
	return req
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x % 255),
				G: uint8(y % 255),
				B: 100,
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func fmtID(id int64) string {
	return strconv.FormatInt(id, 10)
}
