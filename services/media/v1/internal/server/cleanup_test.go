package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

func TestCleanupExpiredRechecksStateUnderLock(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	now := time.Now().UnixMilli()
	expired := &store.Asset{
		ID:              1,
		CreatedByUserID: 1001,
		SubjectID:       3001,
		Kind:            store.KindMessageAttachment,
		Status:          store.StatusCreated,
		PublishedKey:    "attachments/3001/1/token/file.bin",
		ExpiresAt:       now - 1000,
	}
	ready := &store.Asset{
		ID:              2,
		CreatedByUserID: 1001,
		Status:          store.StatusReady,
		ExpiresAt:       now - 1000,
	}
	assets.createAsset(expired)
	assets.createAsset(ready)
	objects.setObject(expired.PublishedKey, "application/octet-stream", []byte("orphan"))

	require.NoError(t, srv.CleanupExpired(t.Context()))
	require.Equal(t, store.StatusExpired, expired.Status)
	require.False(t, objects.hasObject(expired.PublishedKey))
	require.Equal(t, store.StatusReady, ready.Status)
}
