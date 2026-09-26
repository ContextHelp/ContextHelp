package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium/chromiumtest"
)

// These tests drive a built ctxt binary against a synthetic brave user
// data dir whose "Work" profile (folder "Profile 1") holds a History
// database with the real schema. The saved position lives under the
// test's XDG_STATE_HOME, and sends go to the tabsEnv httptest server.

const historyKey = "brave:Profile 1"

// hNow is a per-test reference instant with microsecond precision, the
// resolution of Chromium visit times.
func hNow() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func hVisit(url string, at time.Time) chromiumtest.Visit {
	return chromiumtest.Visit{URL: url, Title: "Title of " + url, At: at}
}

// newHistoryEnv is a tabsEnv whose Work profile has visits in History.
func newHistoryEnv(t *testing.T, extraCfg string, visits ...chromiumtest.Visit) *tabsEnv {
	t.Helper()
	e := newTabsEnv(t, extraCfg, chromiumtest.Profile{DirName: "Profile 1", Name: "Work"})
	chromiumtest.WriteHistory(t, filepath.Join(e.udd, "Profile 1"), visits...)
	return e
}

// browse appends visits to the Work profile's History.
func (e *tabsEnv) browse(visits ...chromiumtest.Visit) {
	e.t.Helper()
	chromiumtest.WriteHistory(e.t, filepath.Join(e.udd, "Profile 1"), visits...)
}

func (e *tabsEnv) statePath() string {
	return filepath.Join(e.home, "state", "ctxt", "ambient", "browserhistory.state")
}

// positions reads the state file; a missing file is nil.
func (e *tabsEnv) positions() map[string]time.Time {
	e.t.Helper()
	data, err := os.ReadFile(e.statePath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		e.t.Fatal(err)
	}
	var f struct {
		Positions map[string]time.Time `json:"positions"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		e.t.Fatalf("state file: %v\n%s", err, data)
	}
	return f.Positions
}

func (e *tabsEnv) writeState(body string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Dir(e.statePath()), 0o700); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(e.statePath(), []byte(body), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

func (e *tabsEnv) stateBytes() []byte {
	e.t.Helper()
	data, err := os.ReadFile(e.statePath())
	if err != nil && !os.IsNotExist(err) {
		e.t.Fatal(err)
	}
	return data
}

func (e *tabsEnv) resetRequests() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reqs = nil
}

func historyArgs(extra ...string) []string {
	return append([]string{"capture", "history", "--browser", "brave", "--browser-profile", "Work"}, extra...)
}

// mustRun runs args and fails unless the exit code is want.
func (e *tabsEnv) mustRun(want int, args ...string) (stdout, stderr string) {
	e.t.Helper()
	out, errOut, code := e.run(args...)
	if code != want {
		e.t.Fatalf("%v: exit %d, want %d\nstdout:\n%s\nstderr:\n%s", args, code, want, out, errOut)
	}
	return out, errOut
}

func assertSent(t *testing.T, e *tabsEnv, want ...string) {
	t.Helper()
	got := e.sentURLs()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("sent %v, want %v", got, want)
	}
}

func assertPosition(t *testing.T, e *tabsEnv, want time.Time) {
	t.Helper()
	got, ok := e.positions()[historyKey]
	if !ok || !got.Equal(want) {
		t.Fatalf("position %s = %v (saved %v), want %v", historyKey, got, ok, want)
	}
}

func assertNoPosition(t *testing.T, e *tabsEnv) {
	t.Helper()
	if got, ok := e.positions()[historyKey]; ok {
		t.Fatalf("position %s = %v, want none", historyKey, got)
	}
}

func TestCaptureHistory_FirstRunDefaultsTo24h(t *testing.T) {
	now := hNow()
	old := hVisit("https://example.com/two-days-ago", now.Add(-48*time.Hour))
	b := hVisit("https://example.com/b", now.Add(-3*time.Hour))
	c := hVisit("https://example.org/c", now.Add(-time.Hour))
	e := newHistoryEnv(t, "", old, b, c)

	out, _ := e.mustRun(0, historyArgs()...)
	assertSent(t, e, b.URL, c.URL)
	assertPosition(t, e, c.At)
	for _, want := range []string{"first run", "sent 2, denied 0, deduped 0, failed 0 (2 visits)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCaptureHistory_ConfigLookbackOverride(t *testing.T) {
	now := hNow()
	old := hVisit("https://example.com/two-days-ago", now.Add(-48*time.Hour))
	older := hVisit("https://example.com/four-days-ago", now.Add(-96*time.Hour))
	b := hVisit("https://example.com/b", now.Add(-time.Hour))

	e := newHistoryEnv(t, "capture:\n  history:\n    initial_lookback: 72h\n", older, old, b)
	e.mustRun(0, historyArgs()...)
	assertSent(t, e, old.URL, b.URL)
	assertPosition(t, e, b.At)

	// -c key=value layers the same setting.
	e2 := newHistoryEnv(t, "", older, old, b)
	e2.mustRun(0, historyArgs("-c", "capture.history.initial_lookback=100h")...)
	assertSent(t, e2, older.URL, old.URL, b.URL)
}

func TestCaptureHistory_InvalidLookback(t *testing.T) {
	now := hNow()
	for _, v := range []string{"0s", "-1h"} {
		t.Run(v, func(t *testing.T) {
			e := newHistoryEnv(t, "capture:\n  history:\n    initial_lookback: "+v+"\n",
				hVisit("https://example.com/a", now.Add(-time.Hour)))
			_, errOut := e.mustRun(2, historyArgs()...)
			if !strings.Contains(errOut, "capture.history.initial_lookback") {
				t.Errorf("stderr should name the key:\n%s", errOut)
			}
			if len(e.requests()) != 0 || e.positions() != nil {
				t.Fatalf("invalid lookback must send and save nothing")
			}
			// A bounded backfill never uses the lookback, so it still runs.
			e.mustRun(0, historyArgs("--since", "2h", "--until", "1m", "--dry-run")...)
		})
	}
}

func TestCaptureHistory_IncrementalNoReplayAndOfflineGap(t *testing.T) {
	now := hNow()
	a := hVisit("https://example.com/a", now.Add(-3*time.Hour))
	b := hVisit("https://example.com/b", now.Add(-2*time.Hour))
	e := newHistoryEnv(t, "", a, b)

	e.mustRun(0, historyArgs()...)
	assertSent(t, e, a.URL, b.URL)
	assertPosition(t, e, b.At)

	// Nothing new: nothing sent, position unchanged.
	e.resetRequests()
	out, _ := e.mustRun(0, historyArgs()...)
	assertSent(t, e)
	assertPosition(t, e, b.At)
	if !strings.Contains(out, "sent 0") {
		t.Errorf("empty run summary:\n%s", out)
	}

	// Browsing while no run happened (machine offline, ctxt stopped):
	// the next run picks every such visit up exactly once, in order.
	c := hVisit("https://example.com/c", now.Add(-90*time.Minute))
	d := hVisit("https://example.com/d", now.Add(-30*time.Minute))
	f := hVisit("https://example.com/f", now.Add(-10*time.Minute))
	e.browse(c, d, f)
	e.resetRequests()
	e.mustRun(0, historyArgs()...)
	assertSent(t, e, c.URL, d.URL, f.URL)
	assertPosition(t, e, f.At)

	e.resetRequests()
	e.mustRun(0, historyArgs()...)
	assertSent(t, e)
}

// Paging through VisitsSince must not drop the rest of a large window.
func TestCaptureHistory_IncrementalPagesPastBatchSize(t *testing.T) {
	now := hNow()
	var visits []chromiumtest.Visit
	for i := range 1203 {
		visits = append(visits, hVisit(fmt.Sprintf("https://example.com/p/%d", i),
			now.Add(-2*time.Hour).Add(time.Duration(i)*time.Second)))
	}
	e := newHistoryEnv(t, "", visits...)
	out, _ := e.mustRun(0, historyArgs("--dry-run")...)
	if !strings.Contains(out, "1203 would be sent") {
		t.Fatalf("dry run should count every page:\n%s", lastLines(out, 3))
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func TestCaptureHistory_SinceAloneAdvances(t *testing.T) {
	now := hNow()
	a := hVisit("https://example.com/a", now.Add(-5*time.Hour))
	b := hVisit("https://example.com/b", now.Add(-2*time.Hour))
	c := hVisit("https://example.com/c", now.Add(-time.Hour))
	e := newHistoryEnv(t, "", a, b, c)

	// --since as the explicit first-run start.
	e.mustRun(0, historyArgs("--since", "3h")...)
	assertSent(t, e, b.URL, c.URL)
	assertPosition(t, e, c.At)

	// The incremental run that follows resumes after it.
	d := hVisit("https://example.com/d", now.Add(-30*time.Minute))
	e.browse(d)
	e.resetRequests()
	e.mustRun(0, historyArgs()...)
	assertSent(t, e, d.URL)
	assertPosition(t, e, d.At)

	// A --since earlier than the position resends that window and never
	// moves the position back, even when a send fails early in it.
	e.resetRequests()
	e.fail[b.URL] = true
	e.mustRun(1, historyArgs("--since", "6h")...)
	assertSent(t, e, a.URL, b.URL, c.URL, d.URL)
	assertPosition(t, e, d.At)
}

func TestCaptureHistory_BoundedBackfillLeavesPositionUntouched(t *testing.T) {
	now := hNow()
	a := hVisit("https://example.com/a", now.Add(-5*time.Hour))
	b := hVisit("https://example.com/b", now.Add(-2*time.Hour))
	c := hVisit("https://example.com/c", now.Add(-time.Hour))

	// No saved position: a backfill does not create one.
	e := newHistoryEnv(t, "", a, b, c)
	e.mustRun(0, historyArgs("--until", "90m")...)
	assertSent(t, e, a.URL, b.URL)
	if _, err := os.Stat(e.statePath()); !os.IsNotExist(err) {
		t.Fatalf("backfill created the state file: %v", err)
	}

	// A saved position is neither read nor moved.
	e.writeState(`{"version":1,"positions":{"brave:Profile 1":"2000-01-01T00:00:00.000000Z"}}`)
	before := e.stateBytes()
	for _, args := range [][]string{
		historyArgs("--since", "6h", "--until", "90m"),
		historyArgs("--range", now.Add(-6*time.Hour).Format(time.RFC3339)+".."+now.Add(-90*time.Minute).Format(time.RFC3339)),
		historyArgs("--range", now.Add(-6*time.Hour).Format(time.RFC3339)+"..", "--range", "..2001-01-01"),
		historyArgs("--until", "1m"),
	} {
		e.resetRequests()
		e.mustRun(0, args...)
		if len(e.requests()) == 0 {
			t.Errorf("%v: backfill sent nothing", args)
		}
		if !bytes.Equal(e.stateBytes(), before) {
			t.Fatalf("%v: backfill changed the state file:\n%s", args, e.stateBytes())
		}
	}
}

func TestCaptureHistory_DryRunTouchesNothing(t *testing.T) {
	now := hNow()
	a := hVisit("https://example.com/a", now.Add(-3*time.Hour))
	crm := hVisit("https://crm.example.net/deal/42", now.Add(-2*time.Hour))
	e := newHistoryEnv(t, crmDeny, a, crm)

	for _, args := range [][]string{
		historyArgs("--dry-run"),
		historyArgs("--since", "7d", "--dry-run"),
		historyArgs("--range", "2001-01-01", "--dry-run"),
	} {
		e.mustRun(0, args...)
	}
	if len(e.requests()) != 0 {
		t.Fatalf("dry run sent %d requests", len(e.requests()))
	}
	if _, err := os.Stat(e.statePath()); !os.IsNotExist(err) {
		t.Fatalf("dry run created the state file: %v", err)
	}

	// Not even read: a corrupt file does not stop a dry run.
	e.writeState("{not json")
	out, _ := e.mustRun(0, historyArgs("--dry-run")...)
	if string(e.stateBytes()) != "{not json" {
		t.Fatalf("dry run changed the state file")
	}
	for _, want := range []string{
		"saved position not read",
		"2 visits: 1 allowed, 1 denied, 0 deduped",
		"would send", a.URL,
		`denied by deny rule "*://crm.example.net/*" (profile:brave/work)`,
		"dry run: 1 would be sent, 1 denied, 0 deduped (2 visits); nothing sent",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
	e.mustRun(0, historyArgs("--reset-position", "--dry-run")...)
	if string(e.stateBytes()) != "{not json" {
		t.Fatalf("reset dry run changed the state file")
	}
}

func TestCaptureHistory_DryRunCountsPerRange(t *testing.T) {
	e := newHistoryEnv(t, crmDeny,
		hVisit("https://example.com/a", time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)),
		hVisit("https://crm.example.net/x", time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)),
		hVisit("https://example.com/a", time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)),
		hVisit("https://example.com/b", time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)),
	)
	out, _ := e.mustRun(0, historyArgs("--tz", "UTC", "--range", "2026-09-10", "--range", "2026-09-12", "--dry-run")...)
	for _, want := range []string{
		"2026-09-10 00:00:00 UTC .. 2026-09-11 00:00:00 UTC: 2 visits: 1 allowed, 1 denied, 0 deduped",
		"2026-09-12 00:00:00 UTC .. 2026-09-13 00:00:00 UTC: 2 visits: 1 allowed, 0 denied, 1 deduped",
		"dry run: 2 would be sent, 1 denied, 1 deduped (4 visits); nothing sent",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCaptureHistory_PartialFailureAdvancesToSafePoint(t *testing.T) {
	now := hNow()
	a := hVisit("https://example.com/a", now.Add(-3*time.Hour))
	b := hVisit("https://example.com/b", now.Add(-2*time.Hour))
	c := hVisit("https://example.com/c", now.Add(-time.Hour))
	e := newHistoryEnv(t, "", a, b, c)
	e.fail[b.URL] = true

	_, errOut := e.mustRun(1, historyArgs()...)
	// The failure does not stop the later sends...
	assertSent(t, e, a.URL, b.URL, c.URL)
	// ...but the position stops before it, so nothing is skipped.
	assertPosition(t, e, a.At)
	if !strings.Contains(errOut, "1 of 3 sends failed") {
		t.Errorf("stderr missing failure count:\n%s", errOut)
	}

	delete(e.fail, b.URL)
	e.resetRequests()
	e.mustRun(0, historyArgs()...)
	assertSent(t, e, b.URL, c.URL)
	assertPosition(t, e, c.At)
}

// A visit sharing its time with the failed one must not become the
// safe point: the next run reads strictly after the position.
func TestCaptureHistory_PartialFailureWithTimestampTie(t *testing.T) {
	now := hNow()
	a := hVisit("https://example.com/a", now.Add(-3*time.Hour))
	b1 := hVisit("https://example.com/b1", now.Add(-2*time.Hour))
	b2 := hVisit("https://example.com/b2", now.Add(-2*time.Hour))
	e := newHistoryEnv(t, "", a, b1, b2)
	e.fail[b2.URL] = true

	e.mustRun(1, historyArgs()...)
	assertPosition(t, e, a.At)

	delete(e.fail, b2.URL)
	e.resetRequests()
	e.mustRun(0, historyArgs()...)
	assertSent(t, e, b1.URL, b2.URL)
}

func TestCaptureHistory_ResetPosition(t *testing.T) {
	now := hNow()
	a := hVisit("https://example.com/a", now.Add(-3*time.Hour))
	e := newHistoryEnv(t, "", a)
	e.writeState(`{"version":1,"positions":{"brave:Profile 1":"` + now.Format("2006-01-02T15:04:05.000000Z07:00") +
		`","brave:Profile 9":"2026-01-01T00:00:00.000000Z"}}`)

	e.mustRun(0, historyArgs()...)
	assertSent(t, e) // position is newer than every visit

	out, _ := e.mustRun(0, historyArgs("--reset-position")...)
	assertNoPosition(t, e)
	if _, ok := e.positions()["brave:Profile 9"]; !ok {
		t.Fatalf("reset removed another profile's position")
	}
	if len(e.requests()) != 0 {
		t.Fatalf("--reset-position must only reset")
	}
	if !strings.Contains(out, historyKey) {
		t.Errorf("output should name the key:\n%s", out)
	}

	// The next incremental run starts over from the initial lookback.
	e.mustRun(0, historyArgs()...)
	assertSent(t, e, a.URL)

	for _, extra := range [][]string{{"--since", "1h"}, {"--until", "1h"}, {"--range", "2026-09-10"}} {
		_, errOut := e.mustRun(2, historyArgs(append([]string{"--reset-position"}, extra...)...)...)
		if !strings.Contains(errOut, "--reset-position") {
			t.Errorf("%v: stderr should name the conflict:\n%s", extra, errOut)
		}
	}
}

func TestCaptureHistory_CorruptPositionFile(t *testing.T) {
	now := hNow()
	a := hVisit("https://example.com/a", now.Add(-3*time.Hour))
	e := newHistoryEnv(t, "", a)
	e.writeState("{not json")

	for _, args := range [][]string{
		historyArgs(),
		historyArgs("--since", "5h"),
		historyArgs("--reset-position"),
	} {
		_, errOut := e.mustRun(3, args...)
		if !strings.Contains(errOut, e.statePath()) {
			t.Errorf("%v: stderr should name the file:\n%s", args, errOut)
		}
	}
	if len(e.requests()) != 0 {
		t.Fatalf("a corrupt position file must stop the run before sending")
	}
	if string(e.stateBytes()) != "{not json" {
		t.Fatalf("corrupt file was rewritten")
	}
	// A bounded backfill never reads the file.
	e.mustRun(0, historyArgs("--since", "5h", "--until", "1m")...)
	assertSent(t, e, a.URL)
}

func TestCaptureHistory_FilterAndDedupe(t *testing.T) {
	now := hNow()
	a := hVisit("https://example.com/a", now.Add(-4*time.Hour))
	crm := hVisit("https://crm.example.net/deal/42", now.Add(-3*time.Hour))
	local := hVisit("http://localhost:3000/admin", now.Add(-150*time.Minute))
	frame := chromiumtest.Visit{URL: "https://ads.example.com/frame", At: now.Add(-140 * time.Minute), Transition: chromiumtest.TransitionAutoSubframe}
	a2 := hVisit("https://example.com/a", now.Add(-2*time.Hour))
	b := hVisit("https://example.org/b", now.Add(-time.Hour))
	e := newHistoryEnv(t, crmDeny, a, crm, local, frame, a2, b)

	out, _ := e.mustRun(0, historyArgs()...)
	assertSent(t, e, a.URL, b.URL)
	if !strings.Contains(out, "sent 2, denied 2, deduped 1, failed 0 (5 visits)") {
		t.Errorf("summary missing:\n%s", out)
	}
	if strings.Contains(out, crm.URL) || strings.Contains(out, local.URL) {
		t.Errorf("a real run must not echo denied URLs:\n%s", out)
	}
	// Denied and deduped visits are handled: the position passes them.
	assertPosition(t, e, b.At)
}

func TestCaptureHistory_SendShapeAndFocusProfile(t *testing.T) {
	a := hVisit("https://example.com/a", hNow().Add(-time.Hour))
	e := newHistoryEnv(t, "", a)
	e.mustRun(0, historyArgs("--profile", "research")...)
	reqs := e.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %+v", reqs)
	}
	r := reqs[0]
	if r.Auth != "Bearer "+tabsTestToken || r.Body.Content != a.URL || r.Body.Source != a.URL ||
		r.Body.Type != "text" || r.Body.Pipeline != "" || r.Body.Profile != "research" {
		t.Errorf("request = %+v", r)
	}
}

func TestCaptureHistory_UnknownProfileKeyWarning(t *testing.T) {
	cfg := crmDeny + `          Contractor:
            deny: ["*://other.example.net/*"]
`
	e := newHistoryEnv(t, cfg, hVisit("https://example.com/a", hNow().Add(-time.Hour)))
	_, errOut := e.mustRun(0, historyArgs("--dry-run")...)
	if !strings.Contains(strings.ToLower(errOut), "capture.url_filter.browsers.brave.profiles.contractor matches no brave profile") {
		t.Errorf("missing warning:\n%s", errOut)
	}
}

func TestCaptureHistory_ExitCodes(t *testing.T) {
	now := hNow()
	e := newHistoryEnv(t, "", hVisit("https://example.com/a", now.Add(-time.Hour)))
	// A profile folder without a History database.
	noHist := newTabsEnv(t, "", chromiumtest.Profile{DirName: "Profile 1", Name: "Work"})

	cases := []struct {
		name    string
		env     *tabsEnv
		args    []string
		code    int
		stderrs []string
	}{
		{"ok", e, historyArgs("--dry-run"), 0, nil},
		{"missing --browser", e, []string{"capture", "history", "--browser-profile", "Work"}, 2, []string{"--browser"}},
		{"missing --browser-profile", e, []string{"capture", "history", "--browser", "brave"}, 2, []string{"--browser-profile"}},
		{"unknown browser", e, []string{"capture", "history", "--browser", "netscape", "--browser-profile", "Work"}, 2, []string{"unknown browser"}},
		{"unknown profile", e, []string{"capture", "history", "--browser", "brave", "--browser-profile", "Nope"}, 2, []string{`no profile "Nope"`}},
		{"bad --since", e, historyArgs("--since", "yesterday"), 2, []string{"--since"}},
		{"empty --since", e, historyArgs("--since", ""), 2, []string{"--since"}},
		{"empty --range", e, historyArgs("--range", ""), 2, []string{"--range"}},
		{"inverted range", e, historyArgs("--range", "2026-09-10..2026-09-01"), 2, []string{"inverted"}},
		{"range with since", e, historyArgs("--range", "2026-09-10", "--since", "1d"), 2, []string{"--range"}},
		{"bad --tz", e, historyArgs("--tz", "Mars/Olympus", "--since", "1d"), 2, []string{"--tz"}},
		{"no History", noHist, historyArgs(), 3, []string{"History"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errOut := tc.env.mustRun(tc.code, tc.args...)
			for _, want := range tc.stderrs {
				if !strings.Contains(errOut, want) {
					t.Errorf("stderr missing %q:\n%s", want, errOut)
				}
			}
		})
	}
	if len(e.requests())+len(noHist.requests()) != 0 {
		t.Fatalf("failed invocations must send nothing")
	}
	if e.positions() != nil || noHist.positions() != nil {
		t.Fatalf("failed invocations must save nothing")
	}

	// Browser not installed.
	e.udd = filepath.Join(e.home, "no-such-browser")
	_, errOut := e.mustRun(3, historyArgs("--dry-run")...)
	if !strings.Contains(errOut, "CTXT_BRAVE_USER_DATA_DIR") {
		t.Errorf("stderr should name the override:\n%s", errOut)
	}
}

// historyDoc mirrors the structured output document.
type historyDoc struct {
	Command string `json:"command"`
	DryRun  bool   `json:"dry_run"`
	Browser string `json:"browser"`
	Mode    string `json:"mode"`
	Profile struct {
		Name string `json:"name"`
		Dir  string `json:"dir"`
	} `json:"profile"`
	Ranges []struct {
		From    *time.Time `json:"from"`
		To      *time.Time `json:"to"`
		Visits  int        `json:"visits"`
		Allowed int        `json:"allowed"`
		Denied  int        `json:"denied"`
		Deduped int        `json:"deduped"`
	} `json:"ranges"`
	Position *struct {
		Key    string     `json:"key"`
		Path   string     `json:"path"`
		Start  string     `json:"start"`
		Before *time.Time `json:"before"`
		After  *time.Time `json:"after"`
	} `json:"position"`
	Visits []struct {
		URL       string    `json:"url"`
		Title     string    `json:"title"`
		VisitedAt time.Time `json:"visited_at"`
		Allowed   bool      `json:"allowed"`
		Decision  string    `json:"decision"`
		Reason    string    `json:"reason"`
		Status    string    `json:"status"`
		JobID     string    `json:"job_id"`
		Error     string    `json:"error"`
	} `json:"visits"`
	Sampled bool `json:"sampled"`
	Summary struct {
		Total     int `json:"total"`
		WouldSend int `json:"would_send"`
		Sent      int `json:"sent"`
		Denied    int `json:"denied"`
		Deduped   int `json:"deduped"`
		Failed    int `json:"failed"`
	} `json:"summary"`
	Warnings []string `json:"warnings"`
}

func decodeHistoryDoc(t *testing.T, out string) historyDoc {
	t.Helper()
	var doc historyDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, out)
	}
	return doc
}

func TestCaptureHistory_JSONShape(t *testing.T) {
	now := hNow()
	a := hVisit("https://example.com/a", now.Add(-3*time.Hour))
	crm := hVisit("https://crm.example.net/deal/42", now.Add(-2*time.Hour))
	a2 := hVisit("https://example.com/a", now.Add(-time.Hour))
	e := newHistoryEnv(t, crmDeny, a, crm, a2)

	for _, args := range [][]string{
		append([]string{"--format", "json"}, historyArgs("--dry-run")...),
		historyArgs("--dry-run", "--format", "json"),
	} {
		out, _ := e.mustRun(0, args...)
		doc := decodeHistoryDoc(t, out)
		if doc.Command != "ctxt capture history" || !doc.DryRun || doc.Browser != "brave" || doc.Mode != "incremental" ||
			doc.Profile.Name != "Work" || doc.Profile.Dir != "Profile 1" || !doc.Sampled || doc.Position != nil {
			t.Errorf("header = %+v", doc)
		}
		if len(doc.Ranges) != 1 || doc.Ranges[0].From == nil || doc.Ranges[0].To != nil ||
			doc.Ranges[0].Visits != 3 || doc.Ranges[0].Allowed != 1 || doc.Ranges[0].Denied != 1 || doc.Ranges[0].Deduped != 1 {
			t.Errorf("ranges = %+v", doc.Ranges)
		}
		if len(doc.Visits) != 2 || doc.Visits[0].Status != "would_send" || doc.Visits[0].URL != a.URL ||
			!doc.Visits[0].VisitedAt.Equal(a.At) || doc.Visits[0].Decision != "allowed" ||
			doc.Visits[1].Status != "denied" || doc.Visits[1].Decision != "deny_rule" || doc.Visits[1].Reason == "" {
			t.Errorf("visits = %+v", doc.Visits)
		}
		if doc.Summary.Total != 3 || doc.Summary.WouldSend != 1 || doc.Summary.Denied != 1 || doc.Summary.Deduped != 1 {
			t.Errorf("summary = %+v", doc.Summary)
		}
	}
	if len(e.requests()) != 0 {
		t.Fatalf("dry run sent requests")
	}

	out, _ := e.mustRun(0, historyArgs("--format", "json")...)
	doc := decodeHistoryDoc(t, out)
	if doc.DryRun || doc.Sampled || doc.Summary.Sent != 1 || len(doc.Visits) != 1 ||
		doc.Visits[0].Status != "sent" || doc.Visits[0].JobID == "" {
		t.Errorf("real run doc = %+v", doc)
	}
	p := doc.Position
	if p == nil || p.Key != historyKey || p.Path != e.statePath() || p.Start != "initial_lookback" ||
		p.Before != nil || p.After == nil || !p.After.Equal(a2.At) {
		t.Errorf("position = %+v", p)
	}

	// Backfill: no position block.
	out, _ = e.mustRun(0, historyArgs("--range", "2001-01-01", "--format", "json")...)
	if doc := decodeHistoryDoc(t, out); doc.Mode != "backfill" || doc.Position != nil {
		t.Errorf("backfill doc = %+v", doc)
	}
}

// URLs and titles reach stdout only; nothing on stderr may carry them.
func TestCaptureHistory_StderrNeverCarriesURLs(t *testing.T) {
	now := hNow()
	a := hVisit("https://example.com/a", now.Add(-3*time.Hour))
	b := hVisit("https://example.org/b", now.Add(-2*time.Hour))
	crm := hVisit("https://crm.example.net/deal/42", now.Add(-time.Hour))
	e := newHistoryEnv(t, crmDeny, a, b, crm)
	e.fail[b.URL] = true

	for _, args := range [][]string{
		historyArgs("-V", "--dry-run"),
		historyArgs("-V"),
		historyArgs("-V", "--since", "1d"),
	} {
		_, errOut, _ := e.run(args...)
		for _, v := range []chromiumtest.Visit{a, b, crm} {
			for _, secret := range []string{v.URL, v.Title, "crm.example.net/deal"} {
				if strings.Contains(errOut, secret) {
					t.Errorf("%v: stderr leaks %q:\n%s", args, secret, errOut)
				}
			}
		}
	}
}
