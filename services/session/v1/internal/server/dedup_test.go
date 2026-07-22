package server

import (
	"math/rand/v2"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testEventType = "guild.updated"

func BenchmarkDedupCheckAndAdd_Single(b *testing.B) {
	ds := newDedupStore()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ds.checkAndAdd(routeKindGuild, int64(i), int64(i), testEventType, dedupTTL)
	}
}

func BenchmarkDedupCheckAndAdd_Concurrent_4(b *testing.B) {
	benchmarkDedupConcurrent(b, 4)
}

func BenchmarkDedupCheckAndAdd_Concurrent_16(b *testing.B) {
	benchmarkDedupConcurrent(b, 16)
}

func BenchmarkDedupCheckAndAdd_Concurrent_64(b *testing.B) {
	benchmarkDedupConcurrent(b, 64)
}

func BenchmarkDedupCheckAndAdd_Concurrent_256(b *testing.B) {
	benchmarkDedupConcurrent(b, 256)
}

func benchmarkDedupConcurrent(b *testing.B, goroutines int) {
	ds := newDedupStore()
	b.ResetTimer()
	var wg sync.WaitGroup
	opsPerG := b.N / goroutines
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < opsPerG; i++ {
				id := int64(base*opsPerG + i)
				ds.checkAndAdd(routeKindGuild, id, id, testEventType, dedupTTL)
			}
		}(g)
	}
	wg.Wait()
}

func BenchmarkDedupCheckAndAdd_Duplicate(b *testing.B) {
	ds := newDedupStore()
	id := int64(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ds.checkAndAdd(routeKindGuild, id, id, testEventType, dedupTTL)
	}
}

func BenchmarkDedupCheckAndAdd_MixedRoutes(b *testing.B) {
	ds := newDedupStore()
	kinds := []uint8{routeKindGuild, routeKindGuildMsg, routeKindUser}
	types := []string{"guild.updated", "message.created", "relationship.updated"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		slot := i % 3
		id := int64(i)
		ds.checkAndAdd(kinds[slot], id, id, types[slot], dedupTTL)
	}
}

func BenchmarkDedupRotateAll_Empty(b *testing.B) {
	ds := newDedupStore()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ds.rotateAll()
	}
}

func BenchmarkDedupRotateAll_Populated(b *testing.B) {
	const entries = 1000000
	ds := newDedupStore()
	for i := 0; i < entries; i++ {
		id := int64(i)
		ds.checkAndAdd(routeKindGuild, id, id, testEventType, dedupTTL)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ds.rotateAll()
	}
}

func BenchmarkDedupRealistic(b *testing.B) {
	ds := newDedupStore()
	kinds := []uint8{routeKindGuild, routeKindGuildMsg, routeKindUser}
	types := []string{"guild.updated", "message.created", "relationship.updated"}
	var idBase [3]int64
	rng := rand.New(rand.NewPCG(42, 42))

	rotateOps := int64(dedupTTL / 2 / time.Nanosecond)
	if rotateOps <= 0 {
		rotateOps = 1
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		slot := i % 3
		idBase[slot]++
		kind := kinds[slot]
		ds.checkAndAdd(kind, idBase[slot], idBase[slot], types[slot], dedupTTL)

		if rng.IntN(10) == 0 {
			dupSlot := rng.IntN(3)
			dupID := idBase[dupSlot] - int64(rng.IntN(1000))
			if dupID > 0 {
				ds.checkAndAdd(kinds[dupSlot], dupID, dupID, types[dupSlot], dedupTTL)
			}
		}

		if int64(i) > 0 && int64(i)%rotateOps == 0 {
			ds.rotateAll()
		}
	}
}

func BenchmarkDedupHighContention_SameKey(b *testing.B) {
	ds := newDedupStore()
	var wg sync.WaitGroup
	goroutines := 64
	opsPerG := b.N / goroutines
	b.ResetTimer()
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerG; i++ {
				ds.checkAndAdd(routeKindGuild, 42, 42, testEventType, dedupTTL)
			}
		}()
	}
	wg.Wait()
}

func TestDedupCorrectnessUnderConcurrency(t *testing.T) {
	ds := newDedupStore()
	const (
		goroutines    = 100
		opsPerRoutine = 1000
	)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(base int64) {
			defer wg.Done()
			for i := int64(0); i < opsPerRoutine; i++ {
				id := base*opsPerRoutine + i
				allowed := ds.checkAndAdd(routeKindGuild, id, id, testEventType, dedupTTL)
				if !allowed {
					panic("unexpected duplicate: " + strconv.FormatInt(id, 10))
				}
			}
		}(int64(g))
	}
	wg.Wait()
}

func TestDedupDuplicateDetection(t *testing.T) {
	ds := newDedupStore()
	if !ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, dedupTTL) {
		t.Fatal("first call should pass")
	}
	if ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, dedupTTL) {
		t.Fatal("second call with same key should be rejected")
	}
	if !ds.checkAndAdd(routeKindUser, 1, 100, "relationship.updated", dedupTTL) {
		t.Fatal("different routeKind should pass")
	}
	if !ds.checkAndAdd(routeKindGuild, 2, 100, testEventType, dedupTTL) {
		t.Fatal("different routeID should pass")
	}
	if !ds.checkAndAdd(routeKindGuild, 1, 200, testEventType, dedupTTL) {
		t.Fatal("different eventID should pass")
	}
}

func TestDedupCrossServiceNamespaceIsolation(t *testing.T) {
	ds := newDedupStore()

	// Same routeKind, routeID, and snowflake ID, but different event types.
	if !ds.checkAndAdd(routeKindUser, 1, 100, "message.created", dedupTTL) {
		t.Fatal("message.created should pass")
	}
	if !ds.checkAndAdd(routeKindUser, 1, 100, "relationship.updated", dedupTTL) {
		t.Fatal("relationship.updated with same snowflake ID should pass (different namespace)")
	}
	if !ds.checkAndAdd(routeKindUser, 1, 100, "dm.channel.created", dedupTTL) {
		t.Fatal("dm.channel.created with same snowflake ID should pass (different namespace)")
	}
	// But same event type should still be deduped.
	if ds.checkAndAdd(routeKindUser, 1, 100, "message.created", dedupTTL) {
		t.Fatal("duplicate message.created should be rejected")
	}
}

func TestDedupZeroEventIDSkipsDedup(t *testing.T) {
	ds := newDedupStore()
	for i := 0; i < 100; i++ {
		if !ds.checkAndAdd(routeKindGuild, 1, 0, testEventType, dedupTTL) {
			t.Fatal("eventID=0 should always pass")
		}
	}
}

func TestDedupRotateExpiresOldGeneration(t *testing.T) {
	ds := newDedupStore()
	shortTTL := 10 * time.Millisecond

	if !ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, shortTTL) {
		t.Fatal("first call should pass")
	}
	if ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, shortTTL) {
		t.Fatal("duplicate should be rejected")
	}

	time.Sleep(shortTTL + 10*time.Millisecond)
	ds.rotateAll()

	time.Sleep(10 * time.Millisecond)
	if !ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, shortTTL) {
		t.Fatal("after rotation and expiry, should pass again")
	}
}

func TestDedupRotatePreservesCurrentGeneration(t *testing.T) {
	ds := newDedupStore()
	shortTTL := 50 * time.Millisecond

	if !ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, shortTTL) {
		t.Fatal("first call should pass")
	}

	ds.rotateAll()

	if ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, shortTTL) {
		t.Fatal("after rotation, current gen becomes previous, should still reject")
	}

	time.Sleep(shortTTL + 10*time.Millisecond)
	ds.rotateAll()

	if !ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, shortTTL) {
		t.Fatal("after second rotation, should pass again")
	}
}

func TestDedupRapidRotationsPreserveUnexpiredEntry(t *testing.T) {
	ds := newDedupStore()
	ttl := time.Hour

	if !ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, ttl) {
		t.Fatal("first call should pass")
	}

	for range dedupNumGens * 2 {
		ds.rotateAll()
	}

	if ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, ttl) {
		t.Fatal("rapid rotations must not discard an unexpired entry")
	}
}

func TestDedupMemoryUsage(t *testing.T) {
	sizes := []int{100, 1000, 10000, 100000, 500000, 1000000}
	kinds := [3]uint8{routeKindGuild, routeKindGuildMsg, routeKindUser}
	types := [3]string{"guild.updated", "message.created", "relationship.updated"}
	for _, n := range sizes {
		ds := newDedupStore()
		runtime.GC()
		var m1 runtime.MemStats
		runtime.ReadMemStats(&m1)

		for i := 0; i < n; i++ {
			id := int64(i)
			slot := i % 3
			ds.checkAndAdd(kinds[slot], id, id, types[slot], dedupTTL)
		}
		runtime.GC()
		var m2 runtime.MemStats
		runtime.ReadMemStats(&m2)
		alloc := m2.TotalAlloc - m1.TotalAlloc
		bytesPerEntry := float64(alloc) / float64(n)

		t.Logf("entries=%d  total_alloc=%s  per_entry=%.0f B",
			n, formatBytes(alloc), bytesPerEntry)
	}
}

func BenchmarkDedupMemoryAndFree(b *testing.B) {
	const entries = 200000
	ds := newDedupStore()
	for i := 0; i < entries; i++ {
		id := int64(i)
		ds.checkAndAdd(routeKindGuild, id, id, testEventType, dedupTTL)
	}

	runtime.GC()
	var beforeRotate runtime.MemStats
	runtime.ReadMemStats(&beforeRotate)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ds.rotateAll()
	}
	b.StopTimer()

	runtime.GC()
	var afterRotate runtime.MemStats
	runtime.ReadMemStats(&afterRotate)
	_ = afterRotate.HeapInuse

	b.ReportMetric(float64(beforeRotate.HeapInuse)/1024/1024, "MiB_before")
	b.ReportMetric(float64(afterRotate.HeapInuse)/1024/1024, "MiB_after")
}

func BenchmarkDedupAllocsPerOp(b *testing.B) {
	ds := newDedupStore()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := int64(i)
		ds.checkAndAdd(routeKindGuild, id, id, testEventType, dedupTTL)
	}
}

func formatBytes(bytes uint64) string {
	units := []string{"B", "KB", "MB", "GB"}
	val := float64(bytes)
	unit := 0
	for val >= 1024 && unit < len(units)-1 {
		val /= 1024
		unit++
	}
	return strconv.FormatFloat(val, 'f', 1, 64) + units[unit]
}

func TestDedupRemoveAllowsRetryAfterFailure(t *testing.T) {
	ds := newDedupStore()
	shortTTL := time.Hour

	if !ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, shortTTL) {
		t.Fatal("first call should pass")
	}
	if ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, shortTTL) {
		t.Fatal("duplicate should be rejected")
	}

	ds.remove(routeKindGuild, 1, 100, testEventType)

	if !ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, shortTTL) {
		t.Fatal("after remove, should pass again")
	}
}

func TestDedupAtomicReservationPreventsConcurrentDuplicates(t *testing.T) {
	ds := newDedupStore()
	const goroutines = 100

	var passed, rejected atomic.Int32
	var wg sync.WaitGroup
	var started sync.WaitGroup
	started.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started.Done()
			started.Wait()
			if ds.checkAndAdd(routeKindGuild, 1, 999, testEventType, dedupTTL) {
				passed.Add(1)
			} else {
				rejected.Add(1)
			}
		}()
	}
	wg.Wait()

	if passed.Load() != 1 {
		t.Fatalf("exactly one goroutine should pass, got %d passed, %d rejected", passed.Load(), rejected.Load())
	}
	if rejected.Load() != goroutines-1 {
		t.Fatalf("expected %d rejected, got %d", goroutines-1, rejected.Load())
	}
}

func TestDedupRemoveIsShardSafeAcrossGenerations(t *testing.T) {
	ds := newDedupStore()
	shortTTL := 10 * time.Millisecond

	if !ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, shortTTL) {
		t.Fatal("first call should pass")
	}

	ds.rotateAll()

	ds.remove(routeKindGuild, 1, 100, testEventType)

	if !ds.checkAndAdd(routeKindGuild, 1, 100, testEventType, shortTTL) {
		t.Fatal("after rotate + remove, should pass again")
	}
}
