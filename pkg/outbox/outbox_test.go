package outbox

import "testing"

func TestPartitionForKey(t *testing.T) {
	key := []byte("channel-123")
	first := PartitionForKey(key)
	if first < 0 || first >= PartitionCount {
		t.Fatalf("PartitionForKey() = %d, want [0, %d)", first, PartitionCount)
	}
	if second := PartitionForKey(key); second != first {
		t.Fatalf("PartitionForKey() changed from %d to %d", first, second)
	}
	if got := PartitionForKey([]byte("130")); got != 2 {
		t.Fatalf("PartitionForKey(130) = %d, want 2", got)
	}
}
