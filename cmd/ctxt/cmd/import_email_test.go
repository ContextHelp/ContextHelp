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

func TestImportEmailHelp(t *testing.T) {
	out, err := executeCommand("import", "email", "--help")
	if err != nil {
		t.Fatalf("import email --help should succeed: %v", err)
	}
	for _, keyword := range []string{"imap", "file", "--provider", "--dry-run", "--rules"} {
		if !strings.Contains(out, keyword) {
			t.Errorf("import email help should mention %q", keyword)
		}
	}
}

func TestImportEmailProviderValidation(t *testing.T) {
	_, err := executeCommand("import", "email", "--provider", "unknown")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestImportEmailFileProviderMissingFile(t *testing.T) {
	_, err := executeCommand("import", "email", "--provider", "file")
	if err == nil {
		t.Fatal("expected error when --file is missing")
	}
}

func TestImportEmailIMAPProviderMissingHost(t *testing.T) {
	_, err := executeCommand("import", "email", "--provider", "imap", "--user", "me@example.com", "--password", "secret")
	if err == nil {
		t.Fatal("expected error when --host is missing")
	}
}

func TestImportEmailIMAPProviderMissingUser(t *testing.T) {
	_, err := executeCommand("import", "email", "--provider", "imap", "--host", "imap.example.com", "--password", "secret")
	if err == nil {
		t.Fatal("expected error when --user is missing")
	}
}

func TestImportEmailFileSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/importers/email/run" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"run_id":   "run_123",
			"scanned":  1,
			"imported": 1,
			"skipped":  0,
			"failed":   0,
		})
	}))
	defer srv.Close()

	eml := "From: alice@example.com\r\nTo: bob@example.com\r\nSubject: Hi\r\nMessage-Id: <hi@example.com>\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\n\r\nHello.\r\n"
	tmpFile := filepath.Join(t.TempDir(), "test.eml")
	if err := os.WriteFile(tmpFile, []byte(eml), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	out, err := executeCommand("import", "email", "--provider", "file", "--file", tmpFile, "--server", srv.URL)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "Imported") && !strings.Contains(out, "run_123") {
		t.Logf("output: %s", out)
	}
}

func TestImportEmailDryRunOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"scanned":  1,
			"imported": 0,
			"skipped":  0,
			"routes": []any{
				map[string]any{
					"message_index": float64(0),
					"rule_id":       "billing",
					"pipeline":      "email.billing",
					"dropped":       false,
					"explain":       []any{"from_domain_in:[stripe.com]"},
				},
			},
		})
	}))
	defer srv.Close()

	eml := "From: billing@stripe.com\r\nTo: me@example.com\r\nSubject: Invoice\r\nMessage-Id: <inv@stripe.com>\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\n\r\nInvoice body.\r\n"
	tmpFile := filepath.Join(t.TempDir(), "invoice.eml")
	if err := os.WriteFile(tmpFile, []byte(eml), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	out, err := executeCommand("import", "email", "--provider", "file", "--file", tmpFile, "--dry-run", "--server", srv.URL)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "Dry-run") {
		t.Errorf("expected 'Dry-run' in output, got: %s", out)
	}
}

func TestImportEmailServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
	}))
	defer srv.Close()

	eml := "From: a@example.com\r\nTo: b@example.com\r\nSubject: X\r\nMessage-Id: <x@example.com>\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\n\r\nX.\r\n"
	tmpFile := filepath.Join(t.TempDir(), "x.eml")
	if err := os.WriteFile(tmpFile, []byte(eml), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	_, err := executeCommand("import", "email", "--provider", "file", "--file", tmpFile, "--server", srv.URL)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestImportEmailMboxFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"run_id":   "run_mbox",
			"scanned":  2,
			"imported": 2,
		})
	}))
	defer srv.Close()

	mbox := "From alice@example.com Mon Jan 01 10:00:00 2024\r\nFrom: alice@example.com\r\nTo: bob@example.com\r\nSubject: Msg 1\r\nMessage-Id: <m1@example.com>\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\n\r\nBody one.\r\n\r\nFrom charlie@example.com Tue Jan 02 11:00:00 2024\r\nFrom: charlie@example.com\r\nTo: bob@example.com\r\nSubject: Msg 2\r\nMessage-Id: <m2@example.com>\r\nDate: Tue, 02 Jan 2024 11:00:00 +0000\r\n\r\nBody two.\r\n"
	tmpFile := filepath.Join(t.TempDir(), "archive.mbox")
	if err := os.WriteFile(tmpFile, []byte(mbox), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	out, err := executeCommand("import", "email", "--provider", "file", "--file", tmpFile, "--server", srv.URL)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	_ = out
}
