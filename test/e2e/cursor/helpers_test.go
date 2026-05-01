//go:build cursor_e2e

// Package cursor exercises US-0409 named-cursor behaviour end-to-end.
//
// Two test surfaces:
//   - Cursor subcommand tests: exec the built `bin/ctxt` binary against a
//     temp $CTXT_CURSOR_FILE; covers cursor list/show/reset/set/delete.
//   - List + cursor composition tests: drive internal/service.ListObjects
//     directly with a SQLite test driver, mirroring runListWithCursor's
//     filter-mutation logic, and assert the cursor advances correctly.
//
// Both surfaces share testdata builders in this file.
package cursor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cursor"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/require"
)

// cursorEnv groups a temp cursor file path + a manager bound to it.
type cursorEnv struct {
	t        *testing.T
	path     string
	mgr      *cursor.Manager
	binEnv   []string
	binReady bool
}

func newCursorEnv(t *testing.T) *cursorEnv {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cursors.yaml")
	t.Setenv(cursor.EnvFile, path)
	return &cursorEnv{
		t:    t,
		path: path,
		mgr:  cursor.NewWithPath(path),
		binEnv: append(os.Environ(),
			cursor.EnvFile+"="+path,
			"HOME="+dir,
			"XDG_CONFIG_HOME="+filepath.Join(dir, ".config"),
		),
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod from %s", dir)
		}
		dir = parent
	}
}

func ctxtBin(t *testing.T) string {
	t.Helper()
	root := projectRoot(t)
	bin := filepath.Join(root, "bin", "ctxt")
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("bin/ctxt not built: %v (run `make build-ctxt`)", err)
	}
	return bin
}

// runBin executes bin/ctxt with the env's CTXT_CURSOR_FILE override.
// Returns combined stdout+stderr and exit code (0 if no error).
func (e *cursorEnv) runBin(args ...string) (string, int) {
	e.t.Helper()
	bin := ctxtBin(e.t)
	cmd := exec.Command(bin, args...) // #nosec G204 -- test helper
	cmd.Env = e.binEnv
	out, err := cmd.CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return string(out), ee.ExitCode()
		}
		return string(out) + "\n" + err.Error(), -1
	}
	return string(out), 0
}

// listKOs builds N graph KOs spaced 1s apart and inserts them into driver.
// Returns IDs in chronological order. Each call uses a monotonically later
// base so successive batches stack chronologically.
func listKOs(t *testing.T, driver storage.StorageDriver, n int, prefix string, mention string) []string {
	t.Helper()
	ctx := context.Background()
	base := nextBatchBase(t, n)
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s-%d", prefix, i)
		ids[i] = id
		ko := storageutil.BuildGraphKO(id, "text", fmt.Sprintf("body %d %s", i, mention))
		ko.CreatedAt = base.Add(time.Duration(i) * time.Second)
		ko.UpdatedAt = ko.CreatedAt
		require.NoError(t, driver.Objects().Create(ctx, ko))
	}
	return ids
}

// batchClock advances per-test so successive listKOs calls produce a strict
// chronological sequence; storage uses second-precision RFC3339 strings, so
// drifting forward in second-sized blocks keeps comparisons unambiguous.
type batchClock struct {
	next time.Time
}

func nextBatchBase(t *testing.T, n int) time.Time {
	t.Helper()
	bc := batchClockFor(t)
	out := bc.next
	bc.next = out.Add(time.Duration(n+5) * time.Second)
	return out
}

var batchClocks sync.Map // *testing.T → *batchClock

func batchClockFor(t *testing.T) *batchClock {
	t.Helper()
	if v, ok := batchClocks.Load(t); ok {
		return v.(*batchClock)
	}
	bc := &batchClock{next: time.Now().UTC().Truncate(time.Second).Add(-time.Hour)}
	batchClocks.Store(t, bc)
	t.Cleanup(func() { batchClocks.Delete(t) })
	return bc
}

// gateByCursor mirrors runListWithCursor's filter mutation: applies
// last_seen_at + 1s as filter.After (storage After is `>=` against
// second-precision RFC3339), forces ASC sort.
func gateByCursor(filter storage.ObjectFilter, c *cursor.Cursor) storage.ObjectFilter {
	if !c.LastSeenAt.IsZero() {
		gate := c.LastSeenAt.Add(time.Second)
		if filter.After == nil || gate.After(*filter.After) {
			filter.After = &gate
		}
	}
	filter.Sort = "created_at"
	filter.Dir = "asc"
	return filter
}

// parseJSON parses cursor list/show JSON output, ignoring fang banners.
func parseJSON(t *testing.T, out string, v any) {
	t.Helper()
	idx := strings.Index(out, "{")
	if idx < 0 {
		idx = strings.Index(out, "[")
	}
	require.GreaterOrEqual(t, idx, 0, "no JSON found in: %s", out)
	require.NoError(t, json.Unmarshal([]byte(out[idx:]), v))
}
