package lint_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/lint"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestCheckOrphans(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	// orphan: no tags, no mentions, no metadata
	orphan := &storage.KnowledgeObject{
		ID:         "orphan-1",
		Type:       "text",
		RawContent: "hello",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := driver.Objects().Create(ctx, orphan); err != nil {
		t.Fatal(err)
	}

	// not orphan: has tags
	tagged := &storage.KnowledgeObject{
		ID:         "tagged-1",
		Type:       "text",
		RawContent: "world",
		Tags:       []storage.Tag{{Label: "test"}},
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := driver.Objects().Create(ctx, tagged); err != nil {
		t.Fatal(err)
	}

	cfg := lint.DefaultConfig()
	cfg.Checks = []string{"orphans"}
	l := lint.New(driver, cfg)

	report, err := l.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Issues) != 1 {
		t.Fatalf("expected 1 orphan issue, got %d", len(report.Issues))
	}
	if report.Issues[0].ObjectID != "orphan-1" {
		t.Errorf("expected orphan-1, got %s", report.Issues[0].ObjectID)
	}
	if report.Issues[0].Check != "orphans" {
		t.Errorf("expected check=orphans, got %s", report.Issues[0].Check)
	}
}

func TestCheckMissingMetadata(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	noMeta := &storage.KnowledgeObject{
		ID:         "no-meta-1",
		Type:       "text",
		RawContent: "content",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := driver.Objects().Create(ctx, noMeta); err != nil {
		t.Fatal(err)
	}

	hasMeta := &storage.KnowledgeObject{
		ID:         "has-meta-1",
		Type:       "text",
		RawContent: "content",
		Metadata: map[string]any{
			"enrichment.structured_metadata": map[string]any{"topic": "test"},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := driver.Objects().Create(ctx, hasMeta); err != nil {
		t.Fatal(err)
	}

	cfg := lint.DefaultConfig()
	cfg.Checks = []string{"missing_metadata"}
	l := lint.New(driver, cfg)

	report, err := l.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(report.Issues))
	}
	if report.Issues[0].ObjectID != "no-meta-1" {
		t.Errorf("expected no-meta-1, got %s", report.Issues[0].ObjectID)
	}
}

func TestCheckStale(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	old := time.Now().AddDate(0, 0, -100)
	staleObj := &storage.KnowledgeObject{
		ID:         "stale-1",
		Type:       "text",
		RawContent: "old stuff",
		CreatedAt:  old,
		UpdatedAt:  old,
	}
	if err := driver.Objects().Create(ctx, staleObj); err != nil {
		t.Fatal(err)
	}

	fresh := &storage.KnowledgeObject{
		ID:         "fresh-1",
		Type:       "text",
		RawContent: "new stuff",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := driver.Objects().Create(ctx, fresh); err != nil {
		t.Fatal(err)
	}

	cfg := lint.DefaultConfig()
	cfg.Checks = []string{"stale"}
	cfg.StaleDays = 90
	l := lint.New(driver, cfg)

	report, err := l.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Issues) != 1 {
		t.Fatalf("expected 1 stale issue, got %d", len(report.Issues))
	}
	if report.Issues[0].ObjectID != "stale-1" {
		t.Errorf("expected stale-1, got %s", report.Issues[0].ObjectID)
	}
}

func TestLimitCapsIssues(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	// create 5 orphans
	for i := 0; i < 5; i++ {
		obj := &storage.KnowledgeObject{
			ID:         fmt.Sprintf("orphan-%d", i),
			Type:       "text",
			RawContent: "x",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		if err := driver.Objects().Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
	}

	cfg := lint.DefaultConfig()
	cfg.Checks = []string{"orphans"}
	cfg.Limit = 2
	l := lint.New(driver, cfg)

	report, err := l.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Issues) > 2 {
		t.Errorf("limit=2 but got %d issues", len(report.Issues))
	}
}

func TestAllChecksRunByDefault(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	cfg := lint.DefaultConfig()
	l := lint.New(driver, cfg)

	report, err := l.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Checks) != len(lint.AllChecks) {
		t.Errorf("expected %d checks, got %d", len(lint.AllChecks), len(report.Checks))
	}
}

func TestReportCounts(t *testing.T) {
	r := &lint.Report{
		Issues: []lint.Issue{
			{Severity: lint.SeverityWarning},
			{Severity: lint.SeverityWarning},
			{Severity: lint.SeverityInfo},
			{Severity: lint.SeverityError},
		},
	}
	counts := r.Counts()
	if counts[lint.SeverityWarning] != 2 {
		t.Errorf("expected 2 warnings, got %d", counts[lint.SeverityWarning])
	}
	if counts[lint.SeverityInfo] != 1 {
		t.Errorf("expected 1 info, got %d", counts[lint.SeverityInfo])
	}
	if counts[lint.SeverityError] != 1 {
		t.Errorf("expected 1 error, got %d", counts[lint.SeverityError])
	}
}
