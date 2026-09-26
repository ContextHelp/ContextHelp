//go:build e2e && unix

package searchgraph_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// viewerStartTimeout bounds the wait for the viewer URL on stderr.
const viewerStartTimeout = 30 * time.Second

var viewerURLLine = regexp.MustCompile(`^Search graph viewer: (http://\S+/)$`)

// viewer is a running `find --graph` viewer process.
type viewer struct {
	t      *testing.T
	cmd    *exec.Cmd
	url    string
	stdout bytes.Buffer
	done   chan struct{}
	code   int

	mu     sync.Mutex
	stderr strings.Builder
}

// startViewer runs `ctxt <args>` and waits for the viewer URL on stderr.
// The process is reaped at cleanup whatever the test did.
func (e *env) startViewer(argv ...string) *viewer {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	v := &viewer{t: e.t, done: make(chan struct{})}
	v.cmd = e.command(ctx, argv...)
	v.cmd.Stdout = &v.stdout
	errPipe, err := v.cmd.StderrPipe()
	if err != nil {
		e.t.Fatal(err)
	}
	if err := v.cmd.Start(); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() {
		select {
		case <-v.done:
		default:
			_ = killGroup(v.cmd)
			<-v.done
		}
		cancel()
	})

	urls := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(errPipe)
		for sc.Scan() {
			line := sc.Text()
			v.mu.Lock()
			v.stderr.WriteString(line + "\n")
			v.mu.Unlock()
			if m := viewerURLLine.FindStringSubmatch(line); m != nil {
				urls <- m[1]
			}
		}
		_, _ = io.Copy(io.Discard, errPipe)
		v.code = exitCode(v.cmd.Wait())
		close(v.done)
	}()

	select {
	case v.url = <-urls:
	case <-v.done:
		e.t.Fatalf("viewer exited %d before printing its URL\nstderr:\n%s", v.code, v.stderrText())
	case <-time.After(viewerStartTimeout):
		e.t.Fatalf("no viewer URL on stderr after %s\nstderr:\n%s", viewerStartTimeout, v.stderrText())
	}
	return v
}

func (v *viewer) stderrText() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.stderr.String()
}

// wait returns the exit code once the process exits within d.
func (v *viewer) wait(d time.Duration) int {
	v.t.Helper()
	select {
	case <-v.done:
		return v.code
	case <-time.After(d):
		v.t.Fatalf("viewer still running after %s\nstderr:\n%s", d, v.stderrText())
		return -1
	}
}

func (v *viewer) running() bool {
	select {
	case <-v.done:
		return false
	default:
		return true
	}
}

// get fetches a path relative to the viewer URL.
func (v *viewer) get(t *testing.T, rawURL string, host string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

// The viewer serves the page and the document behind its token, nothing
// else, and exits 0 on SIGINT or SIGTERM.
func TestViewer_ServesAndStopsOnSignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			e := newEnv(t)
			want := normalize(t, []byte(e.mustRun(args(stageSplitArgs, "--format", "json")...).stdout))

			// No --no-browser: stdout and stderr are pipes, not a terminal,
			// so the viewer must not try to open a browser.
			v := e.startViewer(args(stageSplitArgs, "--graph-idle-timeout", "1m")...)
			assertViewerRoutes(t, v, want)

			if err := v.cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			if code := v.wait(15 * time.Second); code != 0 {
				t.Errorf("exit %d after %s, want 0\nstderr:\n%s", code, sig, v.stderrText())
			}
			if !strings.Contains(v.stderrText(), "Search graph viewer stopped (interrupted).") {
				t.Errorf("stderr lacks the interrupted stop line:\n%s", v.stderrText())
			}
			if v.stdout.Len() != 0 {
				t.Errorf("viewer wrote to stdout:\n%s", v.stdout.String())
			}
			if l := e.browserLaunches(); len(l) != 0 {
				t.Errorf("browser launched without a terminal: %v", l)
			}
		})
	}
}

// assertViewerRoutes checks what the viewer serves: the page and the
// document under a loopback URL with a 256-bit token path, and 404 for
// anything outside it.
func assertViewerRoutes(t *testing.T, v *viewer, want jgf) {
	t.Helper()
	u, err := url.Parse(v.url)
	if err != nil {
		t.Fatal(err)
	}
	if u.Hostname() != "127.0.0.1" || u.Port() == "" {
		t.Errorf("viewer URL %s: want 127.0.0.1 with a port", v.url)
	}
	token := strings.Trim(u.Path, "/")
	if len(token) < 43 || strings.Contains(token, "/") {
		t.Errorf("viewer URL path %q: want one 256-bit token segment", u.Path)
	}

	resp, page := v.get(t, v.url, "")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("GET page: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !bytes.Contains(page, []byte("<title>ctxt search graph</title>")) {
		t.Error("page is not the search graph viewer")
	}
	for h, wantV := range map[string]string{"Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY"} {
		if got := resp.Header.Get(h); got != wantV {
			t.Errorf("%s = %q, want %q", h, got, wantV)
		}
	}

	resp, doc := v.get(t, v.url+"graph.json", "")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("GET graph.json: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	assertSameGraph(t, want, normalize(t, doc))

	base := u.Scheme + "://" + u.Host
	for _, miss := range []struct{ name, url, host string }{
		{"wrong token", base + "/" + strings.Repeat("A", len(token)) + "/", ""},
		{"wrong token, document", base + "/" + strings.Repeat("A", len(token)) + "/graph.json", ""},
		{"no token", base + "/", ""},
		{"unknown file", v.url + "secret.txt", ""},
		{"foreign Host header", v.url, "attacker.example:" + u.Port()},
	} {
		if resp, _ := v.get(t, miss.url, miss.host); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", miss.name, resp.StatusCode)
		}
	}
	if resp, _ := v.get(t, base+"/"+token, ""); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/"+token+"/" {
		t.Errorf("token without slash: %d -> %q, want 302 to the token dir", resp.StatusCode, resp.Header.Get("Location"))
	}
}

// With no request for the idle timeout the viewer stops by itself, exit
// 0; requests keep it alive. An empty result still gets a viewer.
func TestViewer_IdleTimeout(t *testing.T) {
	e := newEnv(t)
	const idle = 1500 * time.Millisecond
	v := e.startViewer("find", "zzqqxx", "--graph", "--no-browser", "--graph-idle-timeout", idle.String())

	_, doc := v.get(t, v.url+"graph.json", "")
	if n := normalize(t, doc); len(nodesOf(n)) != 1 {
		t.Errorf("empty result: served graph has %d nodes, want the query node alone", len(nodesOf(n)))
	}
	// Keep it busy for twice the idle timeout.
	for end := time.Now().Add(2 * idle); time.Now().Before(end); time.Sleep(idle / 4) {
		if !v.running() {
			t.Fatalf("viewer stopped while serving requests\nstderr:\n%s", v.stderrText())
		}
		v.get(t, v.url, "")
	}
	if code := v.wait(idle + 10*time.Second); code != 0 {
		t.Errorf("exit %d after idling, want 0\nstderr:\n%s", code, v.stderrText())
	}
	if !strings.Contains(v.stderrText(), "Search graph viewer stopped (idle for 1.5s).") {
		t.Errorf("stderr lacks the idle stop line:\n%s", v.stderrText())
	}
}
