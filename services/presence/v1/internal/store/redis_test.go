package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewRedisStoreDerivesMutationLockTTL(t *testing.T) {
	tests := []struct {
		name           string
		publishTimeout time.Duration
		want           time.Duration
	}{
		{name: "default timeout", want: 10 * time.Second},
		{name: "short timeout", publishTimeout: time.Second, want: 10 * time.Second},
		{name: "long timeout", publishTimeout: 12 * time.Second, want: 17 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewRedisStore(nil, 0, 0, 0, tt.publishTimeout)
			require.Equal(t, tt.want, store.mutationLockTTL)
		})
	}
}
