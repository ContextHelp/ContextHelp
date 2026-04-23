// Package lint provides automated health checks for the knowledge graph.
package lint

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Severity classifies a lint issue.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Issue is a single lint finding.
type Issue struct {
	Check    string   `json:"check"`
	Severity Severity `json:"severity"`
	ObjectID string   `json:"object_id"`
	Message  string   `json:"message"`
}

// Report is the output of a lint run.
type Report struct {
	Checks   []string `json:"checks"`
	Issues   []Issue  `json:"issues"`
	Duration string   `json:"duration"`
}

// Counts returns issue counts by severity.
func (r *Report) Counts() map[Severity]int {
	m := map[Severity]int{}
	for _, iss := range r.Issues {
		m[iss.Severity]++
	}
	return m
}

// Config controls which checks run and their parameters.
type Config struct {
	Checks          []string      // empty = all
	ProfileID       string        // scope to profile
	Limit           int           // max issues to report (0 = unlimited)
	StaleDays       int           // days before an object is considered stale
	DuplicateThresh float64       // cosine similarity threshold for near-dupes
	Timeout         time.Duration // per-check timeout
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		StaleDays:       90,
		DuplicateThresh: 0.95,
		Timeout:         30 * time.Second,
	}
}

// AllChecks lists the built-in check names.
var AllChecks = []string{"orphans", "missing_metadata", "duplicates", "stale"}

// Linter runs lint checks against the knowledge graph.
type Linter struct {
	driver storage.StorageDriver
	cfg    Config
}

// New creates a Linter backed by the given storage driver.
func New(driver storage.StorageDriver, cfg Config) *Linter {
	return &Linter{driver: driver, cfg: cfg}
}

// Run executes the configured checks and returns a report.
func (l *Linter) Run(ctx context.Context) (*Report, error) {
	start := time.Now()

	checks := l.cfg.Checks
	if len(checks) == 0 {
		checks = AllChecks
	}

	report := &Report{Checks: checks}

	for _, name := range checks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		issues, err := l.runCheck(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", name, err)
		}
		report.Issues = append(report.Issues, issues...)
		if l.cfg.Limit > 0 && len(report.Issues) >= l.cfg.Limit {
			report.Issues = report.Issues[:l.cfg.Limit]
			break
		}
	}

	report.Duration = time.Since(start).Round(time.Millisecond).String()
	return report, nil
}

func (l *Linter) runCheck(ctx context.Context, name string) ([]Issue, error) {
	switch name {
	case "orphans":
		return l.checkOrphans(ctx)
	case "missing_metadata":
		return l.checkMissingMetadata(ctx)
	case "duplicates":
		return l.checkDuplicates(ctx)
	case "stale":
		return l.checkStale(ctx)
	default:
		return nil, fmt.Errorf("unknown check %q", name)
	}
}
