// Package dpkmspod launches, inspects, and shuts down a dpkms instance on an
// existing pod through internal/podrun.
package dpkmspod

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Record stores the local state for a pod-hosted dpkms instance under the
// laptop's state directory (never under <data_dir>/run/, so local dpkms ps
// never confuses it with a local pidfile).
type Record struct {
	Version          int       `json:"version"`
	Pod              string    `json:"pod"`
	Provider         string    `json:"provider"`
	Name             string    `json:"name,omitempty"`
	DpkmsPort        int       `json:"dpkms_port"`
	LocalPort        int       `json:"local_port"`
	ForwardPID       int       `json:"forward_pid,omitempty"`
	ForwardStartedAt time.Time `json:"forward_started_at,omitempty"`
	DataDir          string    `json:"data_dir"`
	StartedAt        time.Time `json:"started_at"`
}

// RecordPath returns the 0600 local record path for podName inside stateDir.
func RecordPath(stateDir, podName string) string {
	return filepath.Join(stateDir, fmt.Sprintf("dpkms.pod.%s.json", podName))
}

// WriteRecord atomically writes rec with 0600 permissions.
func WriteRecord(stateDir string, rec Record) error {
	if rec.Version == 0 {
		rec.Version = 1
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("dpkms pod record: mkdir state dir: %w", err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("dpkms pod record: encode: %w", err)
	}
	tmp, err := os.CreateTemp(stateDir, ".dpkms.pod.*.tmp")
	if err != nil {
		return fmt.Errorf("dpkms pod record: temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("dpkms pod record: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("dpkms pod record: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("dpkms pod record: close: %w", err)
	}
	if err := os.Rename(tmpPath, RecordPath(stateDir, rec.Pod)); err != nil {
		cleanup()
		return fmt.Errorf("dpkms pod record: rename: %w", err)
	}
	return nil
}

// ReadRecord loads the local record for podName. Returns (nil, nil) if absent.
func ReadRecord(stateDir, podName string) (*Record, error) {
	data, err := os.ReadFile(RecordPath(stateDir, podName)) // #nosec G304 -- record path is scoped to stateDir + validated pod token
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dpkms pod record: read: %w", err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("dpkms pod record: decode: %w", err)
	}
	return &rec, nil
}

// DeleteRecord removes the local record for podName if present.
func DeleteRecord(stateDir, podName string) error {
	err := os.Remove(RecordPath(stateDir, podName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
