// Package upgrade owns the in-flight upgrade state machine for ADR-070
// bucket upgrades (ADR-070 Phase 2).
//
// State is in-memory. The daemon serves it on /healthz and in the
// X-Dpkms-Upgrade header on /api/v1 responses (header.go), which the CLI
// banner reads. Writers (the daemon, while running an upgrade bucket) call
// Start, Tick, Complete or Fail.
//
// A Manager built with a path also writes the state as JSON to that
// shadow file, conventionally $XDG_DATA_HOME/contexthelp/run/upgrade-state.json.
// Nothing in ctxt or dpkms reads it any more; the write stays until the
// local `ctxt upgrade run` moves to the API.
//
// State machine:
//
//	idle ──Start──▶ in_progress ──Tick──▶ in_progress
//	                    │              \──Complete──▶ idle
//	                    └─────Fail──────▶ failed ──Start──▶ in_progress
//
// awaiting_consent is reserved for future bucket-3 flows where the
// daemon refuses to run until the operator types `--i-understand-the-cost
// <sha>`. Nothing transitions into it but the type is exported so the
// CLI rendering paths stay forward-compatible.
package upgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	// to run it until operator confirms (reserved).
	StateAwaitingConsent State = "awaiting_consent"
	// StateFailed — the most recent run failed. The state (and the upgrade
	// header) persists so the CLI banner keeps reminding the operator until
	// the next run.
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
	// BucketEmbeddingsMigrate — background re-embed of the objects missing
	// rows for one registered embedding model (ADR-071 "Migration job").
	// Status.Target names the model.
	BucketEmbeddingsMigrate Bucket = "embeddings_migrate"
)

// Status is the wire form consumed by /healthz, the CLI, and the shadow
// file. JSON tags use omitempty so an idle Status serialises to a tiny
// {"state":"idle"} document.
type Status struct {
	State      State     `json:"state"`
	Bucket     Bucket    `json:"bucket,omitempty"`
	Target     string    `json:"target,omitempty"`
	Progress   float64   `json:"progress,omitempty"`
	Done       int       `json:"done,omitempty"`
	Total      int       `json:"total,omitempty"`
	Failed     int       `json:"failed,omitempty"`
	EtaSeconds int       `json:"eta_seconds,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
	// Deferred lists the pipelines whose objects an automatic run left
	// alone because their work would not stay on this machine. Skipped
	// counts, by reason, the objects the latest automatic run left as
	// stored. Both outlive the run, so an idle daemon still shows them.
	Deferred []Deferred     `json:"deferred,omitempty"`
	Skipped  map[string]int `json:"skipped,omitempty"`
	// Stopped lists the groups the latest automatic run stopped on the way
	// (five tries in a row): the rest of the run went on, their remaining
	// objects wait for a later start.
	Stopped []Stopped `json:"stopped,omitempty"`
}

// Stopped is one pipeline group an automatic re-derive stopped: what kept
// failing (llm, vision, embedding, fetch, refresh rejected, unknown) and the
// last cause, with no content or credentials.
type Stopped struct {
	Pipeline string `json:"pipeline"`
	Kind     string `json:"kind"`
	Cause    string `json:"cause"`
	// Groups is how many groups of the pipeline stopped, when more than one;
	// Kind and Cause are the first's.
	Groups int `json:"groups,omitempty"`
}

// Deferred is one pipeline an automatic re-derive did not process: its
// Objects stay as stored, for Reason.
type Deferred struct {
	Pipeline string `json:"pipeline"`
	Objects  int    `json:"objects"`
	Reason   string `json:"reason"`
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
	last       RunSummary
	shadowPath string
	now        func() time.Time // injectable for tests
	// deferred and skipped are what the latest automatic re-derive left
	// alone: they outlive its run and are shown in the snapshot (and the
	// shadow file) of an idle daemon or of a re-derive, never of another run.
	deferred []Deferred
	skipped  map[string]int
	stopped  []Stopped
}

// ErrNotRunning is returned by Tick / Complete / Fail when the manager is
// not in the StateInProgress state. Callers can errors.Is-check it.
var ErrNotRunning = errors.New("upgrade: no upgrade is in progress")

// ErrBusy is returned by Start / StartTarget while another run is in
// progress. Callers can errors.Is-check it.
var ErrBusy = errors.New("upgrade: another run is in progress")

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
	return m.StartTarget(bucket, "", total)
}

// StartTarget is Start for a run that works on one named target, such as
// the embedding model an embeddings_migrate run fills.
func (m *Manager) StartTarget(bucket Bucket, target string, total int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status.State == StateInProgress {
		return fmt.Errorf("%w: %s is already in progress", ErrBusy, m.status.Bucket)
	}
	if total < 0 {
		return fmt.Errorf("upgrade: total must be >= 0 (got %d)", total)
	}

	m.status = Status{
		State:     StateInProgress,
		Bucket:    bucket,
		Target:    target,
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
	return m.tickLocked(done)
}

// TickFailed is Tick for runs that skip objects they could not process:
// done counts every object handled, failed the subset that failed.
func (m *Manager) TickFailed(done, failed int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status.State != StateInProgress {
		return ErrNotRunning
	}
	if done < 0 || failed < 0 || failed > done {
		return fmt.Errorf("upgrade: need 0 <= failed <= done (got done %d, failed %d)", done, failed)
	}
	m.status.Failed = failed
	return m.tickLocked(done)
}

// tickLocked records done and recomputes progress and ETA. Must be called
// with m.mu held.
func (m *Manager) tickLocked(done int) error {
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

// SetDeferred records the pipelines an automatic run leaves alone, for the
// status to show: while it runs, and after it, idle or not. A run that
// defers everything starts nothing, so the list must not need a run. An
// empty list clears it.
func (m *Manager) SetDeferred(d []Deferred) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deferred = append([]Deferred(nil), d...)
	return m.persistIfRunningLocked()
}

// SetStopped records the groups the latest automatic run stopped, like
// SetDeferred: in any state, an empty list clears it.
func (m *Manager) SetStopped(st []Stopped) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = append([]Stopped(nil), st...)
	return m.persistIfRunningLocked()
}

// SetSkipped records, by reason, the objects the latest automatic run left
// as stored. Like SetDeferred it holds in any state; an empty map clears it.
func (m *Manager) SetSkipped(c map[string]int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skipped = nil
	if len(c) > 0 {
		m.skipped = maps.Clone(c)
	}
	return m.persistIfRunningLocked()
}

// persistIfRunningLocked writes the shadow file only while a run is in
// progress or failed: idle has no shadow.
func (m *Manager) persistIfRunningLocked() error {
	if m.status.State == StateIdle || m.status.State == "" {
		return nil
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

	m.status = m.idleLocked()
	return m.removeShadowLocked()
}

// idleLocked is the idle status.
func (m *Manager) idleLocked() Status { return Status{State: StateIdle} }

// withRederiveLocked adds what the automatic re-derive left to a status:
// when idle, or while a re-derive (bucket reingest_selective) runs. Another
// run's status, a re-projection's say, never carries it.
func (m *Manager) withRederiveLocked(st Status) Status {
	if st.State == StateIdle || st.State == "" || st.Bucket == BucketReingestSelective {
		st.Deferred = append([]Deferred(nil), m.deferred...)
		st.Skipped = maps.Clone(m.skipped)
		st.Stopped = append([]Stopped(nil), m.stopped...)
	}
	return st
}

// CompleteRun is Complete that also keeps the run's summary, readable by
// LastRun after the status has gone back to idle. The summary is in
// memory only: it is not part of Status, whose shape is a contract.
func (m *Manager) CompleteRun(sum RunSummary) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State != StateInProgress {
		return ErrNotRunning
	}
	m.status = m.idleLocked()
	m.last = sum
	return m.removeShadowLocked()
}

// LastRun is the summary of the most recent run CompleteRun recorded.
func (m *Manager) LastRun() RunSummary {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last
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
	return m.withRederiveLocked(m.status)
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
	data, err := json.MarshalIndent(m.withRederiveLocked(m.status), "", "  ")
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
