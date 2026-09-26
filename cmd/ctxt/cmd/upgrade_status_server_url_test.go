package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// recordedServer is an httptest server that serves h and records the
// Authorization header of every request it receives.
type recordedServer struct {
	*httptest.Server
	mu    sync.Mutex
	auths []string
}

func newRecordedServer(t *testing.T, h http.Handler) *recordedServer {
	t.Helper()
	rs := &recordedServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.auths = append(rs.auths, r.Header.Get("Authorization"))
		rs.mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(rs.Close)
	return rs
}

// hits returns the Authorization headers seen so far, one per request.
func (rs *recordedServer) hits() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]string(nil), rs.auths...)
}

// inProgressHealthz serves /healthz from the real handler with an
// in-progress embeddings migration toward model-b.
func inProgressHealthz(t *testing.T) http.Handler {
	t.Helper()
	mgr := upgrade.NewManager("")
	if err := mgr.StartTarget(upgrade.BucketEmbeddingsMigrate, "model-b@1", 4); err != nil {
		t.Fatal(err)
	}
	return httpserver.Healthz(nil, httpserver.HealthzProbes{
		Upgrade: func(context.Context) *httpserver.UpgradeSnapshot {
			return httpserver.NewUpgradeSnapshot(mgr.Snapshot())
		},
	})
}

// assertModelBInProgress asserts out carries the in-progress model-b state.
func assertModelBInProgress(t *testing.T, out string) {
	t.Helper()
	var env upgradeEnvelope
	if err := json.Unmarshal([]byte(firstJSON(out)), &env); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if env.State != "in_progress" || !strings.Contains(env.Target, "model-b") {
		t.Fatalf("status = %+v, want the configured server's in-progress migration", env)
	}
}

// `ctxt upgrade status` reads the daemon at the configured server.url when
// --server is not given, like every other client command; it never falls
// back to the built-in default while a server is configured.
func TestUpgradeStatus_UsesConfiguredServerURL(t *testing.T) {
	srv := newRecordedServer(t, inProgressHealthz(t))
	db := setupTestDB(t)
	appendConfig(t, db, "server:\n  url: "+srv.URL+"\n  token: tok-cfg\n")

	out, err := db.exec("upgrade", "status", "--format", "json")
	if err != nil {
		t.Fatalf("upgrade status: %v\n%s", err, out)
	}
	assertModelBInProgress(t, out)
	if got := srv.hits(); len(got) != 1 || got[0] != "Bearer tok-cfg" {
		t.Fatalf("server saw Authorization %q, want one request with the configured token", got)
	}
}

// A -c server.url=... overlay selects the server over the config file's.
func TestUpgradeStatus_ConfigOverlaySelectsServer(t *testing.T) {
	srv := newRecordedServer(t, inProgressHealthz(t))
	db := setupTestDB(t)
	appendConfig(t, db, "server:\n  url: http://127.0.0.1:1\n")

	out, err := db.exec("-c", "server.url="+srv.URL, "upgrade", "status", "--format", "json")
	if err != nil {
		t.Fatalf("upgrade status: %v\n%s", err, out)
	}
	assertModelBInProgress(t, out)
}

// An explicit --server overrides the configured server.url.
func TestUpgradeStatus_ServerFlagOverridesConfig(t *testing.T) {
	configured := newRecordedServer(t, inProgressHealthz(t))
	pinned := newRecordedServer(t, inProgressHealthz(t))
	db := setupTestDB(t)
	appendConfig(t, db, "server:\n  url: "+configured.URL+"\n")

	out, err := db.exec("upgrade", "status", "--server", pinned.URL, "--format", "json")
	if err != nil {
		t.Fatalf("upgrade status: %v\n%s", err, out)
	}
	assertModelBInProgress(t, out)
	if n := len(configured.hits()); n != 0 {
		t.Fatalf("configured server got %d requests, want 0 with --server", n)
	}
	if n := len(pinned.hits()); n != 1 {
		t.Fatalf("--server got %d requests, want 1", n)
	}
}

// plan and run work on the local store; they take no --server that would
// suggest otherwise.
func TestUpgradePlanRun_TakeNoServerFlag(t *testing.T) {
	for _, sub := range []string{"plan", "run"} {
		t.Run(sub, func(t *testing.T) {
			db := setupTestDB(t)
			_, err := db.exec("upgrade", sub, "--server", "http://127.0.0.1:1")
			if err == nil || !strings.Contains(err.Error(), "unknown flag: --server") {
				t.Fatalf("upgrade %s --server: err = %v, want unknown flag", sub, err)
			}
		})
	}
}
