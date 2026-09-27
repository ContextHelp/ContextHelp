package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// These tests pin the enqueue contract of capture tabs and capture
// history on the built binary: the one resolved instance takes the URLs
// or the command fails. Nothing is written locally, nothing is retried,
// and the history position is kept per instance.

// pointAt rewrites the env's config so server.url is url (plus extra
// config lines).
func (e *tabsEnv) pointAt(url, extra string) {
	e.t.Helper()
	cfg := fmt.Sprintf("server:\n  url: %s\n  token: %s\n%s", url, tabsTestToken, extra)
	if err := os.WriteFile(e.cfgPath, []byte(cfg), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// assertNoLocalDB fails when any ctxt/dpkms database file exists under
// the env's home: a failed send must never land in a local store.
func (e *tabsEnv) assertNoLocalDB() {
	e.t.Helper()
	_ = filepath.WalkDir(e.home, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err // a dir that doesn't exist holds no database
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), "db.sqlite") {
			e.t.Errorf("local database file created: %s", path)
		}
		return nil
	})
}

func TestCaptureTabs_UnreachableExits70WritesNothingLocally(t *testing.T) {
	e := newTabsEnv(t, "", workProfile(time.Now(), tabA, tabB))
	e.pointAt(testguard.ClosedServerURL, "")

	out, errOut := e.mustRun(70, tabsArgs()...)
	e.assertNoLocalDB()
	for _, s := range []string{out, errOut} {
		if strings.Contains(strings.ToLower(s), "queued locally") {
			t.Errorf("output claims a local queue:\n%s", s)
		}
	}
	if !strings.Contains(errOut, testguard.ClosedServerURL) {
		t.Errorf("stderr should name the unreachable instance:\n%s", errOut)
	}
}

func TestCaptureTabs_RejectedTokenExits5WithoutRetry(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			e := newTabsEnv(t, "", workProfile(time.Now(), tabA, tabB))
			e.status = status

			_, errOut := e.mustRun(5, tabsArgs()...)
			if n := len(e.requests()); n != 1 {
				t.Fatalf("server saw %d analyze requests; want exactly 1 (no retry, no next tab)", n)
			}
			e.assertNoLocalDB()
			if !strings.Contains(errOut, "UNAUTHORIZED") {
				t.Errorf("stderr should carry the UNAUTHORIZED class:\n%s", errOut)
			}
		})
	}
}

func TestCaptureHistory_UnreachableExits70KeepsPosition(t *testing.T) {
	now := hNow()
	e := newHistoryEnv(t, "", hVisit("https://example.com/a", now.Add(-time.Hour)))
	e.pointAt(testguard.ClosedServerURL, "")

	out, errOut := e.mustRun(70, historyArgs()...)
	e.assertNoLocalDB()
	if len(e.allPositions()) != 0 {
		t.Fatalf("an unreachable instance must not save a position: %v", e.allPositions())
	}
	for _, s := range []string{out, errOut} {
		if strings.Contains(strings.ToLower(s), "queued locally") {
			t.Errorf("output claims a local queue:\n%s", s)
		}
	}
}

func TestCaptureHistory_RejectedTokenExits5WithoutRetry(t *testing.T) {
	now := hNow()
	e := newHistoryEnv(t, "",
		hVisit("https://example.com/a", now.Add(-2*time.Hour)),
		hVisit("https://example.com/b", now.Add(-time.Hour)))
	e.status = http.StatusUnauthorized

	e.mustRun(5, historyArgs()...)
	if n := len(e.requests()); n != 1 {
		t.Fatalf("server saw %d analyze requests; want exactly 1 (no retry, no next visit)", n)
	}
	e.assertNoLocalDB()
	if got := e.positions(); len(got) != 0 {
		t.Fatalf("a rejected token must not move the position: %v", got)
	}
}

// Capturing history into instance A, then B, sends the full window to
// B: the position A saved is A's alone.
func TestCaptureHistory_PositionIsPerInstance(t *testing.T) {
	now := hNow()
	a := hVisit("https://example.com/a", now.Add(-2*time.Hour))
	b := hVisit("https://example.com/b", now.Add(-time.Hour))
	e := newHistoryEnv(t, "", a, b)

	other := newTabsEnv(t, "")
	e.pointAt(e.srv.URL, fmt.Sprintf("  urls:\n    - name: a\n      url: %s\n    - name: b\n      url: %s\n",
		e.srv.URL, other.srv.URL))

	e.mustRun(0, historyArgs("--instance", "a")...)
	assertSent(t, e, a.URL, b.URL)

	out, _ := e.mustRun(0, historyArgs("--instance", "b")...)
	if got := strings.Join(other.sentURLs(), " "); got != a.URL+" "+b.URL {
		t.Fatalf("instance b received %q; want the full window %q\n%s", got, a.URL+" "+b.URL, out)
	}

	all := e.allPositions()
	for _, inst := range []string{"a", "b"} {
		if got, ok := all[inst][historyKey]; !ok || !got.Equal(b.At) {
			t.Errorf("position %s/%s = %v (saved %v); want %v", inst, historyKey, got, ok, b.At)
		}
	}

	// A second run on a sends nothing new; b's position is untouched.
	e.resetRequests()
	e.mustRun(0, historyArgs("--instance", "a")...)
	assertSent(t, e)
}

// A version-1 position file (positions not per instance) stops the run
// with exit 3, is never rewritten, and the error says how to start over.
func TestCaptureHistory_VersionOnePositionFileNeedsReset(t *testing.T) {
	now := hNow()
	e := newHistoryEnv(t, "", hVisit("https://example.com/a", now.Add(-time.Hour)))
	body := `{"version":1,"positions":{"brave:Profile 1":"2000-01-01T00:00:00.000000Z"}}`
	e.writeState(body)

	_, errOut := e.mustRun(3, historyArgs()...)
	for _, want := range []string{e.statePath(), "version 1", "delete"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
	}
	if len(e.requests()) != 0 {
		t.Fatal("an old position file must stop the run before sending")
	}
	if string(e.stateBytes()) != body {
		t.Fatal("the old position file was rewritten")
	}
}

// runStdin runs args with stdin fed from content.
func (e *tabsEnv) runStdin(content string, args ...string) (stdout, stderr string, exit int) {
	e.t.Helper()
	cmd := exec.Command(e.bin, args...) // #nosec G204 -- the ctxt built from this checkout
	cmd.Env = e.env()
	cmd.Dir = e.home
	cmd.Stdin = strings.NewReader(content)
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

// Enqueue commands on the built binary: an unreachable instance exits 70,
// a rejected token exits 5, neither writes a local database.
func TestE2EEnqueueExitCodes(t *testing.T) {
	e := newTabsEnv(t, "", workProfile(time.Now(), tabA))
	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"invalid token"}}`))
	}))
	t.Cleanup(rejecting.Close)

	cases := []struct {
		name  string
		stdin string
		args  []string
		want  int
	}{
		{"analyze, accepted", "", []string{"analyze", "some text"}, 0},
		{"bare content, accepted", "some bare text", nil, 0},
		{"analyze, unreachable", "", []string{"analyze", "some text", "--server", testguard.ClosedServerURL}, 70},
		{"bare content, unreachable", "some bare text", []string{"--server", testguard.ClosedServerURL}, 70},
		{"analyze, rejected token", "", []string{"analyze", "some text", "--server", rejecting.URL}, 5},
		{"bare content, rejected token", "some bare text", []string{"--server", rejecting.URL}, 5},
		{"capture tabs, unreachable", "", append([]string{"-c", "server.url=" + testguard.ClosedServerURL}, tabsArgs()...), 70},
		{"capture tabs, rejected token", "", append([]string{"-c", "server.url=" + rejecting.URL}, tabsArgs()...), 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := e.runStdin(tc.stdin, tc.args...)
			if code != tc.want {
				t.Fatalf("ctxt %v: exit %d, want %d\nstdout:\n%s\nstderr:\n%s", tc.args, code, tc.want, out, errOut)
			}
			if strings.Contains(strings.ToLower(out+errOut), "queued locally") {
				t.Errorf("output claims a local queue:\n%s\n%s", out, errOut)
			}
		})
	}
	e.assertNoLocalDB()
}
