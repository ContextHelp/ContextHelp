//go:build federation_e2e

// Cycle detection e2e tests for US-0318 ("Configure federation targets").
//
// AC under test: "Cycle in federation DAG detected at startup and rejected
// with descriptive error". T-0174 shipped Phase 1 detection inside
// federation.New(): self-loop, duplicate target name, empty target name.
// Multi-hop A→B→A across remote instances is deferred to Phase 2 (US-0322
// topology API) and is covered by a documented Skip below.
//
// Tests call federation.New() directly — the unit-of-work for the cycle
// path. dpkms serve simply propagates the same error as a fatal startup
// failure, so subprocess wrapping adds nothing for this AC.
//
// Run: go test -tags "fts5 federation_e2e" -count=1 ./test/e2e/federation/...

package federation_e2e

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// newCycleSrc opens a SQLite source DB and returns the driver + on-disk
// path. The path is what cycle detection compares against target.URL.
func newCycleSrc(t *testing.T) (drv storage.StorageDriver, path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "self.db")
	d, err := storageutil.NewDriver("sqlite", path)
	if err != nil {
		t.Fatalf("newCycleSrc: open: %v", err)
	}
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("newCycleSrc: init: %v", err)
	}
	t.Cleanup(func() {
		if cerr := d.Close(context.Background()); cerr != nil {
			t.Logf("close src: %v", cerr)
		}
	})
	return d, path
}

// TestCycleDetection_SelfLoop_Rejected — federation.New() must reject a
// config whose target.URL points at the source storage path. Error must
// mention the cycle so operators can act.
func TestCycleDetection_SelfLoop_Rejected(t *testing.T) {
	src, srcPath := newCycleSrc(t)

	cfg := config.Config{
		Federations: []config.FederationEntry{
			{
				Name:     "self-loop",
				URL:      srcPath, // same path as storage → cycle
				SyncMode: "async",
				Interval: time.Minute,
			},
		},
	}
	cfg.Storage.Path = srcPath

	_, err := federation.New(cfg, src)
	if err == nil {
		t.Fatal("New: want self-loop cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q does not mention cycle", err.Error())
	}
}

// TestCycleDetection_DuplicateTargetNames_Rejected — two federation entries
// with the same Name must be rejected; otherwise watermarks/edges silently
// collide. Error must name the offender.
func TestCycleDetection_DuplicateTargetNames_Rejected(t *testing.T) {
	src, srcPath := newCycleSrc(t)

	tgtA := filepath.Join(t.TempDir(), "tgt-a.db")
	tgtB := filepath.Join(t.TempDir(), "tgt-b.db")

	cfg := config.Config{
		Federations: []config.FederationEntry{
			{Name: "dup", URL: tgtA, SyncMode: "async", Interval: time.Minute},
			{Name: "dup", URL: tgtB, SyncMode: "async", Interval: time.Minute},
		},
	}
	cfg.Storage.Path = srcPath

	_, err := federation.New(cfg, src)
	if err == nil {
		t.Fatal("New: want duplicate-name error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q does not mention duplicate", err.Error())
	}
	if !strings.Contains(err.Error(), "dup") {
		t.Errorf("error %q does not name offending target", err.Error())
	}
}

// TestCycleDetection_EmptyTargetName_Rejected — a federation entry with no
// Name is unaddressable; New() must reject before any worker starts.
func TestCycleDetection_EmptyTargetName_Rejected(t *testing.T) {
	src, srcPath := newCycleSrc(t)

	cfg := config.Config{
		Federations: []config.FederationEntry{
			{
				Name:     "",
				URL:      filepath.Join(t.TempDir(), "tgt.db"),
				SyncMode: "async",
				Interval: time.Minute,
			},
		},
	}
	cfg.Storage.Path = srcPath

	_, err := federation.New(cfg, src)
	if err == nil {
		t.Fatal("New: want empty-name error, got nil")
	}
	if !strings.Contains(err.Error(), "empty name") {
		t.Errorf("error %q does not mention empty name", err.Error())
	}
}

// TestCycleDetection_ValidNoLoop_Accepted — distinct names, distinct URLs,
// all distinct from storage path → New() must succeed and the WorkerSet
// must report one worker per async target ready to start.
func TestCycleDetection_ValidNoLoop_Accepted(t *testing.T) {
	src, srcPath := newCycleSrc(t)

	tgtA := filepath.Join(t.TempDir(), "tgt-a.db")
	tgtB := filepath.Join(t.TempDir(), "tgt-b.db")

	cfg := config.Config{
		Federations: []config.FederationEntry{
			{Name: "peer-a", URL: tgtA, SyncMode: "async", Interval: time.Minute},
			{Name: "peer-b", URL: tgtB, SyncMode: "async", Interval: time.Minute},
		},
	}
	cfg.Storage.Path = srcPath

	ws, err := federation.New(cfg, src)
	if err != nil {
		t.Fatalf("New: want nil error on valid DAG, got %v", err)
	}
	if ws == nil {
		t.Fatal("New: WorkerSet nil on valid DAG")
	}
	if got, want := ws.Len(), 2; got != want {
		t.Errorf("worker count: got %d, want %d", got, want)
	}
}

// TestCycleDetection_MultiHopABA_Phase2Deferred — multi-hop A→B→A across
// 3 instances is NOT detected by Phase 1 (only direct self-loop is). Phase
// 2 (US-0322) will query each peer's /federation/topology endpoint at
// startup to walk the full DAG. Until that lands, this test documents the
// gap and skips so CI doesn't pretend coverage exists where it doesn't.
func TestCycleDetection_MultiHopABA_Phase2Deferred(t *testing.T) {
	t.Skip("US-0318 multi-hop cycle (A→B→A across 3 instances) deferred to " +
		"Phase 2 — requires remote topology API per US-0322; Phase 1 only " +
		"detects direct self-loop, duplicate names, empty names")

	// Below documents the intended contract once the topology API lands.
	// Three instances: A=src, B=peer, C=peer-of-peer pointing back to A.
	// Phase 2 cycle detection should walk: A→B (B's federations include
	// C), B→C (C's federations include A) → cycle.
	src, srcPathA := newCycleSrc(t)
	srcPathB := filepath.Join(t.TempDir(), "b.db")

	cfg := config.Config{
		Federations: []config.FederationEntry{
			{Name: "a-to-b", URL: srcPathB, SyncMode: "async", Interval: time.Minute},
		},
	}
	cfg.Storage.Path = srcPathA

	// Today: New() succeeds because Phase 1 only sees A→B locally.
	// Phase 2: would query B's topology, discover B→C→A, and reject.
	if _, err := federation.New(cfg, src); err == nil {
		t.Log("Phase 1: A→B accepted (cannot see B's onward federations)")
	}
}
