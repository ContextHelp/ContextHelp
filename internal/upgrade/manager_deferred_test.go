package upgrade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManager_DeferredAndSkippedOutliveTheRun(t *testing.T) {
	m := NewManager("")
	// A run that defers everything starts nothing: the list is set idle.
	d := []Deferred{{Pipeline: "p", Objects: 2, Reason: "hosted"}}
	if err := m.SetDeferred(d); err != nil {
		t.Fatal(err)
	}
	d[0].Objects = 99 // the manager keeps its own copy
	if snap := m.Snapshot(); snap.State != StateIdle || len(snap.Deferred) != 1 || snap.Deferred[0].Objects != 2 {
		t.Fatalf("idle snapshot = %+v", snap)
	}
	if err := m.StartTarget(BucketReingestSelective, "rederive@x", 3); err != nil {
		t.Fatal(err)
	}
	if err := m.SetSkipped(map[string]int{"refused": 1}); err != nil {
		t.Fatal(err)
	}
	snap := m.Snapshot()
	if len(snap.Deferred) != 1 || snap.Skipped["refused"] != 1 {
		t.Fatalf("running snapshot = %+v", snap)
	}
	snap.Deferred[0].Objects = 77
	snap.Skipped["refused"] = 77
	if got := m.Snapshot(); got.Deferred[0].Objects != 2 || got.Skipped["refused"] != 1 {
		t.Fatal("a snapshot shares the manager's data")
	}
	if err := m.CompleteRun(RunSummary{}); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot(); got.State != StateIdle || len(got.Deferred) != 1 || got.Skipped["refused"] != 1 {
		t.Fatalf("after the run: %+v", got)
	}
	if err := m.SetDeferred(nil); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot(); len(got.Deferred) != 0 {
		t.Fatalf("an empty list must clear: %+v", got)
	}
}

// A re-projection's status and shadow file never carry what a re-derive
// left; a re-derive's do.
func TestManager_DeferredIsNotCopiedIntoAnotherRunsStatus(t *testing.T) {
	shadow := filepath.Join(t.TempDir(), "upgrade-state.json")
	m := NewManager(shadow)
	if err := m.SetDeferred([]Deferred{{Pipeline: "p", Objects: 1, Reason: "hosted"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetSkipped(map[string]int{"refused": 1}); err != nil {
		t.Fatal(err)
	}
	if err := m.StartTarget(BucketReindexAuto, "projection@v1", 5); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot(); len(got.Deferred) != 0 || len(got.Skipped) != 0 {
		t.Fatalf("re-projection snapshot carries re-derive leftovers: %+v", got)
	}
	raw, err := os.ReadFile(shadow)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "deferred") || strings.Contains(string(raw), "skipped") {
		t.Fatalf("re-projection shadow file carries them: %s", raw)
	}
	if err := m.Complete(); err != nil {
		t.Fatal(err)
	}
	if err := m.StartTarget(BucketReingestSelective, "rederive@x", 5); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot(); len(got.Deferred) != 1 || got.Skipped["refused"] != 1 {
		t.Fatalf("re-derive snapshot = %+v", got)
	}
	raw, _ = os.ReadFile(shadow)
	if !strings.Contains(string(raw), "deferred") {
		t.Fatalf("re-derive shadow file lacks them: %s", raw)
	}
}

// The groups a re-derive stopped are shown like the deferred ones: in any
// state, to a re-derive and an idle daemon, never to another run.
func TestManager_StoppedGroupsAreShownLikeTheDeferredOnes(t *testing.T) {
	m := NewManager("")
	if err := m.SetStopped([]Stopped{{Pipeline: "p", Kind: "fetch", Cause: "timeout"}}); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot(); got.State != StateIdle || len(got.Stopped) != 1 || got.Stopped[0].Kind != "fetch" {
		t.Fatalf("idle snapshot = %+v", got)
	}
	if err := m.StartTarget(BucketReindexAuto, "projection@v1", 1); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot(); len(got.Stopped) != 0 {
		t.Fatalf("a re-projection shows the stopped groups: %+v", got)
	}
	if err := m.Complete(); err != nil {
		t.Fatal(err)
	}
	if err := m.SetStopped(nil); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot(); len(got.Stopped) != 0 {
		t.Fatalf("an empty list must clear: %+v", got)
	}
}
