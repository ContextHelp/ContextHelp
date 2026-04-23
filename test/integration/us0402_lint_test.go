package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/lint"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestUS0402_LintOrphan(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	orphan := &storage.KnowledgeObject{
		ID:         "orphan-obj",
		Type:       "text",
		RawContent: "lonely content",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, driver.Objects().Create(ctx, orphan))

	cfg := lint.DefaultConfig()
	cfg.Checks = []string{"orphans"}
	report, err := lint.New(driver, cfg).Run(ctx)
	require.NoError(t, err)

	require.Len(t, report.Issues, 1)
	assert.Equal(t, "orphan-obj", report.Issues[0].ObjectID)
	assert.Equal(t, "orphans", report.Issues[0].Check)
	assert.Equal(t, lint.SeverityWarning, report.Issues[0].Severity)
}

func TestUS0402_LintMissingMetadata(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	obj := &storage.KnowledgeObject{
		ID:         "no-meta",
		Type:       "text",
		RawContent: "some text",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, driver.Objects().Create(ctx, obj))

	cfg := lint.DefaultConfig()
	cfg.Checks = []string{"missing_metadata"}
	report, err := lint.New(driver, cfg).Run(ctx)
	require.NoError(t, err)

	require.Len(t, report.Issues, 1)
	assert.Equal(t, "no-meta", report.Issues[0].ObjectID)
	assert.Equal(t, "missing_metadata", report.Issues[0].Check)
}

func TestUS0402_LintCheckFilter(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	// create an orphan that would also be flagged as missing_metadata
	obj := &storage.KnowledgeObject{
		ID:         "multi-issue",
		Type:       "text",
		RawContent: "x",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, driver.Objects().Create(ctx, obj))

	// only run orphans check
	cfg := lint.DefaultConfig()
	cfg.Checks = []string{"orphans"}
	report, err := lint.New(driver, cfg).Run(ctx)
	require.NoError(t, err)

	for _, iss := range report.Issues {
		assert.Equal(t, "orphans", iss.Check, "only orphans check should run")
	}
}

func TestUS0402_LintAuditLog(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	cfg := lint.DefaultConfig()
	report, err := lint.New(driver, cfg).Run(ctx)
	require.NoError(t, err)

	// simulate what the CLI does: log to audit
	entry := &storage.AuditEntry{
		ID:        "lint-test",
		EventType: "lint.run",
		Actor:     "test",
		Payload: map[string]any{
			"checks":   report.Checks,
			"issues":   len(report.Issues),
			"duration": report.Duration,
		},
		CreatedAt: time.Now(),
	}
	require.NoError(t, driver.AuditLog().Append(ctx, entry))

	entries, _, err := driver.AuditLog().List(ctx, storage.AuditFilter{
		EventType: "lint.run",
	})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "lint.run", entries[0].EventType)
}
