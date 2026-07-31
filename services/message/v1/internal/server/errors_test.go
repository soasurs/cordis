package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

func TestMapStoreErrorMapsIdempotencyContention(t *testing.T) {
	err := mapStoreError(store.ErrIdempotencyContention)
	require.Equal(t, codes.Unavailable, status.Code(err))
}
