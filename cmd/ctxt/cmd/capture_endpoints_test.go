package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
)

// endpointHit is one request a mock dpkms instance received.
type endpointHit struct {
	Path string
	Auth string
}

// endpointRecorder is a mock dpkms instance that records every request
// (path + Authorization header) and, when token is non-empty, rejects
// API calls that do not carry it — the auth middleware answers 401
// before anything is stored.
type endpointRecorder struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits []endpointHit
}

func startEndpointRecorder(t *testing.T, token string) *endpointRecorder {
	t.Helper()
	rec := &endpointRecorder{}
	rec.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.hits = append(rec.hits, endpointHit{Path: r.URL.Path, Auth: r.Header.Get("Authorization")})
		rec.mu.Unlock()
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"UNAUTHORIZED"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_routed"})
		case r.URL.Path == "/api/v1/inbox" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "obj_routed"})
		case strings.HasPrefix(r.URL.Path, "/api/v1/jobs/") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(rec.srv.Close)
	return rec
}

func (r *endpointRecorder) URL() string { return r.srv.URL }

func (r *endpointRecorder) Hits() []endpointHit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]endpointHit(nil), r.hits...)
}

// hitsOn returns the recorded hits whose path matches.
func (r *endpointRecorder) hitsOn(path string) []endpointHit {
	var out []endpointHit
	for _, h := range r.Hits() {
		if h.Path == path {
			out = append(out, h)
		}
	}
	return out
}

// writeCaptureConfig writes a config file with the given YAML body.
func writeCaptureConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

// closedServerURL returns the URL of an httptest server that has already
// been shut down: dialing it fails before any request byte is sent.
func closedServerURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	return u
}

// TestCaptureRoutesToConfiguredServerURL: with no --server, capture posts
// to the configured server.url and authenticates with server.token.
func TestCaptureRoutesToConfiguredServerURL(t *testing.T) {
	inst := startEndpointRecorder(t, "tok-url")
	cfgPath := writeCaptureConfig(t, "server:\n  url: "+inst.URL()+"\n  token: tok-url\n")

	out, err := executeCommand("capture", "routed content", "-c", cfgPath)
	if err != nil {
		t.Fatalf("capture with configured server.url: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "Job ID: job_routed") {
		t.Errorf("output should carry the configured instance's job ID: %q", out)
	}
	hits := inst.hitsOn("/api/v1/analyze")
	if len(hits) != 1 {
		t.Fatalf("configured instance analyze hits = %d; want 1", len(hits))
	}
	if hits[0].Auth != "Bearer tok-url" {
		t.Errorf("Authorization = %q; want Bearer tok-url", hits[0].Auth)
	}
}

// TestCaptureRoutesToServerURLsEntryToken: a server.urls entry with its
// own token routes the capture with that token.
func TestCaptureRoutesToServerURLsEntryToken(t *testing.T) {
	inst := startEndpointRecorder(t, "tok-entry")
	cfgPath := writeServerConfig(t, inst.URL(), "tok-entry")

	out, err := executeCommand("capture", "entry content", "-c", cfgPath)
	if err != nil {
		t.Fatalf("capture with server.urls entry: %v (out=%q)", err, out)
	}
	hits := inst.hitsOn("/api/v1/analyze")
	if len(hits) != 1 || hits[0].Auth != "Bearer tok-entry" {
		t.Errorf("analyze hits = %+v; want one with Bearer tok-entry", hits)
	}
}

// TestCaptureServerFlagOverridesConfig: an explicit --server pins routing
// to that instance in either flag position; the configured instance
// sees nothing, and its token is not leaked to the pinned one.
func TestCaptureServerFlagOverridesConfig(t *testing.T) {
	for name, argv := range map[string]func(pinned, cfgPath string) []string{
		"flag-first": func(pinned, cfgPath string) []string {
			return []string{"capture", "--server", pinned, "pinned content", "-c", cfgPath}
		},
		"flag-last": func(pinned, cfgPath string) []string {
			return []string{"capture", "pinned content", "-c", cfgPath, "--server", pinned}
		},
	} {
		t.Run(name, func(t *testing.T) {
			configured := startEndpointRecorder(t, "tok-configured")
			pinned := startEndpointRecorder(t, "")
			cfgPath := writeServerConfig(t, configured.URL(), "tok-configured")

			out, err := executeCommand(argv(pinned.URL(), cfgPath)...)
			if err != nil {
				t.Fatalf("capture --server: %v (out=%q)", err, out)
			}
			if n := len(configured.Hits()); n != 0 {
				t.Errorf("configured instance got %d hits despite --server", n)
			}
			hits := pinned.hitsOn("/api/v1/analyze")
			if len(hits) != 1 {
				t.Fatalf("pinned instance analyze hits = %d; want 1", len(hits))
			}
			if hits[0].Auth != "" {
				t.Errorf("pinned instance received Authorization %q; the configured token must not leak", hits[0].Auth)
			}
		})
	}
}

// TestCaptureServerFlagReusesConfiguredToken: --server naming a configured
// instance reuses that instance's token.
func TestCaptureServerFlagReusesConfiguredToken(t *testing.T) {
	inst := startEndpointRecorder(t, "tok-pin")
	other := startEndpointRecorder(t, "")
	cfgPath := writeCaptureConfig(t, "server:\n  urls:\n    - "+other.URL()+
		"\n    - url: "+inst.URL()+"\n      token: tok-pin\n")

	out, err := executeCommand("capture", "pinned content", "--server", inst.URL()+"/", "-c", cfgPath)
	if err != nil {
		t.Fatalf("capture --server <configured>: %v (out=%q)", err, out)
	}
	if n := len(other.Hits()); n != 0 {
		t.Errorf("primary got %d hits despite --server pinning the secondary", n)
	}
	hits := inst.hitsOn("/api/v1/analyze")
	if len(hits) != 1 || hits[0].Auth != "Bearer tok-pin" {
		t.Errorf("pinned analyze hits = %+v; want one with Bearer tok-pin", hits)
	}
}

// TestCaptureInboxRoutesToConfiguredInstance: --inbox follows the same
// resolution and credentials.
func TestCaptureInboxRoutesToConfiguredInstance(t *testing.T) {
	inst := startEndpointRecorder(t, "tok-inbox")
	cfgPath := writeServerConfig(t, inst.URL(), "tok-inbox")

	out, err := executeCommand("capture", "inbox content", "--inbox", "-c", cfgPath)
	if err != nil {
		t.Fatalf("capture --inbox: %v (out=%q)", err, out)
	}
	hits := inst.hitsOn("/api/v1/inbox")
	if len(hits) != 1 || hits[0].Auth != "Bearer tok-inbox" {
		t.Errorf("inbox hits = %+v; want one with Bearer tok-inbox", hits)
	}
}

// TestCaptureWaitPollsServingInstanceWithToken: --wait polls the instance
// that accepted the capture, with that instance's token.
func TestCaptureWaitPollsServingInstanceWithToken(t *testing.T) {
	inst := startEndpointRecorder(t, "tok-wait")
	cfgPath := writeServerConfig(t, inst.URL(), "tok-wait")

	out, err := executeCommand("capture", "wait content", "--wait", "-c", cfgPath)
	if err != nil {
		t.Fatalf("capture --wait: %v (out=%q)", err, out)
	}
	polls := inst.hitsOn("/api/v1/jobs/job_routed")
	if len(polls) == 0 {
		t.Fatal("--wait never polled the serving instance")
	}
	for _, p := range polls {
		if p.Auth != "Bearer tok-wait" {
			t.Errorf("poll Authorization = %q; want Bearer tok-wait", p.Auth)
		}
	}
}

// TestCaptureFailsOverWhenPrimaryUnreachable: a primary that cannot be
// dialed passes the capture to the next configured instance, as analyze
// does.
func TestCaptureFailsOverWhenPrimaryUnreachable(t *testing.T) {
	secondary := startEndpointRecorder(t, "tok-2")
	cfgPath := writeCaptureConfig(t, "server:\n  urls:\n    - "+closedServerURL(t)+
		"\n    - url: "+secondary.URL()+"\n      token: tok-2\n")

	out, err := executeCommand("capture", "failover content", "--wait", "-c", cfgPath)
	if err != nil {
		t.Fatalf("capture with unreachable primary: %v (out=%q)", err, out)
	}
	hits := secondary.hitsOn("/api/v1/analyze")
	if len(hits) != 1 || hits[0].Auth != "Bearer tok-2" {
		t.Errorf("secondary analyze hits = %+v; want one with Bearer tok-2", hits)
	}
	if polls := secondary.hitsOn("/api/v1/jobs/job_routed"); len(polls) == 0 {
		t.Error("--wait should poll the secondary that accepted the capture")
	}
}

// TestCaptureFailsOverOnCredentialRejection: a primary that rejects the
// credentials (refused before anything is stored) passes the capture on,
// with a warning naming the instance.
func TestCaptureFailsOverOnCredentialRejection(t *testing.T) {
	primary := startEndpointRecorder(t, "tok-real")
	secondary := startEndpointRecorder(t, "")
	cfgPath := writeCaptureConfig(t, "server:\n  urls:\n    - url: "+primary.URL()+
		"\n      token: tok-wrong\n    - "+secondary.URL()+"\n")

	out, err := executeCommand("capture", "auth content", "-c", cfgPath)
	if err != nil {
		t.Fatalf("capture with rejected primary credentials: %v (out=%q)", err, out)
	}
	if len(secondary.hitsOn("/api/v1/analyze")) != 1 {
		t.Error("capture should fall through to the secondary after a 401")
	}
	if !strings.Contains(out, primary.URL()) || !strings.Contains(out, "rejected credentials") {
		t.Errorf("expected a warning naming the rejecting instance; got %q", out)
	}
}

// TestCaptureDoesNotReplayLiveRejection: a live instance's non-auth
// rejection is surfaced, never replayed against the next instance.
func TestCaptureDoesNotReplayLiveRejection(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	t.Cleanup(primary.Close)
	secondary := startEndpointRecorder(t, "")
	cfgPath := writeCaptureConfig(t, "server:\n  urls:\n    - "+primary.URL+"\n    - "+secondary.URL()+"\n")

	_, err := executeCommand("capture", "rejected content", "-c", cfgPath)
	if err == nil || !strings.Contains(err.Error(), "dpkms returned 500") {
		t.Fatalf("expected the primary's 500 to surface; got %v", err)
	}
	if n := len(secondary.Hits()); n != 0 {
		t.Errorf("secondary got %d hits; a live rejection must not be replayed", n)
	}
}

// TestCaptureEndpointsUnconfiguredDefault: with nothing configured and no
// --server, capture targets the shared client default (loopback :8080),
// unauthenticated.
func TestCaptureEndpointsUnconfiguredDefault(t *testing.T) {
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = &config.Config{}
	resetAllFlags(rootCmd)

	got := captureEndpoints(captureCmd)
	want := []idxbridge.Endpoint{{URL: "http://127.0.0.1:8080"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("captureEndpoints() = %+v; want %+v", got, want)
	}
}
