package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func startMockImportServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/import":
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]any{
				"batch_id": "batch_abc123",
				"count":    5,
			})

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/import/"):
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(batchStatusResponse{
				ID:        "batch_abc123",
				Status:    "completed",
				Total:     5,
				Processed: 5,
				Failed:    0,
				CreatedAt: "2026-03-13T10:00:00Z",
			})

		default:
			http.NotFound(w, r)
		}
	}))
}

func TestImportBatchHelp(t *testing.T) {
	out, err := executeCommand("import", "batch", "--help")
	if err != nil {
		t.Fatalf("import batch --help should succeed: %v", err)
	}
	for _, flag := range []string{"--file", "--dir", "--format", "--dry-run"} {
		if !strings.Contains(out, flag) {
			t.Errorf("import batch help should list flag %s", flag)
		}
	}
}

func TestImportBatchRequiresFileOrDir(t *testing.T) {
	_, err := executeCommand("import", "batch")
	if err == nil {
		t.Error("import batch without --file or --dir should fail")
	}
}

func TestImportBatchFileMutuallyExclusiveWithDir(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "data.jsonl")
	if err := os.WriteFile(f, []byte(`{"content":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := executeCommand("import", "batch", "--file", f, "--dir", dir)
	if err == nil {
		t.Error("import batch with both --file and --dir should fail")
	}
}

func TestImportBatchFileJSONL(t *testing.T) {
	srv := startMockImportServer(t)
	defer srv.Close()

	dir := t.TempDir()
	f := filepath.Join(dir, "records.jsonl")
	content := `{"content":"first record"}` + "\n" + `{"content":"second record"}` + "\n"
	if err := os.WriteFile(f, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("import", "batch", "--file", f, "--server", srv.URL)
	if err != nil {
		t.Fatalf("import batch --file should succeed: %v", err)
	}
	if !strings.Contains(out, "batch_abc123") {
		t.Errorf("output should contain batch ID, got: %s", out)
	}
}

func TestImportBatchFileCSVWithMapping(t *testing.T) {
	srv := startMockImportServer(t)
	defer srv.Close()

	dir := t.TempDir()
	f := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(f, []byte("body,kind,labels\nhello,note,work"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("import", "batch",
		"--file", f,
		"--map-content", "body",
		"--map-type", "kind",
		"--map-tags", "labels",
		"--server", srv.URL,
	)
	if err != nil {
		t.Fatalf("import batch CSV with mapping should succeed: %v", err)
	}
	if !strings.Contains(out, "batch_abc123") {
		t.Errorf("output should contain batch ID, got: %s", out)
	}
}

func TestImportBatchFileDryRun(t *testing.T) {
	srv := startMockImportServer(t)
	defer srv.Close()

	dir := t.TempDir()
	f := filepath.Join(dir, "records.jsonl")
	if err := os.WriteFile(f, []byte(`{"content":"test"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("import", "batch", "--file", f, "--dry-run", "--server", srv.URL)
	if err != nil {
		t.Fatalf("import batch --dry-run should succeed: %v", err)
	}
	if !strings.Contains(out, "Dry-run") {
		t.Errorf("output should indicate dry-run, got: %s", out)
	}
}

func TestImportBatchFileMissingFile(t *testing.T) {
	_, err := executeCommand("import", "batch", "--file", "/nonexistent/file.jsonl")
	if err == nil {
		t.Error("import batch with missing file should fail")
	}
}

func TestImportBatchFileUnknownFormat(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "data.xyz")
	if err := os.WriteFile(f, []byte("some data"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := executeCommand("import", "batch", "--file", f)
	if err == nil {
		t.Error("import batch with unknown extension and no --format should fail")
	}
}

func TestImportBatchFileExplicitFormat(t *testing.T) {
	srv := startMockImportServer(t)
	defer srv.Close()

	dir := t.TempDir()
	f := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(f, []byte(`{"content":"test"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("import", "batch", "--file", f, "--format", "jsonl", "--server", srv.URL)
	if err != nil {
		t.Fatalf("import batch with explicit --format should succeed: %v", err)
	}
	if !strings.Contains(out, "batch_abc123") {
		t.Errorf("output should contain batch ID, got: %s", out)
	}
}

func TestImportBatchDirMarkdown(t *testing.T) {
	srv := startMockImportServer(t)
	defer srv.Close()

	dir := t.TempDir()
	for _, name := range []string{"note1.md", "note2.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# "+name+"\n\nContent here."), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// A non-markdown file should be ignored.
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("skip me"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("import", "batch", "--dir", dir, "--server", srv.URL)
	if err != nil {
		t.Fatalf("import batch --dir should succeed: %v", err)
	}
	if !strings.Contains(out, "Files imported: 2") {
		t.Errorf("output should show 2 files imported, got: %s", out)
	}
}

func TestImportBatchDirDryRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Note\n\nContent."), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("import", "batch", "--dir", dir, "--dry-run")
	if err != nil {
		t.Fatalf("import batch --dir --dry-run should succeed: %v", err)
	}
	if !strings.Contains(out, "Dry-run") {
		t.Errorf("output should indicate dry-run, got: %s", out)
	}
	if !strings.Contains(out, "1 markdown files") {
		t.Errorf("output should report file count, got: %s", out)
	}
}

func TestImportBatchDirNoMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.csv"), []byte("a,b,c"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := executeCommand("import", "batch", "--dir", dir)
	if err == nil {
		t.Error("import batch --dir with no markdown files should fail")
	}
}

func TestImportBatchStatusSuccess(t *testing.T) {
	srv := startMockImportServer(t)
	defer srv.Close()

	out, err := executeCommand("import", "batch", "status", "batch_abc123", "--server", srv.URL)
	if err != nil {
		t.Fatalf("import batch status should succeed: %v", err)
	}
	if !strings.Contains(out, "batch_abc123") {
		t.Errorf("output should contain batch ID, got: %s", out)
	}
	if !strings.Contains(out, "Status:") {
		t.Errorf("output should contain Status field, got: %s", out)
	}
	if !strings.Contains(out, "completed") {
		t.Errorf("output should contain status value, got: %s", out)
	}
}

func TestImportBatchStatusRequiresID(t *testing.T) {
	_, err := executeCommand("import", "batch", "status")
	if err == nil {
		t.Error("import batch status without ID should fail")
	}
}

func TestDetectFormat(t *testing.T) {
	cases := []struct {
		path   string
		expect string
	}{
		{"data.jsonl", "jsonl"},
		{"DATA.JSONL", "jsonl"},
		{"records.csv", "csv"},
		{"export.tsv", "tsv"},
		{"note.md", "markdown"},
		{"note.markdown", "markdown"},
		{"unknown.xyz", ""},
	}
	for _, tc := range cases {
		got := detectFormat(tc.path)
		if got != tc.expect {
			t.Errorf("detectFormat(%q) = %q, want %q", tc.path, got, tc.expect)
		}
	}
}
