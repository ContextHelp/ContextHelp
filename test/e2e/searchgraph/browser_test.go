//go:build e2e && unix

package searchgraph_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/launch"
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

// chromePath finds a Chrome or Chromium for the headless smoke tests
// through launch.FindChrome: CTXT_CHROME names one explicitly, or "off"
// disables them.
func chromePath(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("headless browser smoke: -short")
	}
	p, err := launch.FindChrome()
	switch {
	case errors.Is(err, launch.ErrDisabled), errors.Is(err, launch.ErrNotFound):
		t.Skip(err)
	case err != nil:
		t.Fatal(err)
	}
	return p
}

// chromeDeadline bounds one headless page render; chromeAttempts is how
// many renders a test tries when Chrome produces no document at all, as
// a cold start can stall. chromeSettle is the virtual time the page gets
// once its network is idle.
const (
	chromeDeadline = 20 * time.Second
	chromeAttempts = 3
	chromeSettle   = time.Second
)

// renderDOM renders target in headless Chrome and returns its DOM once
// the page has settled. The served viewer fetches graph.json after its
// script loads, and the load event, where a plain dump happens, does not
// wait for that fetch; a virtual time budget does, since virtual time
// stands still while a fetch is pending. The viewer picks its renderer
// synchronously once the document arrives, so the dump is the rendered
// page, or a viewer that did not render, for assertRendered to report.
func renderDOM(t *testing.T, e *env, chrome, target string) string {
	t.Helper()
	dom, err := launch.DumpDOM(context.Background(), target, launch.Options{
		Chrome:            chrome,
		Timeout:           chromeDeadline,
		Attempts:          chromeAttempts,
		VirtualTimeBudget: chromeSettle,
		Env:               e.environ(),
		Dir:               e.dir,
		TempDir:           e.dir,
	})
	if err != nil {
		t.Fatalf("headless chrome, %d attempts: %v", chromeAttempts, err)
	}
	return dom
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

// A graph.json that arrives well after the load event still renders:
// the check waits for the page's own fetch rather than racing it. A
// proxy in front of the viewer holds the document back.
func TestHeadless_ServedViewerLateDocument(t *testing.T) {
	chrome := chromePath(t)
	e := newEnv(t)
	v := e.startViewer(args(stageSplitArgs, "--graph-idle-timeout", "2m")...)
	target, err := url.Parse(v.url)
	if err != nil {
		t.Fatal(err)
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(&url.URL{Scheme: target.Scheme, Host: target.Host})
	}}
	late := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if path.Base(r.URL.Path) == "graph.json" {
			time.Sleep(time.Second)
		}
		proxy.ServeHTTP(w, r)
	}))
	defer late.Close()
	assertRendered(t, renderDOM(t, e, chrome, late.URL+target.Path), "deployment")
}

func TestHeadless_StandaloneHTML(t *testing.T) {
	chrome := chromePath(t)
	e := newEnv(t)
	path := filepath.Join(t.TempDir(), "graph.html")
	e.mustRun(args(stageSplitArgs, "-o", path)...)
	assertRendered(t, renderDOM(t, e, chrome, "file://"+path), "deployment")
}
