package upgrade

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ShadowStaleAfter is the freshness window for the shadow file. Readers
// (CLI banner) treat any shadow file last-modified more than this long ago
// as stale and pretend it doesn't exist — guards against the operator
// killing the daemon mid-upgrade and leaving a permanent banner behind.
//
// 30s matches the value documented in the banner package contract; pick a
// new constant ONLY if the daemon's Tick cadence drops materially below
// 10s (Tick is currently called per object, well under 1s per tick for
// typical workloads).
const ShadowStaleAfter = 30 * time.Second

// ReadShadow reads and decodes the upgrade-state shadow file at path.
// Returns the decoded Status plus the file's modtime so callers can
// implement their own staleness policy.
//
// Errors are wrapped:
//   - os.IsNotExist on the returned error means "no upgrade in progress"
//     (the canonical idle state).
//   - any other error is a genuine read/decode failure and should be
//     surfaced to the operator.
func ReadShadow(path string) (Status, time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Status{}, time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Status{}, info.ModTime(), fmt.Errorf("upgrade: read shadow: %w", err)
	}
	var st Status
	if err := json.Unmarshal(data, &st); err != nil {
		return Status{}, info.ModTime(), fmt.Errorf("upgrade: decode shadow: %w", err)
	}
	return st, info.ModTime(), nil
}

// ReadShadowFresh wraps ReadShadow with the standard staleness check.
// Returns (status, true, nil) when a fresh upgrade is in progress.
// Returns (zero, false, nil) when:
//   - the file does not exist (idle), or
//   - the file's modtime is older than ShadowStaleAfter (treat as idle).
//
// Any other error (permission denied, malformed JSON) is returned as-is.
func ReadShadowFresh(path string, now time.Time) (Status, bool, error) {
	st, modTime, err := ReadShadow(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Status{}, false, nil
		}
		return Status{}, false, err
	}
	if now.Sub(modTime) > ShadowStaleAfter {
		return Status{}, false, nil
	}
	return st, true, nil
}
