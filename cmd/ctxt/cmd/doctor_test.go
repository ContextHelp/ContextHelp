package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lint"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestDoctorHelp(t *testing.T) {
	out, err := executeCommand("doctor", "--help")
	if err != nil {
		t.Fatalf("doctor --help should succeed: %v", err)
	}
	for _, want := range []string{"doctor", "--check", "--limit", "--profile"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestDoctorText(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("doctor")
	if err != nil {
		t.Fatalf("doctor should succeed: %v", err)
	}
	if !strings.Contains(out, "Lint checks") {
		t.Errorf("expected doctor header in output, got: %s", out)
	}
}

func TestDoctorJSON(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("--format", "json", "doctor")
	if err != nil {
		t.Fatalf("doctor --output json should succeed: %v", err)
	}
	for _, want := range []string{`"checks"`, `"issues"`, `"duration"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing key %q", want)
		}
	}
}

func TestDoctorCheckFlag(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("--format", "json", "doctor", "--check", "orphans")
	if err != nil {
		t.Fatalf("doctor --check orphans should succeed: %v", err)
	}
	if !strings.Contains(out, `"orphans"`) {
		t.Errorf("expected orphans check in output")
	}
	// should not contain other check names in the checks array
	if strings.Contains(out, `"stale"`) {
		t.Errorf("should not run stale check when only orphans requested")
	}
}

// doctor reads the store read-only, so it cannot record its doctor.run
// audit entry: it says so instead of failing the write silently, and
// writes nothing.
func TestDoctorReadOnlySkipsAudit(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	if !strings.Contains(out, "doctor.run not recorded in the audit log: the store is open read-only") {
		t.Errorf("doctor does not say the audit entry was skipped:\n%s", out)
	}
	ctx := context.Background()
	_, n, err := db.Driver.AuditLog().List(ctx, storage.AuditFilter{EventType: "doctor.run"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d doctor.run entries written on a read-only open", n)
	}
}

// On a writable store the doctor.run entry is
// written, with no note.
func TestDoctorWritableRecordsAudit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	var w strings.Builder
	recordLintReport(ctx, &w, db.Driver, &lint.Report{Checks: []string{"orphans"}})
	if w.Len() != 0 {
		t.Errorf("unexpected output on a writable store: %s", w.String())
	}
	_, n, err := db.Driver.AuditLog().List(ctx, storage.AuditFilter{EventType: "doctor.run"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d doctor.run entries, want 1", n)
	}
}
