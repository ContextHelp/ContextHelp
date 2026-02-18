package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImportRaindropDryRun(t *testing.T) {
	t.Setenv(raindropTokenEnv, "test-token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/v1/raindrops/0":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"result": true,
				"items": []map[string]any{
					{
						"_id":        101,
						"title":      "Raindrop Item",
						"link":       "https://example.com/raindrop",
						"lastUpdate": "2026-02-18T10:00:00Z",
						"collection": map[string]any{"$id": 7},
					},
				},
				"count": 1,
			})
		case "/rest/v1/collections":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"result": true,
				"items": []map[string]any{
					{"_id": 7, "title": "Research"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeCommand(
		"import", "raindrop",
		"--all",
		"--dry-run",
		"--raindrop-base-url", srv.URL,
	)
	if err != nil {
		t.Fatalf("import raindrop dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Raindrop items selected: 1 (dry-run)") {
		t.Fatalf("expected dry-run summary, got:\n%s", out)
	}
	if !strings.Contains(out, "[Research] Raindrop Item -> https://example.com/raindrop") {
		t.Fatalf("expected preview output with collection and URL, got:\n%s", out)
	}
}

func TestImportRaindropEnqueueSuccess(t *testing.T) {
	t.Setenv(raindropTokenEnv, "test-token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/v1/raindrops/0":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"result": true,
				"items": []map[string]any{
					{
						"_id":        101,
						"title":      "Raindrop Item",
						"link":       "https://example.com/raindrop",
						"lastUpdate": "2026-02-18T10:00:00Z",
						"collection": map[string]any{"$id": 7},
						"tags":       []string{"work"},
					},
				},
				"count": 1,
			})
		case "/rest/v1/collections":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"result": true,
				"items": []map[string]any{
					{"_id": 7, "title": "Research"},
				},
			})
		case "/api/v1/pipelines/enqueue":
			var req enqueueRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode enqueue request: %v", err)
			}
			if req.Type != "text" {
				t.Fatalf("enqueue type = %q, want text", req.Type)
			}
			if req.Pipeline != "import.raindrop" {
				t.Fatalf("enqueue pipeline = %q, want import.raindrop", req.Pipeline)
			}
			if req.Source != "https://example.com/raindrop" {
				t.Fatalf("enqueue source = %q", req.Source)
			}
			if !strings.Contains(req.Content, "# Raindrop Item") || !strings.Contains(req.Content, "Provider: Raindrop.io") {
				t.Fatalf("unexpected enqueue content:\n%s", req.Content)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_raindrop_1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeCommand(
		"import", "raindrop",
		"--all",
		"--raindrop-base-url", srv.URL,
		"--server", srv.URL,
	)
	if err != nil {
		t.Fatalf("import raindrop should succeed: %v", err)
	}

	if !strings.Contains(out, "Raindrop items processed: 1") {
		t.Fatalf("expected processed summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Jobs enqueued: 1") {
		t.Fatalf("expected enqueue summary, got:\n%s", out)
	}
}

func TestImportRaindropRequiresToken(t *testing.T) {
	t.Setenv(raindropTokenEnv, "")

	_, err := executeCommand("import", "raindrop", "--all", "--dry-run")
	if err == nil {
		t.Fatal("expected missing token error")
	}
	if !strings.Contains(err.Error(), "raindrop token is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportRaindropRequiresScope(t *testing.T) {
	t.Setenv(raindropTokenEnv, "test-token")

	_, err := executeCommand("import", "raindrop", "--dry-run")
	if err == nil {
		t.Fatal("expected missing scope error")
	}
	if !strings.Contains(err.Error(), "provide at least one scope") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, status int, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
