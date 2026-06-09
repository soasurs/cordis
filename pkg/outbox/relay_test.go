package outbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRetryDelay(t *testing.T) {
	tests := []struct {
		retryCount int
		want       time.Duration
	}{
		{retryCount: 0, want: time.Second},
		{retryCount: 1, want: 2 * time.Second},
		{retryCount: 5, want: 32 * time.Second},
		{retryCount: 6, want: 64 * time.Second},
		{retryCount: 20, want: 64 * time.Second},
	}

	for _, tt := range tests {
		got := retryDelay(tt.retryCount)
		require.Equal(t, tt.want, got, "retryDelay(%d)", tt.retryCount)
	}
}

func TestBatchYieldDelay(t *testing.T) {
	for range 100 {
		delay := batchYieldDelay()
		require.GreaterOrEqual(t, delay, 5*time.Millisecond)
		require.LessOrEqual(t, delay, 20*time.Millisecond)
	}
}

func TestRotatedPartitions(t *testing.T) {
	tests := []struct {
		start int
		want  []int
	}{
		{start: 0, want: []int{1, 3, 7}},
		{start: 3, want: []int{3, 7, 1}},
		{start: 5, want: []int{7, 1, 3}},
		{start: 8, want: []int{1, 3, 7}},
	}
	for _, tt := range tests {
		got := rotatedPartitions([]int{1, 3, 7}, tt.start)
		require.Equal(t, tt.want, got, "rotatedPartitions(start=%d)", tt.start)
	}
}
