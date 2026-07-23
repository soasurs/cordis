package objectstore

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreatePresignedPutURLSignsContentContract(t *testing.T) {
	store, err := NewS3(Config{
		Endpoint:  "s3.example.com",
		Region:    "us-east-1",
		Bucket:    "media",
		AccessKey: "access-key",
		SecretKey: "secret-key",
		Secure:    true,
	})
	require.NoError(t, err)

	rawURL, err := store.CreatePresignedPutURL(
		t.Context(),
		"staging/123",
		"image/png",
		4096,
		900,
	)
	require.NoError(t, err)
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)

	signedHeaders := strings.Split(parsed.Query().Get("X-Amz-SignedHeaders"), ";")
	require.Contains(t, signedHeaders, "content-length")
	require.Contains(t, signedHeaders, "content-type")
	require.Contains(t, signedHeaders, "host")
}
