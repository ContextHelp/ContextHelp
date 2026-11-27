package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// failSecond is a reanalyzer that fails its second call.
type failSecond struct {
	mu    sync.Mutex
	calls int
}

func (f *failSecond) ReanalyzeObject(_ context.Context, id string) (string, float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls == 2 {
		return "", 0, errors.New("step exploded on " + id)
	}
	return "text.short@v1", 0, nil
}

// `upgrade run` prints processed, failed and skipped counts and exits
// non-zero when any object failed; the run itself goes on past the failure.
func TestUpgradeRunRegressions_FailedObjectCountsAndExit(t *testing.T) {
	for _, f := range []string{"filter", "where", "all", "i-understand-the-cost", "dry-run"} {
		_ = upgradeRunCmd.Flags().Set(f, "")
	}
	_ = upgradeRunCmd.Flags().Set("all", "false")
	_ = upgradeRunCmd.Flags().Set("dry-run", "false")

	db := setupTestDB(t)
	for _, id := range []string{"ur-1", "ur-2", "ur-3"} {
		seedReingestObject(t, db, id, "text.short@v0")
	}
	fake := &failSecond{}
	prev := upgradeReanalyzer
	upgradeReanalyzer = func(*service.Service) interface {
		ReanalyzeObject(ctx context.Context, id string) (string, float64, error)
	} {
		return fake
	}
	t.Cleanup(func() { upgradeReanalyzer = prev })

	out, err := db.exec("upgrade", "run", "--filter", "pipeline=text.short@v0")
	require.Error(t, err, "a failed object must make the command exit non-zero")
	assert.Contains(t, err.Error(), "1 objects failed")
	assert.Contains(t, out, "2 processed, 1 failed, 0 skipped", "out: %s", out)
	assert.Equal(t, 3, fake.calls, "the run stopped at the failing object")
}

// An over-budget run prints the counts it reached and exits non-zero.
func TestUpgradeRunRegressions_BudgetStopPrintsCounts(t *testing.T) {
	for _, f := range []string{"filter", "where", "i-understand-the-cost"} {
		_ = upgradeRunCmd.Flags().Set(f, "")
	}
	_ = upgradeRunCmd.Flags().Set("all", "false")
	_ = upgradeRunCmd.Flags().Set("dry-run", "false")

	db := setupTestDB(t)
	for _, id := range []string{"ub-1", "ub-2", "ub-3"} {
		seedReingestObject(t, db, id, "text.short@v0")
	}
	prev := upgradeReanalyzer
	upgradeReanalyzer = func(*service.Service) interface {
		ReanalyzeObject(ctx context.Context, id string) (string, float64, error)
	} {
		return costly{}
	}
	t.Cleanup(func() {
		upgradeReanalyzer = prev
		_ = upgradeRunCmd.Flags().Set("budget", "0")
	})

	out, err := db.exec("upgrade", "run", "--filter", "pipeline=text.short@v0", "--budget", "0.05")
	require.Error(t, err)
	assert.Contains(t, out, "Re-ingest stopped")
	assert.Contains(t, out, "1 processed, 0 failed, 0 skipped", "out: %s", out)
}

type costly struct{}

func (costly) ReanalyzeObject(context.Context, string) (string, float64, error) {
	return "text.short@v1", 0.10, nil
}

// In structured output the stopped summary is part of the document, not
// a text line before it.
func TestUpgradeRunRegressions_StoppedSummaryIsJSON(t *testing.T) {
	for _, f := range []string{"filter", "where", "i-understand-the-cost"} {
		_ = upgradeRunCmd.Flags().Set(f, "")
	}
	_ = upgradeRunCmd.Flags().Set("all", "false")
	_ = upgradeRunCmd.Flags().Set("dry-run", "false")

	db := setupTestDB(t)
	for _, id := range []string{"uj-1", "uj-2"} {
		seedReingestObject(t, db, id, "text.short@v0")
	}
	prev := upgradeReanalyzer
	upgradeReanalyzer = func(*service.Service) interface {
		ReanalyzeObject(ctx context.Context, id string) (string, float64, error)
	} {
		return costly{}
	}
	t.Cleanup(func() {
		upgradeReanalyzer = prev
		_ = upgradeRunCmd.Flags().Set("budget", "0")
	})

	out, err := db.exec("upgrade", "run", "--filter", "pipeline=text.short@v0", "--budget", "0.05", "--format", "json")
	require.Error(t, err)
	var doc struct {
		Stopped   bool `json:"stopped"`
		Processed int  `json:"processed"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &doc), "stdout must be one JSON document: %q", out)
	assert.True(t, doc.Stopped)
	assert.Equal(t, 1, doc.Processed)
}
