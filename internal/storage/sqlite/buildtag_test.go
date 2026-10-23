package sqlite

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestBuildWithoutFTS5TagFails asserts a build that leaves out the fts5 tag
// cannot produce a binary. Every migration creates FTS5 tables, so a binary
// without the tag can only fail at run time; sqlite3_nofts5.go turns that
// into a compile error, and this test keeps it one. It runs under -short,
// so the CI unit job enforces it.
func TestBuildWithoutFTS5TagFails(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go binary not on PATH")
	}
	cmd := exec.Command(gobin, "build", "-o", os.DevNull, ".") // #nosec G204 -- test-controlled arguments
	// No -tags, and no GOFLAGS that could carry -tags=fts5 in from the caller.
	cmd.Env = append(os.Environ(), "GOFLAGS=", "CGO_ENABLED=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("package built without the fts5 tag; the compile guard in sqlite3_nofts5.go is gone")
	}
	if !strings.Contains(string(out), "fts5BuildTagRequired_RebuildWith_tags_fts5") {
		t.Fatalf("build without fts5 failed for another reason:\n%s", out)
	}
}
