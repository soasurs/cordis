package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigValidateAttachmentAccess(t *testing.T) {
	base := Config{
		ObjectStore: ObjectStoreConfig{
			PublicBucket:     "public",
			StagingBucket:    "staging",
			AttachmentBucket: "attachments",
		},
	}

	public := base
	public.Media.AttachmentAccessMode = AttachmentAccessPublic
	public.ObjectStore.AttachmentPublicBaseURL = "https://cdn.example.com/media"
	require.NoError(t, public.Validate())

	presigned := base
	presigned.Media.AttachmentAccessMode = AttachmentAccessPresigned
	require.NoError(t, presigned.Validate())

	invalidMode := base
	invalidMode.Media.AttachmentAccessMode = "mixed"
	require.ErrorContains(t, invalidMode.Validate(), "unsupported attachment access mode")

	invalidURL := base
	invalidURL.Media.AttachmentAccessMode = AttachmentAccessPublic
	invalidURL.ObjectStore.AttachmentPublicBaseURL = "http://cdn.example.com"
	require.ErrorContains(t, invalidURL.Validate(), "absolute https url")

	missingBucket := base
	missingBucket.ObjectStore.AttachmentBucket = ""
	require.ErrorContains(t, missingBucket.Validate(), "attachment object store bucket is required")
}

func TestImageConstraintsArePurposeSpecific(t *testing.T) {
	cfg := MediaConfig{
		ImageConstraints: ImageConstraintsConfig{
			UserAvatar: ImageConstraintProfile{
				MaxSizeBytes:        1 << 20,
				MaxDimension:        512,
				MaxPixels:           512 * 512,
				AllowedContentTypes: []string{"image/png"},
			},
			GuildIcon: ImageConstraintProfile{
				MaxSizeBytes:        2 << 20,
				MaxDimension:        1024,
				MaxPixels:           1024 * 1024,
				AllowedContentTypes: []string{"image/jpeg", "image/png"},
			},
		},
	}

	avatar := cfg.ImageConstraintsFor(ImagePurposeUserAvatar)
	require.Equal(t, int64(1<<20), avatar.MaxSizeBytes)
	require.Equal(t, int32(512), avatar.MaxDimension)
	require.Equal(t, []string{"image/png"}, avatar.AllowedContentTypes)

	icon := cfg.ImageConstraintsFor(ImagePurposeGuildIcon)
	require.Equal(t, int64(2<<20), icon.MaxSizeBytes)
	require.Equal(t, int32(1024), icon.MaxDimension)
	require.True(t, icon.AllowsContentType("image/jpeg"))

	attachment := cfg.ImageConstraintsFor(ImagePurposeMessageAttachment)
	require.Equal(t, int64(10<<20), attachment.MaxSizeBytes)
	require.Equal(t, []string{"image/jpeg", "image/png", "image/webp"}, attachment.AllowedContentTypes)
}
