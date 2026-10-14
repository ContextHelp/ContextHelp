package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// URL credentials the fixture embeds in every configured locator: a
// password in the userinfo and a token in the query. Each is at least
// 16 characters with a distinct suffix, like redactFixtureSecrets, and
// shares no 4-character run with another, so a leak names its source.
const (
	urlDpkmsPass   = "Hq3ZvK8pWm2xRt7c0101"
	urlDpkmsQuery  = "Bn6YjL4sFd9gTk1u0102"
	urlLocalDBPass = "Pc2XwN7hJq5vMz8e0103"
	urlRegPass     = "Gt4KbR9yDs3nWx6a0104"
	urlRegQuery    = "Lm8QeV2jZc7pHf5r0105"
	urlOffPass     = "Sx5NdT1kYw9bMq3g0106"
	urlOffQuery    = "Ej7RuP4zCv8tKn2h0107"
	urlEmbedPass   = "Wa9FyB6mXr3sQd1k0108"
	urlEmbedQuery  = "Ok1TgJ5wNe8cVz4y0109"
)

var urlRedactSecrets = []string{
	urlDpkmsPass, urlDpkmsQuery, urlLocalDBPass, urlRegPass, urlRegQuery,
	urlOffPass, urlOffQuery, urlEmbedPass, urlEmbedQuery,
}

// withUserinfo puts user:pass@ into the http URL raw.
func withUserinfo(raw, user, pass string) string {
	return strings.Replace(raw, "://", "://"+user+":"+pass+"@", 1)
}

// Every command that prints a configured URL (server.urls entries, a
// local instance's database, registry urls, the embedding endpoint)
// shows it redacted, in every --format and in its errors: a password in
// the userinfo or a token in the query never reaches the output.
func TestCommandsRedactConfiguredURLs(t *testing.T) {
	db := setupTestDB(t)

	dpkms := newRecordedServer(t, fakeDpkmsAPI())
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(storage.RegistryManifest{Name: "reg", Version: "1.0.0"})
	}))
	t.Cleanup(reg.Close)

	homeURL := withUserinfo(dpkms.URL, "dpuser", urlDpkmsPass)
	qsURL := testguard.ClosedServerURL + "/?token=" + urlDpkmsQuery
	regURL := withUserinfo(reg.URL, "reguser", urlRegPass) + "/manifest.json?token=" + urlRegQuery
	offURL := withUserinfo(testguard.ClosedServerURL, "offuser", urlOffPass) + "/manifest.json?token=" + urlOffQuery
	embedURL := withUserinfo(testguard.ClosedServerURL, "emb", urlEmbedPass) + "/?token=" + urlEmbedQuery

	appendConfig(t, db, "server:\n  urls:\n"+
		"    - name: home\n      url: "+homeURL+"\n"+
		"    - name: qs\n      url: "+qsURL+"\n"+
		"registries:\n"+
		"  - name: reg\n    url: "+regURL+"\n"+
		"  - name: off\n    url: "+offURL+"\n"+
		"providers:\n  embedding:\n    provider: ollama\n    endpoint: "+embedURL+"\n")
	// TestMain points the embedding endpoint at a closed port through the
	// env layer, which outranks the config file: carry the credentials
	// there too (still a closed port).
	t.Setenv("CTXT_EMBEDDING_ENDPOINT", embedURL)

	runDir, err := config.RunDir(testDataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := pidfile.Write(runDir, pidfile.Info{
		PID: os.Getpid(), Name: "work", Port: 18095, StartedAt: time.Now(),
		DBPath: "postgres://pguser:" + urlLocalDBPass + "@db.internal:5432/ctxt?sslmode=disable",
	}); err != nil {
		t.Fatal(err)
	}

	// Seed the registry cache the read commands report on.
	if out, err := db.exec("registry", "add", "reg", regURL); err != nil {
		t.Fatalf("registry add: %v\n%s", err, out)
	}

	commands := [][]string{
		{"instance", "list"},
		{"instance", "current", "--instance", "home"},
		{"instance", "current", "--instance", "qs"},
		{"instance", "current", "--instance", "work"},
		{"instance", "use", "home"},
		{"instance", "use", "qs"},
		{"instance", "use", "-"},
		{"status", "--instance", "home"},
		{"status", "--instance", "qs"},
		{"upgrade", "status", "--instance", "home"},
		{"upgrade", "status", "--instance", "qs"},
		{"watch", "status", "--instance", "home"},
		{"watch", "status", "--instance", "qs"},
		{"ui", "open", "--dry-run", "--instance", "home"},
		{"ui", "open", "--no-browser", "--instance", "home"},
		{"registry", "add", "reg", regURL},
		{"registry", "add", "unlisted", regURL},
		{"registry", "list"},
		{"registry", "info", "reg"},
		{"registry", "capabilities", "reg"},
		{"registry", "sync", "--dry-run", "reg"},
		{"registry", "sync", "reg"},
		{"registry", "sync", "off"},
		{"registry", "sync", "--reconcile", "reg"},
		{"embeddings", "provider"},
		{"embeddings", "register", "nomic-embed-text", "--dry-run"},
		{"config", "show"},
	}
	// The default-format run of these shows the URL masked rather than
	// dropping it: proof the walk reaches the print it guards.
	shown := map[string][]string{
		"instance list":                     {"dpuser:****0101@", "token=****0102", "pguser:****0103@"},
		"instance current --instance home":  {"dpuser:****0101@"},
		"instance current --instance qs":    {"token=****0102"},
		"instance use home":                 {"dpuser:****0101@"},
		"status --instance home":            {"dpuser:****0101@"},
		"status --instance qs":              {"token=****0102"},
		"watch status --instance home":      {"dpuser:****0101@"},
		"ui open --dry-run --instance home": {"dpuser:****0101@"},
		"registry add reg " + regURL:        {"reguser:****0104@", "token=****0105"},
		"registry list":                     {"reguser:****0104@", "token=****0105"},
		"registry info reg":                 {"reguser:****0104@", "token=****0105"},
		"registry capabilities reg":         {"reguser:****0104@", "token=****0105"},
		"registry sync off":                 {"token=****0107"},
		"embeddings provider":               {"emb:****0108@", "token=****0109"},
	}
	formats := append([]string{""}, cliformat.Valid()...)
	for _, args := range commands {
		for _, f := range formats {
			name := f
			if name == "" {
				name = "default"
			}
			t.Run(strings.Join(args, " ")+"/"+name, func(t *testing.T) {
				full := append([]string{}, args...)
				if f != "" {
					full = append(full, "--format", f)
				}
				out, err := db.exec(full...)
				if err != nil {
					assertNoSecretOf(t, urlRedactSecrets, "error", err.Error())
				}
				assertNoSecretOf(t, urlRedactSecrets, "output", out)
				if f != "" {
					return
				}
				all := out
				if err != nil {
					all += err.Error()
				}
				for _, want := range shown[strings.Join(args, " ")] {
					if !strings.Contains(all, want) {
						t.Errorf("want masked %q in output:\n%s", want, all)
					}
				}
			})
		}
	}
}
