package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium/chromiumtest"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// These tests drive a built ctxt binary: exit codes are a property of
// the process, and kit validates the command tree at Execute time, so
// only a real invocation proves both. The browser is a synthetic user
// data dir (chromiumtest) reached through CTXT_BRAVE_USER_DATA_DIR, and
// the ctxt server is an httptest.Server configured as server.url.

const tabsTestToken = "tabs-test-token"

var (
	tabsBinaryPath string
	tabsBinaryErr  error
	tabsBinaryOnce sync.Once
)

// tabsBinary builds ctxt once per test run.
func tabsBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("capture tabs e2e: -short")
	}
	tabsBinaryOnce.Do(func() {
		root, err := tabsRepoRoot()
		if err != nil {
			tabsBinaryErr = err
			return
		}
		dir, err := os.MkdirTemp("", "ctxt-tabs-e2e-")
		if err != nil {
			tabsBinaryErr = err
			return
		}
		bin := filepath.Join(dir, "ctxt")
		build := exec.Command("go", "build", "-tags", "fts5", "-buildvcs=false", "-o", bin, "./cmd/ctxt")
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			tabsBinaryErr = fmt.Errorf("go build: %w\n%s", err, out)
			return
		}
		tabsBinaryPath = bin
	})
	if tabsBinaryErr != nil {
		t.Fatalf("build ctxt: %v", tabsBinaryErr)
	}
	return tabsBinaryPath
}

func tabsRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found above test dir")
		}
		dir = parent
	}
}

// tabsReq is one request the fake server received.
type tabsReq struct {
	Auth string
	Body service.AnalyzeRequest
}

type tabsEnv struct {
	t       *testing.T
	bin     string
	home    string
	cfgPath string
	udd     string
	srv     *httptest.Server

	mu   sync.Mutex
	reqs []tabsReq
	// fail makes the server reject an analyze request for these URLs.
	fail map[string]bool
	// extraEnv is appended to the process environment, e.g. a second
	// browser's CTXT_<BROWSER>_USER_DATA_DIR.
	extraEnv []string
}

// newTabsEnv writes a brave user data dir holding profiles and starts a
// fake ctxt server wired in through server.url + server.token. extraCfg
// is appended to the generated config file.
func newTabsEnv(t *testing.T, extraCfg string, profiles ...chromiumtest.Profile) *tabsEnv {
	t.Helper()
	e := &tabsEnv{
		t:    t,
		bin:  tabsBinary(t),
		home: t.TempDir(),
		udd:  chromiumtest.WriteUserDataDir(t, profiles...),
		fail: map[string]bool{},
	}
	e.srv = httptest.NewServer(http.HandlerFunc(e.serve))
	t.Cleanup(e.srv.Close)

	e.cfgPath = filepath.Join(e.home, "ctxt.yaml")
	cfg := fmt.Sprintf("server:\n  url: %s\n  token: %s\n%s", e.srv.URL, tabsTestToken, extraCfg)
	if err := os.WriteFile(e.cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *tabsEnv) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/health":
		w.WriteHeader(http.StatusOK)
	case r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost:
		var body service.AnalyzeRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		e.mu.Lock()
		e.reqs = append(e.reqs, tabsReq{Auth: r.Header.Get("Authorization"), Body: body})
		n := len(e.reqs)
		fail := e.fail[body.Content]
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": fmt.Sprintf("job_%d", n)})
	default:
		http.NotFound(w, r)
	}
}

func (e *tabsEnv) requests() []tabsReq {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]tabsReq(nil), e.reqs...)
}

func (e *tabsEnv) sentURLs() []string {
	var out []string
	for _, r := range e.requests() {
		out = append(out, r.Body.Content)
	}
	return out
}

// env is a hermetic process environment: no inherited ctxt, XDG or git
// state, everything rooted in the test's temp home.
func (e *tabsEnv) env() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(k, "CTXT_"), strings.HasPrefix(k, "XDG_"),
			strings.HasPrefix(k, "GIT_"), k == "HOME":
			continue
		}
		out = append(out, kv)
	}
	return append(append(
		out,
		"HOME="+e.home,
		"XDG_CONFIG_HOME="+filepath.Join(e.home, "config"),
		"XDG_DATA_HOME="+filepath.Join(e.home, "data"),
		"XDG_STATE_HOME="+filepath.Join(e.home, "state"),
		"XDG_CACHE_HOME="+filepath.Join(e.home, "cache"),
		"CTXT_CONFIG="+e.cfgPath,
		"CTXT_DATA_DIR="+filepath.Join(e.home, "data", "ctxt"),
		"CTXT_NO_CLIPBOARD=1",
		"CTXT_BRAVE_USER_DATA_DIR="+e.udd,
	), e.extraEnv...)
}

func (e *tabsEnv) run(args ...string) (stdout, stderr string, exit int) {
	e.t.Helper()
	cmd := exec.Command(e.bin, args...)
	cmd.Env = e.env()
	cmd.Dir = e.home
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	var xerr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &xerr):
		exit = xerr.ExitCode()
	default:
		e.t.Fatalf("run ctxt: %v", err)
	}
	return so.String(), se.String(), exit
}

// tabsArgs is the common invocation for the default fixture profile.
func tabsArgs(extra ...string) []string {
	return append([]string{"capture", "tabs", "--browser", "brave", "--browser-profile", "Work"}, extra...)
}

func workProfile(mtime time.Time, tabs ...chromiumtest.Tab) chromiumtest.Profile {
	return chromiumtest.Profile{DirName: "Profile 1", Name: "Work", ModTime: mtime, Session: chromiumtest.Session(tabs...)}
}

var (
	tabA   = chromiumtest.Tab{URL: "https://example.com/a", Title: "Alpha page"}
	tabB   = chromiumtest.Tab{URL: "https://example.org/b", Title: "Bravo page"}
	tabCRM = chromiumtest.Tab{URL: "https://crm.example.net/deal/42", Title: "Deal 42"}
)

// crmDeny denies the crm host for the Work profile only.
const crmDeny = `capture:
  url_filter:
    browsers:
      brave:
        profiles:
          work:
            deny: ["*://crm.example.net/*"]
`

// A bare host rule denies that host on any path; a non-http(s) scheme is
// refused by the builtin layer before any rule, and the dry run says so.
func TestCaptureTabs_DryRunShowsSchemeAndBareHostDecisions(t *testing.T) {
	const bareDeny = `capture:
  url_filter:
    deny: ["crm.example.net"]
`
	ftp := chromiumtest.Tab{URL: "ftp://files.example.org/pub/", Title: "Files"}
	about := chromiumtest.Tab{URL: "about:blank", Title: "New tab"}
	e := newTabsEnv(t, bareDeny, workProfile(time.Now(), tabA, tabCRM, ftp, about))

	out, errOut, code := e.run(tabsArgs("--dry-run")...)
	if code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	for _, want := range []string{
		`denied by deny rule "crm.example.net" (global)`,
		"builtin: scheme not captured (ftp)",
		"builtin: scheme not captured (about)",
		"1 would be sent, 3 denied",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
	if got := e.requests(); len(got) != 0 {
		t.Fatalf("dry run sent %d requests", len(got))
	}
}

func TestCaptureTabs_DryRunSendsNothing(t *testing.T) {
	e := newTabsEnv(t, crmDeny, workProfile(time.Now(), tabA, tabCRM, tabA))

	out, errOut, code := e.run(tabsArgs("--dry-run")...)
	if code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if got := e.requests(); len(got) != 0 {
		t.Fatalf("dry run sent %d requests: %+v", len(got), got)
	}
	for _, want := range []string{
		tabA.URL, tabA.Title, tabCRM.URL, tabCRM.Title,
		"allowed",
		`denied by deny rule "*://crm.example.net/*" (profile:brave/work)`,
		"duplicate",
		"session as of",
		"1 would be sent, 1 denied, 1 deduped",
		"nothing sent",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
}

func TestCaptureTabs_SendsAllowedToConfiguredServerWithToken(t *testing.T) {
	e := newTabsEnv(t, "", workProfile(time.Now(), tabA, tabB))

	out, errOut, code := e.run(tabsArgs()...)
	if code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	reqs := e.requests()
	if len(reqs) != 2 {
		t.Fatalf("server got %d requests, want 2 (tabs must go to server.url, not the default)", len(reqs))
	}
	for _, r := range reqs {
		if r.Auth != "Bearer "+tabsTestToken {
			t.Errorf("Authorization = %q, want the configured server.token", r.Auth)
		}
		// Same request shape `ctxt capture <url>` sends: the URL as
		// content and source, no pipeline, server-side detection.
		if r.Body.Source != r.Body.Content || r.Body.Pipeline != "" || r.Body.Type != "text" {
			t.Errorf("request shape = %+v", r.Body)
		}
	}
	if got := strings.Join(e.sentURLs(), " "); got != tabA.URL+" "+tabB.URL {
		t.Errorf("sent %q", got)
	}
	if !strings.Contains(out, "sent 2, denied 0, deduped 0, failed 0") {
		t.Errorf("summary missing:\n%s", out)
	}
}

func TestCaptureTabs_FocusProfilePassesThrough(t *testing.T) {
	e := newTabsEnv(t, "", workProfile(time.Now(), tabA))

	if out, errOut, code := e.run(tabsArgs("--profile", "research")...); code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	reqs := e.requests()
	if len(reqs) != 1 || reqs[0].Body.Profile != "research" {
		t.Fatalf("focus profile not forwarded: %+v", reqs)
	}
}

func TestCaptureTabs_DeniedTabNotSent(t *testing.T) {
	e := newTabsEnv(t, crmDeny, workProfile(time.Now(), tabA, tabCRM))

	out, errOut, code := e.run(tabsArgs()...)
	if code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if got := e.sentURLs(); len(got) != 1 || got[0] != tabA.URL {
		t.Fatalf("sent %v, want only %s", got, tabA.URL)
	}
	if strings.Contains(out, tabCRM.URL) {
		t.Errorf("a real run must not echo denied URLs:\n%s", out)
	}
	if !strings.Contains(out, "sent 1, denied 1, deduped 0, failed 0") {
		t.Errorf("summary missing:\n%s", out)
	}
}

func TestCaptureTabs_BuiltinDenyApplies(t *testing.T) {
	local := chromiumtest.Tab{URL: "http://localhost:3000/admin", Title: "Local"}
	e := newTabsEnv(t, "", workProfile(time.Now(), local, tabA))

	if _, errOut, code := e.run(tabsArgs()...); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := e.sentURLs(); len(got) != 1 || got[0] != tabA.URL {
		t.Fatalf("sent %v", got)
	}
}

func TestCaptureTabs_Dedupe(t *testing.T) {
	e := newTabsEnv(t, "", workProfile(time.Now(), tabA, tabB, tabA, tabA))

	out, errOut, code := e.run(tabsArgs()...)
	if code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if got := e.sentURLs(); len(got) != 2 {
		t.Fatalf("sent %v, want each URL once", got)
	}
	if !strings.Contains(out, "sent 2, denied 0, deduped 2, failed 0") {
		t.Errorf("summary missing:\n%s", out)
	}
}

func TestCaptureTabs_PartialFailure(t *testing.T) {
	e := newTabsEnv(t, crmDeny, workProfile(time.Now(), tabA, tabB, tabCRM, tabA))
	e.fail[tabA.URL] = true

	out, errOut, code := e.run(tabsArgs()...)
	if code != 1 {
		t.Fatalf("exit %d, want 1 when a send fails\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	// Failure on the first tab must not stop the second.
	if got := strings.Join(e.sentURLs(), " "); got != tabA.URL+" "+tabB.URL {
		t.Fatalf("attempted %q, want both allowed URLs", got)
	}
	if !strings.Contains(out, "sent 1, denied 1, deduped 1, failed 1") {
		t.Errorf("summary missing:\n%s", out)
	}
	if !strings.Contains(errOut, "1 of 2 sends failed") {
		t.Errorf("stderr missing failure count:\n%s", errOut)
	}
}

func TestCaptureTabs_ProfileResolutionErrors(t *testing.T) {
	now := time.Now()
	e := newTabsEnv(
		t, "",
		workProfile(now, tabA),
		chromiumtest.Profile{DirName: "Profile 2", Name: "Personal", ModTime: now, Session: chromiumtest.Session(tabB)},
		chromiumtest.Profile{DirName: "Profile 3", Name: "Personal", ModTime: now, Session: chromiumtest.Session(tabB)},
		chromiumtest.Profile{DirName: "Profile 4", Name: "Archive", Missing: true},
	)

	cases := []struct {
		name    string
		args    []string
		code    int
		stderrs []string
	}{
		{
			name:    "not found lists candidates",
			args:    []string{"capture", "tabs", "--browser", "brave", "--browser-profile", "Nope"},
			code:    2,
			stderrs: []string{`no profile "Nope"`, `"Work" (Profile 1)`, `"Personal" (Profile 2)`},
		},
		{
			name:    "ambiguous lists candidates",
			args:    []string{"capture", "tabs", "--browser", "brave", "--browser-profile", "personal"},
			code:    2,
			stderrs: []string{"ambiguous", "Profile 2", "Profile 3"},
		},
		{
			name:    "folder name disambiguates",
			args:    []string{"capture", "tabs", "--browser", "brave", "--browser-profile", "Profile 3", "--dry-run"},
			code:    0,
			stderrs: nil,
		},
		{
			name:    "missing profile dir",
			args:    []string{"capture", "tabs", "--browser", "brave", "--browser-profile", "Archive"},
			code:    3,
			stderrs: []string{"profile directory missing"},
		},
		{
			name:    "unknown browser",
			args:    []string{"capture", "tabs", "--browser", "netscape", "--browser-profile", "Work"},
			code:    2,
			stderrs: []string{"unknown browser", "brave"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := e.run(tc.args...)
			if code != tc.code {
				t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", code, tc.code, out, errOut)
			}
			for _, want := range tc.stderrs {
				if !strings.Contains(errOut, want) {
					t.Errorf("stderr missing %q:\n%s", want, errOut)
				}
			}
		})
	}
	if got := e.requests(); len(got) != 0 {
		t.Fatalf("resolution failures must send nothing, got %d", len(got))
	}
}

func TestCaptureTabs_NoBrowserInstall(t *testing.T) {
	e := newTabsEnv(t, "", workProfile(time.Now(), tabA))
	e.udd = filepath.Join(e.home, "no-such-browser")

	_, errOut, code := e.run(tabsArgs("--dry-run")...)
	if code != 3 {
		t.Fatalf("exit %d, want 3\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "CTXT_BRAVE_USER_DATA_DIR") {
		t.Errorf("stderr should name the override:\n%s", errOut)
	}
}

func TestCaptureTabs_StaleSessionNotice(t *testing.T) {
	stale := newTabsEnv(t, "", workProfile(time.Now().Add(-2*time.Hour), tabA))
	_, errOut, code := stale.run(tabsArgs("--dry-run")...)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "browser may be closed") {
		t.Errorf("stale session: stderr missing notice:\n%s", errOut)
	}

	fresh := newTabsEnv(t, "", workProfile(time.Now(), tabA))
	_, errOut, _ = fresh.run(tabsArgs("--dry-run")...)
	if strings.Contains(errOut, "browser may be closed") {
		t.Errorf("fresh session: unexpected notice:\n%s", errOut)
	}
}

func TestCaptureTabs_UnknownProfileKeyWarning(t *testing.T) {
	cfg := `capture:
  url_filter:
    browsers:
      brave:
        profiles:
          work:
            deny: ["*://crm.example.net/*"]
          Contractor:
            deny: ["*://other.example.net/*"]
      chrome:
        profiles:
          Elsewhere:
            deny: ["*://x.example.net/*"]
`
	e := newTabsEnv(t, cfg, workProfile(time.Now(), tabA))

	_, errOut, code := e.run(tabsArgs("--dry-run")...)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(strings.ToLower(errOut), "capture.url_filter.browsers.brave.profiles.contractor matches no brave profile") {
		t.Errorf("missing warning for the unmatched key:\n%s", errOut)
	}
	if strings.Contains(errOut, "profiles.work ") || strings.Contains(errOut, "Elsewhere") {
		t.Errorf("warned about a matching key or another browser:\n%s", errOut)
	}
}

// tabsDoc mirrors the structured output document.
type tabsDoc struct {
	Command string `json:"command"`
	DryRun  bool   `json:"dry_run"`
	Browser string `json:"browser"`
	Profile struct {
		Name string `json:"name"`
		Dir  string `json:"dir"`
	} `json:"profile"`
	Session struct {
		AsOf  time.Time `json:"as_of"`
		Path  string    `json:"path"`
		Stale bool      `json:"stale"`
	} `json:"session"`
	Tabs []struct {
		Window   int32  `json:"window"`
		Index    int    `json:"index"`
		URL      string `json:"url"`
		Title    string `json:"title"`
		Allowed  bool   `json:"allowed"`
		Reason   string `json:"reason"`
		Decision string `json:"decision"`
		Status   string `json:"status"`
		JobID    string `json:"job_id"`
		Error    string `json:"error"`
	} `json:"tabs"`
	Summary struct {
		Total   int `json:"total"`
		Sent    int `json:"sent"`
		Denied  int `json:"denied"`
		Deduped int `json:"deduped"`
		Failed  int `json:"failed"`
	} `json:"summary"`
	Warnings []string `json:"warnings"`
}

func TestCaptureTabs_JSONShape(t *testing.T) {
	mtime := time.Now().Add(-time.Minute).Truncate(time.Second)
	e := newTabsEnv(t, crmDeny, workProfile(mtime, tabA, tabCRM, tabA))

	// Global flag before and after the leaf: both positions must work.
	for _, args := range [][]string{
		append([]string{"--format", "json"}, tabsArgs("--dry-run")...),
		tabsArgs("--dry-run", "--format", "json"),
	} {
		out, errOut, code := e.run(args...)
		if code != 0 {
			t.Fatalf("%v: exit %d\n%s", args, code, errOut)
		}
		var doc tabsDoc
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("%v: stdout is not one JSON document: %v\n%s", args, err, out)
		}
		if doc.Command != "ctxt capture tabs" || !doc.DryRun || doc.Browser != "brave" ||
			doc.Profile.Name != "Work" || doc.Profile.Dir != "Profile 1" {
			t.Errorf("header = %+v", doc)
		}
		if !doc.Session.AsOf.Equal(mtime) || doc.Session.Stale || !strings.HasSuffix(doc.Session.Path, chromiumtest.SessionFileName) {
			t.Errorf("session = %+v, want as_of %v", doc.Session, mtime)
		}
		if len(doc.Tabs) != 3 {
			t.Fatalf("tabs = %+v", doc.Tabs)
		}
		want := []struct {
			url, status, decision string
			allowed               bool
		}{
			{tabA.URL, "would_send", "allowed", true},
			{tabCRM.URL, "denied", "deny_rule", false},
			{tabA.URL, "duplicate", "allowed", true},
		}
		for i, w := range want {
			got := doc.Tabs[i]
			if got.URL != w.url || got.Status != w.status || got.Decision != w.decision || got.Allowed != w.allowed || got.Reason == "" {
				t.Errorf("tabs[%d] = %+v, want %+v", i, got, w)
			}
		}
		if doc.Summary.Total != 3 || doc.Summary.Sent != 0 || doc.Summary.Denied != 1 || doc.Summary.Deduped != 1 {
			t.Errorf("summary = %+v", doc.Summary)
		}
	}
	if got := e.requests(); len(got) != 0 {
		t.Fatalf("dry run sent %d requests", len(got))
	}

	out, errOut, code := e.run(tabsArgs("--format", "json")...)
	if code != 0 {
		t.Fatalf("real run: exit %d\n%s", code, errOut)
	}
	var doc tabsDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("real run: %v\n%s", err, out)
	}
	if doc.DryRun || doc.Summary.Sent != 1 || doc.Tabs[0].Status != "sent" || doc.Tabs[0].JobID == "" {
		t.Errorf("real run doc = %+v", doc)
	}
}

// URLs and titles reach stdout (the user-facing listing) only; nothing
// written to stderr, logs included at -V, may carry them.
func TestCaptureTabs_StderrNeverCarriesURLs(t *testing.T) {
	e := newTabsEnv(t, crmDeny, workProfile(time.Now().Add(-2*time.Hour), tabA, tabB, tabCRM))
	e.fail[tabB.URL] = true

	for _, args := range [][]string{
		tabsArgs("-V"),
		tabsArgs("-V", "--dry-run"),
	} {
		_, errOut, _ := e.run(args...)
		for _, tab := range []chromiumtest.Tab{tabA, tabB, tabCRM} {
			for _, secret := range []string{tab.URL, tab.Title, "crm.example.net/deal"} {
				if strings.Contains(errOut, secret) {
					t.Errorf("%v: stderr leaks %q:\n%s", args, secret, errOut)
				}
			}
		}
		if !strings.Contains(errOut, chromiumtest.SessionFileName) {
			t.Errorf("%v: -V should name the session file:\n%s", args, errOut)
		}
	}
}
