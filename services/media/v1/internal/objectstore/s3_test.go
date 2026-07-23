package objectstore

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreatePresignedPutRequestSignsContentContract(t *testing.T) {
	store, err := NewS3(Config{
		Endpoint:  "s3.example.com",
		Region:    "us-east-1",
		Bucket:    "media",
		AccessKey: "access-key",
		SecretKey: "secret-key",
		Secure:    true,
	})
	require.NoError(t, err)

	request, err := store.CreatePresignedPutRequest(
		t.Context(),
		"staging/123",
		"image/png",
		4096,
		900,
	)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"Content-Length": "4096",
		"Content-Type":   "image/png",
	}, request.RequestHeaders)
	parsed, err := url.Parse(request.URL)
	require.NoError(t, err)

	signedHeaders := strings.Split(parsed.Query().Get("X-Amz-SignedHeaders"), ";")
	require.Contains(t, signedHeaders, "content-length")
	require.Contains(t, signedHeaders, "content-type")
	require.Contains(t, signedHeaders, "host")
}
