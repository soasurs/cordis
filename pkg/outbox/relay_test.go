package outbox

import (
	"reflect"
	"testing"
	"time"
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
		if got := retryDelay(tt.retryCount); got != tt.want {
			t.Fatalf("retryDelay(%d) = %s, want %s", tt.retryCount, got, tt.want)
		}
	}
}

func TestBatchYieldDelay(t *testing.T) {
	for range 100 {
		delay := batchYieldDelay()
		if delay < 5*time.Millisecond || delay > 20*time.Millisecond {
			t.Fatalf("batchYieldDelay() = %s, want [5ms, 20ms]", delay)
		}
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
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("rotatedPartitions(start=%d) = %v, want %v", tt.start, got, tt.want)
		}
	}
}
