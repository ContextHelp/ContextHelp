package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	onedriveimporter "github.com/ideacrafterslabs/ctxt/internal/importer/onedrive"
)

// fakeDpkmsAPI answers every dpkms route the HTTP commands (feed,
// import, status, log, upgrade status, capture) call with a minimal
// success body.
func fakeDpkmsAPI() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case r.Method == http.MethodPost && p == "/api/v1/pipelines/enqueue":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_1"})
		case r.Method == http.MethodPost && p == "/api/v1/import":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"batch_id": "batch_1", "count": 1})
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/api/v1/import/"):
			_ = json.NewEncoder(w).Encode(batchStatusResponse{ID: "batch_1", Status: "completed", Total: 1, Processed: 1})
		case r.Method == http.MethodPost && p == "/api/v1/importers/email/run":
			_ = json.NewEncoder(w).Encode(map[string]any{"run_id": "run_1", "scanned": 1, "imported": 1})
		case r.Method == http.MethodPost && p == "/api/v1/feeds":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "feed_1"})
		case r.Method == http.MethodGet && p == "/api/v1/feeds":
			_ = json.NewEncoder(w).Encode([]feedResponse{{ID: "feed_1", URL: "https://example.com/feed.xml", Status: "active"}})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/sync"):
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_sync"})
		case r.Method == http.MethodDelete && strings.HasPrefix(p, "/api/v1/feeds/"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && p == "/healthz":
			_ = json.NewEncoder(w).Encode(statusEnvelope{Health: "healthy"})
		case r.Method == http.MethodGet && p == "/api/v1/audit-log":
			_ = json.NewEncoder(w).Encode(logResponse{})
		case r.Method == http.MethodPost && p == "/api/v1/analyze":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_capture"})
		case r.Method == http.MethodPost && p == "/api/v1/inbox":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "obj_inbox"})
		default:
			http.NotFound(w, r)
		}
	})
}

// fakeSourceAPI stands in for an importer's upstream service (Notion,
// Raindrop, Dropbox, Google Drive): fixed JSON bodies by path.
func fakeSourceAPI(t *testing.T, routes map[string]string) string {
	t.Helper()
	srv := newRecordedServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	return srv.URL
}

// writeFixture writes content to name under a fresh temp dir and returns
// the file path.
func writeFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// serverClientCase is one client command that talks to dpkms; args builds
// its arguments (without --server) and any fixtures or upstream stubs.
type serverClientCase struct {
	name string
	args func(t *testing.T) []string
}

func serverClientCases() []serverClientCase {
	bookmarks := func(cmd string) serverClientCase {
		return serverClientCase{"import " + cmd, func(t *testing.T) []string {
			return []string{"import", cmd, "--file", writeTempBookmarks(t, chromeBookmarksFixture)}
		}}
	}
	return []serverClientCase{
		{"feed add", func(*testing.T) []string { return []string{"feed", "add", "https://example.com/feed.xml"} }},
		{"feed list", func(*testing.T) []string { return []string{"feed", "list"} }},
		{"feed sync", func(*testing.T) []string {
			return []string{"feed", "sync", "--url", "https://example.com/feed.xml"}
		}},
		{"feed delete", func(*testing.T) []string {
			return []string{"feed", "delete", "https://example.com/feed.xml", "--confirm=yes"}
		}},
		{"import batch", func(t *testing.T) []string {
			return []string{"import", "batch", "--file", writeFixture(t, "records.jsonl", `{"content":"one"}`+"\n")}
		}},
		{"import batch dir", func(t *testing.T) []string {
			return []string{"import", "batch", "--dir", filepath.Dir(writeFixture(t, "note.md", "# Note\n"))}
		}},
		{"import batch status", func(*testing.T) []string { return []string{"import", "batch", "status", "batch_1"} }},
		{"import email", func(t *testing.T) []string {
			eml := "From: a@example.com\r\nTo: b@example.com\r\nSubject: Hi\r\nMessage-Id: <hi@example.com>\r\n" +
				"Date: Mon, 01 Jan 2024 10:00:00 +0000\r\n\r\nHello.\r\n"
			return []string{"import", "email", "--provider", "file", "--file", writeFixture(t, "m.eml", eml)}
		}},
		bookmarks("chrome"),
		bookmarks("edge"),
		bookmarks("firefox"),
		bookmarks("safari"),
		{"import discord", func(t *testing.T) []string {
			return []string{"import", "discord", "--file", writeDiscordExport(t)}
		}},
		{"import evernote", func(t *testing.T) []string {
			return []string{"import", "evernote", "--file", writeTempBookmarks(t, evernoteENEXFixture)}
		}},
		{"import logseq", func(t *testing.T) []string {
			page := writeFixture(t, filepath.Join("pages", "Topic.md"), "- a block about a topic\n")
			return []string{"import", "logseq", "--graph", filepath.Dir(filepath.Dir(page))}
		}},
		{"import obsidian", func(t *testing.T) []string {
			note := writeFixture(t, "Note.md", "# Note\n\nbody\n")
			return []string{"import", "obsidian", "--vault", filepath.Dir(note)}
		}},
		{"import slack", func(t *testing.T) []string { return []string{"import", "slack", "--dir", writeSlackExport(t)} }},
		{"import linkedin", func(t *testing.T) []string {
			csv := "Date,ShareCommentary,ShareMediaCategory,SharedUrl\n" +
				"2023-03-15 10:30:00 UTC,Thoughts on distributed systems.,ARTICLE,https://example.com/post\n"
			return []string{"import", "linkedin", "--posts", writeFixture(t, "Posts.csv", csv)}
		}},
		{"import twitter", func(t *testing.T) []string {
			js := `window.YTD.tweets.part0 = [{"tweet":{"id_str":"1","full_text":"hello","created_at":"Mon Jan 02 15:04:05 +0000 2023","entities":{"urls":[],"hashtags":[]}}}]`
			return []string{"import", "twitter", "--file", writeFixture(t, "tweets.js", js)}
		}},
		{"import pinboard", func(t *testing.T) []string {
			t.Setenv(pinboardTokenEnv, "")
			js := `[{"href":"https://example.com/go","description":"Go","hash":"h1","time":"2026-02-01T12:00:00Z","tags":"go"}]`
			return []string{"import", "pinboard", "--file", writeFixture(t, "pinboard.json", js)}
		}},
		{"import notion", func(t *testing.T) []string {
			t.Setenv("NOTION_TOKEN", "upstream-token")
			api := fakeSourceAPI(t, map[string]string{
				"/v1/pages/page-1": `{"object":"page","id":"page-1","url":"https://notion.so/page-1","last_edited_time":"2026-02-18T12:00:00Z",` +
					`"properties":{"title":{"type":"title","title":[{"plain_text":"Page One"}]}}}`,
				"/v1/blocks/page-1/children": `{"results":[{"type":"paragraph","paragraph":{"rich_text":[{"plain_text":"Line"}]}}],"has_more":false}`,
			})
			return []string{"import", "notion", "--page-id", "page-1", "--notion-base-url", api}
		}},
		{"import raindrop", func(t *testing.T) []string {
			t.Setenv(raindropTokenEnv, "upstream-token")
			api := fakeSourceAPI(t, map[string]string{
				"/rest/v1/raindrops/0": `{"result":true,"count":1,"items":[{"_id":1,"title":"Item","link":"https://example.com/r",` +
					`"lastUpdate":"2026-02-18T10:00:00Z","collection":{"$id":7}}]}`,
				"/rest/v1/collections": `{"result":true,"items":[{"_id":7,"title":"Research"}]}`,
			})
			return []string{"import", "raindrop", "--all", "--raindrop-base-url", api}
		}},
		{"import dropbox", func(t *testing.T) []string {
			api := fakeSourceAPI(t, map[string]string{
				"/2/files/list_folder": `{"entries":[{".tag":"file","id":"id:f1","name":"notes.md","path_display":"/notes.md",` +
					`"size":10,"server_modified":"2026-02-01T10:00:00Z"}],"cursor":"c","has_more":false}`,
			})
			return []string{
				"import", "dropbox", "--access-token", "upstream-token",
				"--dropbox-api-url", api, "--dropbox-content-url", api,
			}
		}},
		{"import gdrive", func(t *testing.T) []string {
			api := fakeSourceAPI(t, map[string]string{
				"/drive/v3/files": `{"files":[{"id":"pdf-1","name":"Spec","mimeType":"application/pdf",` +
					`"modifiedTime":"2026-01-20T12:00:00Z","webViewLink":"https://drive.google.com/file/d/pdf-1/view"}]}`,
			})
			return []string{"import", "gdrive", "--access-token", "upstream-token", "--drive-base-url", api}
		}},
		{"import github", func(t *testing.T) []string {
			orig := newGitHubClient
			newGitHubClient = func(string, string) githubFetcher { return &stubGitHubFetcher{repos: sampleRepos()} }
			t.Cleanup(func() { newGitHubClient = orig })
			return []string{"import", "github", "--username", "octocat"}
		}},
		{"import onedrive", func(t *testing.T) []string {
			t.Setenv("ONEDRIVE_TOKEN", "upstream-token")
			orig := newOneDriveClient
			newOneDriveClient = func(string, string) oneDriveClient {
				return &fakeOneDriveClient{listDriveFolderItems: func(context.Context, string, string, int) ([]onedriveimporter.Item, error) {
					return []onedriveimporter.Item{{
						DriveID: "b!d", ID: "i1", Name: "r.pdf", Path: "/r.pdf",
						WebURL: "https://example.com/r", IsFile: true,
					}}, nil
				}}
			}
			t.Cleanup(func() { newOneDriveClient = orig })
			return []string{"import", "onedrive", "--drive-id", "b!d"}
		}},
	}
}

// Every feed and import command sends its requests to the configured
// server.url with the configured token when --server is not given, never
// to a built-in default.
func TestServerClients_UseConfiguredServerAndToken(t *testing.T) {
	for _, tc := range serverClientCases() {
		t.Run(tc.name, func(t *testing.T) {
			srv := newRecordedServer(t, fakeDpkmsAPI())
			db := setupTestDB(t)
			appendConfig(t, db, "server:\n  url: "+srv.URL+"\n  token: tok-cfg\n")

			out, err := db.exec(tc.args(t)...)
			if err != nil {
				t.Fatalf("%s: %v\n%s", tc.name, err, out)
			}
			assertAllAuthorized(t, srv.hits(), "Bearer tok-cfg")
		})
	}
}

// The primary of server.urls wins over server.url, with its own token.
func TestServerClients_UseServerURLsPrimary(t *testing.T) {
	for _, tc := range serverClientCases() {
		t.Run(tc.name, func(t *testing.T) {
			primary := newRecordedServer(t, fakeDpkmsAPI())
			single := newRecordedServer(t, fakeDpkmsAPI())
			db := setupTestDB(t)
			appendConfig(t, db, "server:\n  url: "+single.URL+"\n  token: tok-default\n"+
				"  urls:\n    - url: "+primary.URL+"\n      token: tok-primary\n")

			out, err := db.exec(tc.args(t)...)
			if err != nil {
				t.Fatalf("%s: %v\n%s", tc.name, err, out)
			}
			assertAllAuthorized(t, primary.hits(), "Bearer tok-primary")
			if n := len(single.hits()); n != 0 {
				t.Fatalf("server.url got %d requests while server.urls is set, want 0", n)
			}
		})
	}
}

// An explicit --server overrides the configured server.url.
func TestServerClients_ServerFlagOverridesConfig(t *testing.T) {
	for _, tc := range serverClientCases() {
		t.Run(tc.name, func(t *testing.T) {
			configured := newRecordedServer(t, fakeDpkmsAPI())
			pinned := newRecordedServer(t, fakeDpkmsAPI())
			db := setupTestDB(t)
			appendConfig(t, db, "server:\n  url: "+configured.URL+"\n  token: tok-cfg\n")

			out, err := db.exec(append(tc.args(t), "--server", pinned.URL)...)
			if err != nil {
				t.Fatalf("%s --server: %v\n%s", tc.name, err, out)
			}
			if n := len(configured.hits()); n != 0 {
				t.Fatalf("configured server got %d requests, want 0 with --server", n)
			}
			assertAllAuthorized(t, pinned.hits(), "Bearer tok-cfg")
		})
	}
}

// assertAllAuthorized fails unless hits is non-empty and every request
// carried want.
func assertAllAuthorized(t *testing.T, hits []string, want string) {
	t.Helper()
	if len(hits) == 0 {
		t.Fatal("server got no requests")
	}
	for i, got := range hits {
		if got != want {
			t.Fatalf("request %d Authorization = %q, want %q", i, got, want)
		}
	}
}
