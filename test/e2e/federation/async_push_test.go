//go:build federation_e2e

package federation_e2e

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// auditRef is the audit doc that flags the impl gap surfaced by skipped tests.
const auditRef = "see audit doc 2026-04-30-federation-e2e-audit.md (T-0093/T-0088)"

// TestFederation_AsyncPush_ObjectAppearsAtMergedDB verifies AC #1:
// "Objects appear in merged DB within configured interval (default 5m)."
//
// Strategy: capture in source, drive one synchronous push, poll target.
// The push step stands in for the (unwired) async worker tick.
func TestFederation_AsyncPush_ObjectAppearsAtMergedDB(t *testing.T) {
	ix := setupTwoInstances(t)

	captureBookmark(t, ix.src, "obj-appear-1", "hash-appear-1", "hello world", nil)

	if err := pushOnce(t, ix); err != nil {
		t.Fatalf("pushOnce: %v", err)
	}

	waitForFederation(t, ix.tgt, "obj-appear-1", 2*time.Second)
}

// TestFederation_AsyncPush_DedupeByContentHash verifies AC #2:
// "Dedup: re-pushing same content-hash → no duplicate rows in target."
func TestFederation_AsyncPush_DedupeByContentHash(t *testing.T) {
	ix := setupTwoInstances(t)

	captureBookmark(t, ix.src, "obj-dup-1", "hash-shared", "first capture", nil)

	if err := pushOnce(t, ix); err != nil {
		t.Fatalf("first pushOnce: %v", err)
	}

	// Push again — LocalPusher must dedup by ContentHash.
	if err := pushOnce(t, ix); err != nil {
		t.Fatalf("second pushOnce: %v", err)
	}

	if got := countTargetObjects(t, ix.tgt); got != 1 {
		t.Errorf("dedup broken: want 1 row at target after re-push, got %d", got)
	}
}

// TestFederation_AsyncPush_WatermarkAdvances verifies AC #3:
// "federation_watermarks table updated with last_synced_at after each
// successful batch."
//
// SKIP REASON: ctxt has the federation_watermarks SQLite table (migration
// 025) but NO Go API to read or advance it. LocalPusher.Push neither reads
// nor writes the watermark — the worker that should is unwired. Until the
// worker + Watermark store land, this AC is unverifiable end-to-end.
func TestFederation_AsyncPush_WatermarkAdvances(t *testing.T) {
	t.Skip("US-0319 watermark advancement not wired: federation_watermarks " +
		"table exists (migration 025) but LocalPusher does not advance it " +
		"and no Go Watermark store exists; " + auditRef)

	ix := setupTwoInstances(t)
	captureBookmark(t, ix.src, "obj-wm-1", "hash-wm-1", "watermark probe", nil)

	if err := pushOnce(t, ix); err != nil {
		t.Fatalf("pushOnce: %v", err)
	}

	got, err := readWatermark(t, ix.tgt.path, ix.fedName)
	if err != nil {
		t.Fatalf("readWatermark: %v", err)
	}
	if got == "" {
		t.Fatal("watermark not written: expected federation_watermarks row " +
			"after successful push")
	}
	parsed, err := time.Parse(time.RFC3339Nano, got)
	if err != nil {
		t.Fatalf("watermark format: parse %q: %v", got, err)
	}
	epoch := time.Unix(0, 0).UTC()
	if !parsed.After(epoch) {
		t.Errorf("watermark not advanced: got %v, want > epoch", parsed)
	}
}

// TestFederation_AsyncPush_CrashRecovery verifies AC #4:
// "Worker crash (mid-push) → watermark not advanced → objects re-pushed on
// next tick."
//
// SKIP REASON: requires (1) async worker goroutine, (2) watermark advance
// inside a transaction, and (3) ability to inject a crash mid-push. None of
// these exist today. LocalPusher.Push is single-shot and has no transaction
// boundary that wraps watermark + objects together.
func TestFederation_AsyncPush_CrashRecovery(t *testing.T) {
	t.Skip("US-0319 crash recovery not testable: async worker not wired in " +
		"cmd/dpkms; LocalPusher.Push has no tx-boundary covering watermark " +
		"+ objects; " + auditRef)

	ix := setupTwoInstances(t)
	captureBookmark(t, ix.src, "obj-crash-1", "hash-crash-1", "crash probe", nil)

	// Cancel context immediately — simulate worker death mid-push.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pusher := federation.NewLocalPusher(ix.fedName, ix.tgt.path)
	if err := pusher.Push(ctx, []storage.KnowledgeObject{}, nil); err == nil {
		t.Log("Push on canceled ctx returned nil (expected: error or no-op)")
	}

	// Watermark must NOT be advanced after a failed push.
	got, err := readWatermark(t, ix.tgt.path, ix.fedName)
	if err != nil {
		t.Fatalf("readWatermark: %v", err)
	}
	if got != "" {
		t.Errorf("watermark advanced despite crash: got %q, want empty", got)
	}

	// Restart: object should appear on next tick.
	if err := pushOnce(t, ix); err != nil {
		t.Fatalf("recovery pushOnce: %v", err)
	}
	waitForFederation(t, ix.tgt, "obj-crash-1", 2*time.Second)
}

// TestFederation_AsyncPush_EdgesPushed verifies AC #5:
// "Edges associated with pushed objects are upserted at target."
func TestFederation_AsyncPush_EdgesPushed(t *testing.T) {
	ix := setupTwoInstances(t)

	captureBookmark(t, ix.src, "obj-from", "hash-from", "from", nil)
	captureBookmark(t, ix.src, "obj-to", "hash-to", "to", nil)

	now := time.Now().UTC()
	edge := &storage.Edge{
		ID:        "edge-from-to",
		FromType:  "object",
		FromID:    "obj-from",
		ToType:    "object",
		ToID:      "obj-to",
		EdgeType:  "related",
		CreatedAt: now,
	}
	if err := ix.src.drv.Edges().Create(context.Background(), edge); err != nil {
		t.Fatalf("create edge: %v", err)
	}

	if err := pushOnce(t, ix); err != nil {
		t.Fatalf("pushOnce: %v", err)
	}

	got, err := ix.tgt.drv.Edges().ListFrom(context.Background(), "object", "obj-from")
	if err != nil {
		t.Fatalf("ListFrom on target: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 edge at target, got %d", len(got))
	}
	if got[0].ID != "edge-from-to" {
		t.Errorf("edge ID: got %q, want %q", got[0].ID, "edge-from-to")
	}
}

// TestFederation_AsyncPush_MentionsPushed verifies AC #6:
// "Mentions associated with pushed objects are upserted at target."
//
// SKIP REASON: ctxt represents mentions as []uri.URI inside a
// KnowledgeObject (pkg/pluginapi.KnowledgeObject.Mentions field) and as
// metadata.people in ObjectFilter, NOT as a separate mentions table. There
// is no MentionStore interface and no dedicated table to assert against.
// Once mentions are normalised into edges (mention edges) per ADR-049 and
// the federation pusher carries those edges, this test becomes
// implementable. Today it would be testing the same code path as the
// edges test above.
func TestFederation_AsyncPush_MentionsPushed(t *testing.T) {
	t.Skip("US-0319 mentions: no separate mentions table or store in ctxt " +
		"(mentions live as []uri.URI inside KnowledgeObject and as " +
		"object→entity edges); test requires either a dedicated " +
		"MentionStore or edge-based mention representation; " + auditRef)

	ix := setupTwoInstances(t)
	captureBookmark(t, ix.src, "obj-m-1", "hash-m-1", "mention probe",
		[]string{"alice", "bob"})

	if err := pushOnce(t, ix); err != nil {
		t.Fatalf("pushOnce: %v", err)
	}

	got, err := ix.tgt.drv.Objects().Get(context.Background(), "obj-m-1")
	if err != nil {
		t.Fatalf("Get on target: %v", err)
	}
	people, ok := got.Metadata["people"].([]any)
	if !ok {
		t.Fatalf("metadata.people not []any: got %T", got.Metadata["people"])
	}
	if len(people) != 2 {
		t.Errorf("want 2 mentions at target, got %d", len(people))
	}
}

// TestFederation_AsyncPush_NoChangesNoOp verifies AC #7:
// "Zero objects since last watermark → tick is a no-op (no DB writes,
// no error)."
//
// With no source objects, push must succeed and not write anything.
func TestFederation_AsyncPush_NoChangesNoOp(t *testing.T) {
	ix := setupTwoInstances(t)

	before := countTargetObjects(t, ix.tgt)
	if err := pushOnce(t, ix); err != nil {
		t.Fatalf("pushOnce on empty source returned error: %v", err)
	}
	after := countTargetObjects(t, ix.tgt)
	if before != after {
		t.Errorf("no-op tick wrote rows: before=%d after=%d", before, after)
	}

	// Watermark must remain unchanged (currently always empty since
	// nothing writes it; once watermark write lands, this assertion will
	// need updating along with the AsyncPush_WatermarkAdvances test).
	got, err := readWatermark(t, ix.tgt.path, ix.fedName)
	if err != nil {
		t.Fatalf("readWatermark: %v", err)
	}
	if got != "" {
		t.Errorf("no-op tick advanced watermark: got %q, want empty", got)
	}
}

// TestFederation_AsyncPush_GracefulShutdown verifies AC #9:
// "Goroutine exits cleanly on server shutdown (context cancel respected)."
//
// SKIP REASON: there is no federation worker goroutine to observe. cmd/dpkms
// does not start one (audit T-0093 finding). Once the worker exists and is
// wired into the server lifecycle, this test should boot the real dpkms
// binary, send SIGTERM, and assert the process exits within 5s with no
// leaked goroutines.
func TestFederation_AsyncPush_GracefulShutdown(t *testing.T) {
	t.Skip("US-0319 worker goroutine not wired into cmd/dpkms; nothing to " +
		"shut down; " + auditRef)

	// Below documents the intended contract once impl lands.
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)

	// Hypothetical: federation.RunWorker(ctx, target, src, tgt) blocks until
	// ctx is canceled. Today no such function exists.
	go func() {
		defer wg.Done()
		<-ctx.Done()
	}()

	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit within 5s after context cancel")
	}
}
