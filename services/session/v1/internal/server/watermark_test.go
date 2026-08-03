package server

import "testing"

func TestWatermarkAcceptsMonotonicSequences(t *testing.T) {
	store := newWatermarkStore()
	if !store.accept(routeKindGuildMsg, 1, 10, 5) {
		t.Fatal("first sequence should be accepted")
	}
	if store.accept(routeKindGuildMsg, 1, 10, 5) {
		t.Fatal("duplicate sequence should be rejected")
	}
	if store.accept(routeKindGuildMsg, 1, 10, 4) {
		t.Fatal("older sequence should be rejected")
	}
	if !store.accept(routeKindGuildMsg, 1, 10, 6) {
		t.Fatal("newer sequence should be accepted")
	}
}

func TestWatermarkIsScopedByRouteAndChannel(t *testing.T) {
	store := newWatermarkStore()
	store.accept(routeKindGuildMsg, 1, 10, 5)
	if !store.accept(routeKindGuildMsg, 1, 11, 1) {
		t.Fatal("different channel must be independent")
	}
	if !store.accept(routeKindGuildMsg, 2, 10, 1) {
		t.Fatal("different route must be independent")
	}
	if !store.accept(routeKindUser, 1, 10, 1) {
		t.Fatal("different route kind must be independent")
	}
}

func TestWatermarkAcceptsLegacyEventsWithoutSequence(t *testing.T) {
	store := newWatermarkStore()
	if !store.accept(routeKindGuildMsg, 1, 10, 0) {
		t.Fatal("zero sequence must be accepted")
	}
}

func TestWatermarkAcceptsLargeSnowflakeIDs(t *testing.T) {
	store := newWatermarkStore()
	if !store.accept(routeKindGuildMsg, 9007199254740993, 9007199254740994, 1) {
		t.Fatal("large snowflake ids must not panic")
	}
}
