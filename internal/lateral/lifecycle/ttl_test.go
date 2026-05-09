package lifecycle

import (
	"testing"
	"time"
)

func TestTTL_ExpiresAtFromParent(t *testing.T) {
	now := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	got := ComputeExpiresAt(now, 180*24*time.Hour)
	want := now.Add(180 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTTL_ColdCycleStale(t *testing.T) {
	now := time.Now()
	thirtyOneDaysAgo := now.Add(-31 * 24 * time.Hour)
	if !IsColdStale(now, thirtyOneDaysAgo, 30*24*time.Hour) {
		t.Fatal("expected cold-stale")
	}
	tenDaysAgo := now.Add(-10 * 24 * time.Hour)
	if IsColdStale(now, tenDaysAgo, 30*24*time.Hour) {
		t.Fatal("expected not-stale")
	}
}

func TestTTL_ColdCycleZeroColdSinceIsNotStale(t *testing.T) {
	now := time.Now()
	if IsColdStale(now, time.Time{}, 30*24*time.Hour) {
		t.Fatal("zero coldSince must mean never went cold")
	}
}

func TestTTL_SanityBoundExpiresAtFarFuture(t *testing.T) {
	now := time.Now()
	expires := now.Add(60 * 24 * time.Hour) // 60d in future, beyond 30d sanity bound
	if !IsSanityViolation(now, expires, time.Time{}) {
		t.Fatal("expected sanity violation for 60d-future expiry")
	}
}

func TestTTL_SanityBoundExpiresBeforeDiscovered(t *testing.T) {
	now := time.Now()
	discovered := now
	expires := now.Add(-1 * time.Hour) // before discovered_at
	if !IsSanityViolation(now, expires, discovered) {
		t.Fatal("expected sanity violation for expires<discovered")
	}
}

func TestTTL_SanityBoundWithinFutureBoundAndAfterDiscovered(t *testing.T) {
	now := time.Now()
	discovered := now.Add(-10 * 24 * time.Hour) // discovered 10d ago
	expires := now.Add(20 * 24 * time.Hour)     // 20d in future, within 30d bound
	if IsSanityViolation(now, expires, discovered) {
		t.Fatal("expected no violation for in-bounds expiry")
	}
}
