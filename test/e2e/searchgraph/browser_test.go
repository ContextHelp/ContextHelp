//go:build e2e && unix

package searchgraph_test

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ttyCommand wraps argv so it runs with stdout and stderr on a
// pseudo-terminal, via script(1). It skips when script is missing or
// does not provide a terminal here.
func (e *env) ttyCommand(ctx context.Context, t *testing.T, argv ...string) *exec.Cmd {
	t.Helper()
	wrap := func(argv ...string) *exec.Cmd {
		if runtime.GOOS == "darwin" || strings.HasSuffix(runtime.GOOS, "bsd") {
			return e.commandOf(ctx, "script", append([]string{"-q", "/dev/null"}, argv...)...)
		}
		quoted := make([]string, len(argv))
		for i, a := range argv {
			quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		return e.commandOf(ctx, "script", "-q", "-e", "-c", strings.Join(quoted, " "), "/dev/null")
	}
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script(1) not found: cannot give the viewer a terminal")
	}
	probe := wrap("/bin/sh", "-c", "test -t 1 && test -t 2")
	probe.Stdin = nil
	if err := probe.Run(); err != nil {
		t.Skipf("script(1) does not provide a terminal here: %v", err)
	}
	return wrap(argv...)
}

// At a terminal the viewer opens the browser on its URL, unless
// --no-browser says not to.
func TestViewer_BrowserOnlyAtTerminal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra []string
		opens bool
	}{
		{"terminal", nil, true},
		{"terminal with --no-browser", []string{"--no-browser"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
			defer cancel()
			argv := append([]string{suite.bin, "find", "deployment", "--graph", "--graph-idle-timeout", "1s"}, tc.extra...)
			cmd := e.ttyCommand(ctx, t, argv...)
			out, err := cmd.CombinedOutput()
			if code := exitCode(err); code != 0 {
				t.Fatalf("exit %d, want 0 after the idle timeout\n%s", code, out)
			}
			m := regexp.MustCompile(`Search graph viewer: (http://\S+/)`).FindSubmatch(out)
			if m == nil {
				t.Fatalf("no viewer URL in terminal output:\n%s", out)
			}
			launches := e.browserLaunches()
			switch {
			case tc.opens && (len(launches) != 1 || launches[0] != string(m[1])):
				t.Errorf("browser launches = %v, want exactly the viewer URL %s", launches, m[1])
			case !tc.opens && len(launches) != 0:
				t.Errorf("browser launched despite --no-browser: %v", launches)
			}
		})
	}
}

// chromePath finds a Chrome or Chromium for the headless smoke tests.
// CTXT_E2E_CHROME names one explicitly, or "off" disables them.
func chromePath(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("headless browser smoke: -short")
	}
	switch v := os.Getenv("CTXT_E2E_CHROME"); v {
	case "off", "0", "false":
		t.Skip("headless browser smoke disabled by CTXT_E2E_CHROME")
	case "":
	default:
		return v
	}
	for _, p := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Skip("no Chrome or Chromium found; set CTXT_E2E_CHROME to one")
	return ""
}

// chromeDeadline bounds one headless page render; chromeAttempts is how
// many renders a test tries before failing.
const (
	chromeDeadline = 20 * time.Second
	chromeAttempts = 3
)

// renderDOM renders target in headless Chrome and returns the DOM once
// the viewer script has picked a renderer. A served page fetches
// graph.json after its script loads and Chrome may dump the DOM on the
// load event before that fetch resolves, and a cold Chrome start can
// stall; either way the render is retried. A viewer that never renders
// fails every attempt.
func renderDOM(t *testing.T, e *env, chrome, target string) string {
	t.Helper()
	var dom string
	var err error
	for range chromeAttempts {
		dom, err = dumpDOM(e, chrome, target)
		if err == nil && bodyRenderer.MatchString(dom) {
			return dom
		}
	}
	if err != nil {
		t.Fatalf("headless chrome, %d attempts: %v", chromeAttempts, err)
	}
	return dom
}

// dumpDOM runs one headless Chrome --dump-dom of target. Chrome writes
// the DOM, then may keep running while the 3D view animates, so the
// process group is killed once the document is complete rather than
// waited for.
func dumpDOM(e *env, chrome, target string) (string, error) {
	profile, err := os.MkdirTemp(e.dir, "chrome-")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), chromeDeadline)
	defer cancel()
	argv := []string{
		"--headless=new", "--no-first-run", "--no-default-browser-check",
		"--disable-extensions", "--disable-background-networking", "--disable-component-update",
		"--disable-sync", "--disable-default-apps", "--mute-audio",
		// Never touch the OS credential store: no macOS keychain prompt
		// ("Chrome Safe Storage"), no Linux keyring.
		"--use-mock-keychain", "--password-store=basic",
		"--user-data-dir=" + profile,
		"--timeout=10000", "--dump-dom", target,
	}
	// Chrome refuses its sandbox as root, and Linux CI runners may deny
	// the unprivileged user namespaces it needs. The page is this test's
	// own loopback or file output, rendered in a throwaway profile.
	if os.Geteuid() == 0 || (runtime.GOOS == "linux" && os.Getenv("CI") != "") {
		argv = append([]string{"--no-sandbox"}, argv...)
	}
	cmd := e.commandOf(ctx, chrome, argv...)
	var stderr tailBuffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start chrome: %w", err)
	}
	defer func() {
		_ = killGroup(cmd)
		_ = cmd.Wait()
	}()
	var dom strings.Builder
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		dom.WriteString(sc.Text() + "\n")
		if strings.Contains(sc.Text(), "</html>") {
			return dom.String(), nil
		}
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("no complete DOM: %w; chrome stderr tail:\n%s", ctx.Err(), stderr.String())
	}
	return "", fmt.Errorf("no complete DOM: chrome exited; stderr tail:\n%s", stderr.String())
}

// tailBuffer keeps the last 4 KiB written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if n := len(b.buf) - 4096; n > 0 {
		b.buf = b.buf[n:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

var (
	bodyRenderer = regexp.MustCompile(`<body[^>]*\bdata-renderer="([23]d)"`)
	queryText    = regexp.MustCompile(`<q id="query">([^<]*)</q>`)
	errorHidden  = regexp.MustCompile(`<p id="error"[^>]*\bhidden\b[^>]*></p>`)
)

// assertRendered checks the viewer script ran to completion: a renderer
// is chosen, the query is shown and the error banner stays hidden.
func assertRendered(t *testing.T, dom, query string) {
	t.Helper()
	if m := bodyRenderer.FindStringSubmatch(dom); m == nil {
		t.Errorf("body has no data-renderer: the viewer did not render\n%.2000s", dom)
	}
	if m := queryText.FindStringSubmatch(dom); len(m) < 2 || m[1] != query {
		t.Errorf("query element = %v, want %q", m, query)
	}
	if !errorHidden.MatchString(dom) {
		i := strings.Index(dom, `<p id="error"`)
		t.Errorf("error banner shown: %.300s", dom[max(i, 0):])
	}
}

func TestHeadless_ServedViewer(t *testing.T) {
	chrome := chromePath(t)
	e := newEnv(t)
	v := e.startViewer(args(stageSplitArgs, "--graph-idle-timeout", "2m")...)
	dom := renderDOM(t, e, chrome, v.url)
	assertRendered(t, dom, "deployment")
	if err := v.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if code := v.wait(15 * time.Second); code != 0 {
		t.Errorf("viewer exit %d after SIGINT, want 0", code)
	}
}

func TestHeadless_StandaloneHTML(t *testing.T) {
	chrome := chromePath(t)
	e := newEnv(t)
	path := filepath.Join(t.TempDir(), "graph.html")
	e.mustRun(args(stageSplitArgs, "-o", path)...)
	assertRendered(t, renderDOM(t, e, chrome, "file://"+path), "deployment")
}
