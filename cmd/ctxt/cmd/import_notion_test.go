package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImportNotionDryRunPageID(t *testing.T) {
	t.Setenv("NOTION_TOKEN", "test-token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/pages/page-1" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		writeNotionJSON(t, w, http.StatusOK, map[string]any{
			"object":           "page",
			"id":               "page-1",
			"url":              "https://notion.so/page-1",
			"last_edited_time": "2026-02-18T12:00:00Z",
			"properties": map[string]any{
				"title": map[string]any{
					"type": "title",
					"title": []map[string]any{
						{"plain_text": "Page One"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	out, err := executeCommand(
		"import", "notion",
		"--page-id", "page-1",
		"--dry-run",
		"--notion-base-url", srv.URL,
	)
	if err != nil {
		t.Fatalf("import notion dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Notion pages selected: 1 (dry-run)") {
		t.Fatalf("expected dry-run summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Page One") {
		t.Fatalf("expected page preview title, got:\n%s", out)
	}
}

func TestImportNotionEnqueueSuccess(t *testing.T) {
	t.Setenv("NOTION_TOKEN", "test-token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/pages/page-1" && r.Method == http.MethodGet:
			writeNotionJSON(t, w, http.StatusOK, map[string]any{
				"object":           "page",
				"id":               "page-1",
				"url":              "https://notion.so/page-1",
				"last_edited_time": "2026-02-18T12:00:00Z",
				"properties": map[string]any{
					"title": map[string]any{
						"type": "title",
						"title": []map[string]any{
							{"plain_text": "Page One"},
						},
					},
				},
			})
		case r.URL.Path == "/v1/blocks/page-1/children" && r.Method == http.MethodGet:
			writeNotionJSON(t, w, http.StatusOK, map[string]any{
				"results": []map[string]any{
					{
						"type": "paragraph",
						"paragraph": map[string]any{
							"rich_text": []map[string]any{
								{"plain_text": "Line one"},
							},
						},
					},
				},
				"has_more":    false,
				"next_cursor": "",
			})
		case r.URL.Path == "/api/v1/pipelines/enqueue" && r.Method == http.MethodPost:
			var req enqueueRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode enqueue request: %v", err)
			}
			if req.Type != "text" {
				t.Fatalf("enqueue type = %q, want text", req.Type)
			}
			if req.Source != "https://notion.so/page-1" {
				t.Fatalf("enqueue source = %q", req.Source)
			}
			if !strings.Contains(req.Content, "# Page One") || !strings.Contains(req.Content, "Line one") {
				t.Fatalf("unexpected enqueue content:\n%s", req.Content)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_notion_1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeCommand(
		"import", "notion",
		"--page-id", "page-1",
		"--notion-base-url", srv.URL,
		"--server", srv.URL,
	)
	if err != nil {
		t.Fatalf("import notion should succeed: %v", err)
	}

	if !strings.Contains(out, "Notion pages processed: 1") {
		t.Fatalf("expected processed summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Jobs enqueued: 1") {
		t.Fatalf("expected enqueue summary, got:\n%s", out)
	}
}

func TestImportNotionRequiresToken(t *testing.T) {
	t.Setenv("NOTION_TOKEN", "")

	_, err := executeCommand("import", "notion", "--all-shared", "--dry-run")
	if err == nil {
		t.Fatal("expected missing token error")
	}
	if !strings.Contains(err.Error(), "notion token is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportNotionRequiresScope(t *testing.T) {
	t.Setenv("NOTION_TOKEN", "test-token")

	_, err := executeCommand("import", "notion", "--dry-run")
	if err == nil {
		t.Fatal("expected missing scope error")
	}
	if !strings.Contains(err.Error(), "provide at least one scope") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeNotionJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
