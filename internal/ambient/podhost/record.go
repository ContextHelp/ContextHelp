// Package podhost launches and controls the ambient capture daemon (ctxd) on
// an existing pod through internal/podrun.
package podhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RecordFileName is the local 0600 cache record stored under the laptop's
// ambient state directory.
const RecordFileName = "ctxd.pod.json"

// Record caches the last known pod-side ctxd control port and bearer token so
// status/stop/tail/buffer verbs can open a short-lived forward without
// re-reading the remote run file on every call. The pod-side lock and run file
// remain authoritative; if a cached forward/call fails, the host refreshes
// from the remote run file once.
type Record struct {
	Version        int       `json:"version"`
	Pod            string    `json:"pod"`
	Provider       string    `json:"provider"`
	RemoteStateDir string    `json:"remote_state_dir"`
	ControlPort    int       `json:"control_port"`
	Token          string    `json:"token"`
	PID            int       `json:"pid,omitempty"`
	StartedAt      time.Time `json:"started_at"`
}

// RunInfo is the downstream seam shared by local and pod-backed ambient daemon
// verbs. In pod mode Control is the local loopback end of the active forward
// (127.0.0.1:<localPort>), Launcher is "pod <name>", and Pod names the pod.
type RunInfo struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Launcher  string    `json:"launcher"`
	Build     string    `json:"build,omitempty"`
	Instance  string    `json:"instance,omitempty"`
	Endpoint  string    `json:"endpoint,omitempty"`
	Control   string    `json:"control"`
	Token     string    `json:"token"`
	Pod       string    `json:"pod,omitempty"`
}

// RecordPath returns the path to ctxd.pod.json under stateDir.
func RecordPath(stateDir string) string {
	return filepath.Join(stateDir, RecordFileName)
}

// WriteRecord writes rec atomically with 0600 permissions.
func WriteRecord(stateDir string, rec Record) error {
	if rec.Version == 0 {
		rec.Version = 1
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("ambient podhost: mkdir state dir: %w", err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("ambient podhost: encode record: %w", err)
	}
	tmp, err := os.CreateTemp(stateDir, ".ctxd.pod.*.tmp")
	if err != nil {
		return fmt.Errorf("ambient podhost: temp record: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("ambient podhost: chmod record: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("ambient podhost: write record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("ambient podhost: close record: %w", err)
	}
	if err := os.Rename(tmpPath, RecordPath(stateDir)); err != nil {
		cleanup()
		return fmt.Errorf("ambient podhost: commit record: %w", err)
	}
	return nil
}

// ReadRecord reads ctxd.pod.json from stateDir. Returns (nil, nil) if absent.
func ReadRecord(stateDir string) (*Record, error) {
	data, err := os.ReadFile(RecordPath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ambient podhost: read record: %w", err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("ambient podhost: parse record: %w", err)
	}
	return &rec, nil
}

// DeleteRecord removes ctxd.pod.json if present.
func DeleteRecord(stateDir string) error {
	err := os.Remove(RecordPath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
