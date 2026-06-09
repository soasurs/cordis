package outbox

import "testing"

func TestPartitionForKey(t *testing.T) {
	key := []byte("channel-123")
	first := PartitionForKey(key, DefaultPartitionCount)
	if first < 0 || first >= DefaultPartitionCount {
		t.Fatalf("PartitionForKey() = %d, want [0, %d)", first, DefaultPartitionCount)
	}
	if second := PartitionForKey(key, DefaultPartitionCount); second != first {
		t.Fatalf("PartitionForKey() changed from %d to %d", first, second)
	}
	if got := PartitionForKey([]byte("130"), DefaultPartitionCount); got != 2 {
		t.Fatalf("PartitionForKey(130) = %d, want 2", got)
	}
}
