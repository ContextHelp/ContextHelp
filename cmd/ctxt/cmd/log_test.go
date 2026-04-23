package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestLogHelp(t *testing.T) {
	out, err := executeCommand("log", "--help")
	if err != nil {
		t.Fatalf("log --help should succeed: %v", err)
	}
	for _, flag := range []string{"--limit", "--type", "--since", "--until", "--object", "--actor", "--server"} {
		if !strings.Contains(out, flag) {
			t.Errorf("log help should list flag %s", flag)
		}
	}
}

func TestLogInvalidSince(t *testing.T) {
	_, err := executeCommand("log", "--since", "not-a-date", "--server", "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("log should fail on invalid --since")
	}
	if !strings.Contains(err.Error(), "--since") {
		t.Error("error should mention --since")
	}
}

func TestLogInvalidUntil(t *testing.T) {
	_, err := executeCommand("log", "--until", "xyz", "--server", "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("log should fail on invalid --until")
	}
	if !strings.Contains(err.Error(), "--until") {
		t.Error("error should mention --until")
	}
}

func TestLogFromServer(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	entries := []*storage.AuditEntry{
		{ID: "e1", EventType: "object.created", ObjectID: "obj_01",
			Actor: "system", Payload: map[string]any{"src": "cli"},
			CreatedAt: now},
		{ID: "e2", EventType: "fanout.completed", ObjectID: "obj_01",
			Actor: "system", Payload: map[string]any{},
			CreatedAt: now.Add(time.Second)},
	}

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/audit-log" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data":  entries,
				"total": len(entries),
			})
		}))
	defer srv.Close()

	out, err := executeCommand("log", "--server", srv.URL)
	if err != nil {
		t.Fatalf("log should succeed: %v", err)
	}
	if !strings.Contains(out, "Audit Log") {
		t.Error("output should contain 'Audit Log'")
	}
	if !strings.Contains(out, "object.created") {
		t.Error("output should contain event type")
	}
	if !strings.Contains(out, "obj_01") {
		t.Error("output should contain object ID")
	}
}

func TestLogFromServerJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	entries := []*storage.AuditEntry{
		{ID: "e1", EventType: "object.created", ObjectID: "obj_01",
			Actor: "system", Payload: map[string]any{},
			CreatedAt: now},
	}

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data":  entries,
				"total": 1,
			})
		}))
	defer srv.Close()

	out, err := executeCommand("log", "--server", srv.URL, "--output", "json")
	if err != nil {
		t.Fatalf("log --output json should succeed: %v", err)
	}
	var result logResponse
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output should be valid JSON: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("total want 1, got %d", result.Total)
	}
}

func TestLogQueryParams(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			captured = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data":  []*storage.AuditEntry{},
				"total": 0,
			})
		}))
	defer srv.Close()

	_, err := executeCommand("log",
		"--server", srv.URL,
		"--type", "create,enrich",
		"--object", "obj_xyz",
		"--actor", "alice",
		"--since", "2026-01-01",
		"--until", "2026-12-31",
		"--limit", "5",
	)
	if err != nil {
		t.Fatalf("log should succeed: %v", err)
	}

	for _, want := range []string{
		"type=create%2Cenrich",
		"object_id=obj_xyz",
		"actor=alice",
		"since=2026-01-01",
		"until=2026-12-31",
		"limit=5",
	} {
		if !strings.Contains(captured, want) {
			t.Errorf("query should contain %q, got %q", want, captured)
		}
	}
}

func TestFormatPayload(t *testing.T) {
	if got := formatPayload(nil); got != "" {
		t.Errorf("nil payload want empty, got %q", got)
	}
	if got := formatPayload(map[string]any{}); got != "" {
		t.Errorf("empty payload want empty, got %q", got)
	}
	got := formatPayload(map[string]any{"k": "v"})
	if !strings.Contains(got, `"k"`) {
		t.Errorf("payload should contain key, got %q", got)
	}
}
