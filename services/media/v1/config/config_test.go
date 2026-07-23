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
