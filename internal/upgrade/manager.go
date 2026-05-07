// Package upgrade owns the in-flight upgrade state machine for ADR-070
// bucket upgrades (T-0580, ADR-070 Phase 2).
//
// State is in-memory and JSON-shadowed to disk for the CLI banner so each
// CLI invocation can decide whether to print the upgrade-in-progress banner
// without paying an HTTP round-trip to /healthz on the daemon. Writers
// (the daemon, while running an upgrade bucket) call Start, Tick, Complete
// or Fail; readers (CLI banner middleware) call ReadShadow on the file
// directly with no Manager instance.
//
// The shadow file lives at $XDG_DATA_HOME/contexthelp/run/upgrade-state.json
// (the same RunDir() used for pidfiles per internal/config/config.go).
//
// State machine:
//
//	idle ──Start──▶ in_progress ──Tick──▶ in_progress
//	                    │              \──Complete──▶ idle
//	                    └─────Fail──────▶ failed ──Start──▶ in_progress
//
// awaiting_consent is reserved for future bucket-3 flows (T-0581+) where the
// daemon refuses to run until the operator types `--i-understand-the-cost
// <sha>`. T-0580 does not transition into it but the type is exported so the
// CLI rendering paths stay forward-compatible.
package upgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State enumerates the upgrade-state machine vertices. The wire form is a
// lower_snake_case string consumed by both /healthz and the CLI.
type State string

const (
	// StateIdle — no upgrade is running. Shadow file SHOULD NOT exist.
	StateIdle State = "idle"
	// StateInProgress — daemon is actively working a bucket. Tick updates
	// progress.
	StateInProgress State = "in_progress"
	// StateAwaitingConsent — bucket-3 work is queued but the daemon refuses
	// to run it until operator confirms (reserved for T-0581+).
	StateAwaitingConsent State = "awaiting_consent"
	// StateFailed — the most recent run failed. Shadow file persists so the
	// CLI banner keeps reminding the operator until they acknowledge.
	StateFailed State = "failed"
)

// Bucket enumerates the three ADR-070 upgrade buckets. The wire form is a
// lower_snake_case string consumed by /healthz, the CLI, and (eventually)
// the eva contract.
type Bucket string

const (
	// BucketReindexAuto — silent automatic reindex. Daemon detects signature
	// mismatch and rebuilds the FTS / vector index in place.
	BucketReindexAuto Bucket = "reindex_auto"
	// BucketReingestSelective — re-runs the pipeline against the original
	// RawContent for a SQL-predicate-narrow subset of objects.
	BucketReingestSelective Bucket = "reingest_selective"
	// BucketReingestAll — full-corpus re-ingest. Requires explicit operator
	// consent via --i-understand-the-cost <sha>.
	BucketReingestAll Bucket = "reingest_all"
)

// Status is the wire form consumed by /healthz, the CLI, and the shadow
// file. JSON tags use omitempty so an idle Status serialises to a tiny
// {"state":"idle"} document.
type Status struct {
	State      State     `json:"state"`
	Bucket     Bucket    `json:"bucket,omitempty"`
	Progress   float64   `json:"progress,omitempty"`
	Done       int       `json:"done,omitempty"`
	Total      int       `json:"total,omitempty"`
	EtaSeconds int       `json:"eta_seconds,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
}

// Manager owns the upgrade-state machine for a single dpkms process.
// Only one Manager should exist per process.
//
// Thread-safety: every public method takes the same internal lock. Tick is
// the hot path during an upgrade and is cheap (one map write + one shadow
// rewrite); contention is not expected in practice since only the bucket
// worker calls it.
type Manager struct {
	mu         sync.Mutex
	status     Status
	shadowPath string
	now        func() time.Time // injectable for tests
}

// ErrNotRunning is returned by Tick / Complete / Fail when the manager is
// not in the StateInProgress state. Callers can errors.Is-check it.
var ErrNotRunning = errors.New("upgrade: no upgrade is in progress")

// NewManager constructs a Manager that shadows its state to shadowPath.
// Empty shadowPath disables shadowing (useful for tests that don't want a
// stray file). The caller is responsible for ensuring the parent directory
// of shadowPath exists when shadowing is desired.
func NewManager(shadowPath string) *Manager {
	return &Manager{
		shadowPath: shadowPath,
		status:     Status{State: StateIdle},
		now:        time.Now,
	}
}

// Start begins a new upgrade run with the given bucket and total. It moves
// the state machine from idle (or failed — a fresh attempt is allowed) to
// in_progress. Returns an error if a run is already in flight.
func (m *Manager) Start(bucket Bucket, total int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status.State == StateInProgress {
		return fmt.Errorf("upgrade: another %s run is already in progress", m.status.Bucket)
	}
	if total < 0 {
		return fmt.Errorf("upgrade: total must be >= 0 (got %d)", total)
	}

	m.status = Status{
		State:     StateInProgress,
		Bucket:    bucket,
		Total:     total,
		Done:      0,
		StartedAt: m.now(),
	}
	return m.persistLocked()
}

// Tick records progress. done is the cumulative count of completed objects.
// ETA is recomputed by linear extrapolation from elapsed time and remaining
// work; if total is zero (an unbounded run) ETA is left as zero.
func (m *Manager) Tick(done int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status.State != StateInProgress {
		return ErrNotRunning
	}
	if done < 0 {
		return fmt.Errorf("upgrade: done must be >= 0 (got %d)", done)
	}
	m.status.Done = done

	if m.status.Total > 0 {
		m.status.Progress = float64(done) / float64(m.status.Total)
		if m.status.Progress > 1 {
			m.status.Progress = 1
		}
		m.status.EtaSeconds = computeETA(done, m.status.Total, m.status.StartedAt, m.now())
	}

	return m.persistLocked()
}

// Complete moves the state machine to idle and removes the shadow file
// (idle implies "nothing to display"). Returns ErrNotRunning if no run is
// in progress.
func (m *Manager) Complete() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status.State != StateInProgress {
		return ErrNotRunning
	}

	m.status = Status{State: StateIdle}
	return m.removeShadowLocked()
}

// Fail moves the state machine to failed, retaining the bucket / progress
// fields so an operator inspecting status sees what was running when it
// died. The error message is recorded in LastError; nil err is rejected.
func (m *Manager) Fail(err error) error {
	if err == nil {
		return fmt.Errorf("upgrade: Fail requires a non-nil error")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status.State != StateInProgress {
		return ErrNotRunning
	}
	m.status.State = StateFailed
	m.status.LastError = err.Error()
	return m.persistLocked()
}

// Snapshot returns a copy of the current Status. Cheap; callers MUST NOT
// mutate the returned value (it is by-value but a future field addition
// may include slices).
func (m *Manager) Snapshot() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// computeETA estimates remaining seconds via linear extrapolation. Returns
// 0 when no progress has been made (avoids divide-by-zero) or when the run
// is at/past completion.
func computeETA(done, total int, startedAt, now time.Time) int {
	if done <= 0 || total <= 0 || done >= total {
		return 0
	}
	elapsed := now.Sub(startedAt)
	if elapsed <= 0 {
		return 0
	}
	perItem := elapsed / time.Duration(done)
	remaining := perItem * time.Duration(total-done)
	secs := int(remaining.Seconds())
	if secs < 0 {
		return 0
	}
	return secs
}

// persistLocked writes the current status to the shadow file. Must be
// called with m.mu held. No-op when shadowPath is empty.
func (m *Manager) persistLocked() error {
	if m.shadowPath == "" {
		return nil
	}
	data, err := json.MarshalIndent(m.status, "", "  ")
	if err != nil {
		return fmt.Errorf("upgrade: marshal shadow: %w", err)
	}
	// Write to a temp file in the same directory and rename for atomicity
	// — readers may be tailing the file mid-write otherwise.
	dir := filepath.Dir(m.shadowPath)
	tmp, err := os.CreateTemp(dir, ".upgrade-state-*.json")
	if err != nil {
		return fmt.Errorf("upgrade: create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("upgrade: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("upgrade: close temp: %w", err)
	}
	if err := os.Rename(tmpName, m.shadowPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("upgrade: rename shadow: %w", err)
	}
	return nil
}

// removeShadowLocked deletes the shadow file. Must be called with m.mu held.
// No-op when shadowPath is empty or the file is already gone.
func (m *Manager) removeShadowLocked() error {
	if m.shadowPath == "" {
		return nil
	}
	if err := os.Remove(m.shadowPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("upgrade: remove shadow: %w", err)
	}
	return nil
}
