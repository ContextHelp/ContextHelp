//go:build e2e && unix

package searchgraph_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/ideacrafterslabs/ctxt/internal/browser/launch"
)

// These smoke tests serve the viewer from a real, private `dpkms serve`
// over a copy of the fixture corpus: the page, its assets and the graph
// document all come from the daemon, under its own headers.

// cookieBridgePort is the port dpkms prefers for its cookie bridge. The
// tests hold it while a daemon starts, so a test daemon never takes it
// from a developer's own.
const cookieBridgePort = 9377

var dpkmsBin = sync.OnceValues(func() (string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	bin := filepath.Join(suite.work, "dpkms")
	build := exec.Command("go", "build", "-tags", "fts5", "-buildvcs=false", "-o", bin, "./cmd/dpkms")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=1")
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build ./cmd/dpkms: %w\n%s", err, out)
	}
	return bin, nil
})

// daemon is a running `dpkms serve`.
type daemon struct {
	url string // http://127.0.0.1:<port>

	mu  sync.Mutex
	out bytes.Buffer
}

func (d *daemon) output() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.out.String()
}

var httpListenLine = regexp.MustCompile(`HTTP server listening on (127\.0\.0\.1:\d+)`)

// startDpkms serves e's database from a private dpkms instance (no
// inbound auth, loopback only) on free high ports, and stops it, with
// anything it spawned, when the test ends.
func (e *env) startDpkms() *daemon {
	e.t.Helper()
	bin, err := dpkmsBin()
	if err != nil {
		e.t.Fatal(err)
	}
	if ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cookieBridgePort))); err == nil {
		defer ln.Close() // held until the daemon has bound its own ports
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	cmd := e.commandOf(ctx, bin, "--config", e.cfg, "serve",
		"--name", "searchgraph-e2e", "--workers", "1",
		"--port", freePort(e.t), "--grpc-port", freePort(e.t))
	cmd.Env = append(cmd.Env, "BUS_TOKEN=searchgraph-e2e")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		e.t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		e.t.Fatal(err)
	}
	d := &daemon{}
	done := make(chan struct{})
	addrs := make(chan string, 1)
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pipe)
		for sc.Scan() {
			d.mu.Lock()
			d.out.WriteString(sc.Text() + "\n")
			d.mu.Unlock()
			if m := httpListenLine.FindStringSubmatch(sc.Text()); m != nil {
				select {
				case addrs <- m[1]:
				default:
				}
			}
		}
		_, _ = io.Copy(io.Discard, pipe)
		_ = cmd.Wait()
	}()
	e.t.Cleanup(func() {
		_ = killGroup(cmd)
		<-done
		cancel()
	})

	select {
	case addr := <-addrs:
		d.url = "http://" + addr
	case <-done:
		e.t.Fatalf("dpkms exited before listening\n%s", d.output())
	case <-time.After(time.Minute):
		e.t.Fatalf("dpkms not listening after 1m\n%s", d.output())
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		res, err := http.Get(d.url + "/health")
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return d
			}
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("dpkms /health not OK after 30s: %v\n%s", err, d.output())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// freePort returns a loopback port nothing listens on: reserved, then
// released.
func freePort(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(closedURL())
	if err != nil {
		t.Fatal(err)
	}
	return u.Port()
}

// cspProbeJS lists CSP violations in body[data-csp] as
// "<directive> <blocked URL>", joined by " | ", and fetches the given URL,
// which the policy must refuse.
const cspProbeJS = `(function () {
  var seen = [];
  document.addEventListener("securitypolicyviolation", function (e) {
    seen.push(e.effectiveDirective + " " + e.blockedURI);
    document.body.dataset.csp = seen.join(" | ");
  });
  fetch(%q).catch(function () {});
})();`

// dumpViewer renders target in headless Chrome once the page has
// settled. Chrome dumps on the load event, which does not wait for the
// page's graph fetch; a virtual time budget holds the dump until pending
// requests resolve, as renderDOM does. The daemon serves the graph from
// a real search, so its budget is longer.
func dumpViewer(t *testing.T, e *env, target string) string {
	t.Helper()
	dom, err := launch.DumpDOM(context.Background(), target, launch.Options{
		Chrome:            chromePath(t),
		Timeout:           chromeDeadline,
		Attempts:          chromeAttempts,
		VirtualTimeBudget: 5 * chromeSettle,
		Env:               e.environ(),
		Dir:               e.dir,
		TempDir:           e.dir,
	})
	if err != nil {
		t.Fatalf("headless chrome, %d attempts: %v", chromeAttempts, err)
	}
	return dom
}

// The daemon's page renders the search its URL names: the viewer
// fetches /api/v1/search/graph with the page's query under the
// daemon's own security headers and the page's Content-Security-Policy.
// The slashless URL redirects there with the query kept.
func TestHeadless_DpkmsServedViewer(t *testing.T) {
	chromePath(t)
	e := newEnv(t)
	d := e.startDpkms()

	for _, target := range []string{
		d.url + "/ui/searchgraph/?q=deployment",
		d.url + "/ui/searchgraph?q=deployment",
	} {
		t.Run(strings.TrimPrefix(target, d.url), func(t *testing.T) {
			dom := dumpViewer(t, e, target)
			assertRendered(t, dom, "deployment")
		})
	}
}

// A click on an object links to the web UI's object page on the
// daemon's origin, and that page loads. The page cannot be scripted
// from outside, so a same-origin proxy in front of the daemon adds the
// click probe to the viewer page and passes everything else through.
// A second probe records every Content-Security-Policy violation and
// makes one cross-origin fetch as a control: the only violation must be
// that fetch, so the policy the page runs under (the daemon's header
// plus the page's meta element) allows the viewer's own loads and its
// same-origin graph fetch, and still refuses anything off-origin.
func TestHeadless_DpkmsViewerObjectLink(t *testing.T) {
	chromePath(t)
	e := newEnv(t)
	d := e.startDpkms()
	target, err := url.Parse(d.url)
	if err != nil {
		t.Fatal(err)
	}
	const tag = `<script src="viewer.js"></script>`
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(target) },
		ModifyResponse: func(res *http.Response) error {
			if res.Request.URL.Path != "/ui/searchgraph/" || res.StatusCode != http.StatusOK {
				return nil
			}
			body, err := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if err != nil {
				return err
			}
			if bytes.Count(body, []byte(tag)) != 1 {
				return fmt.Errorf("viewer page: script tag not found")
			}
			body = bytes.Replace(body, []byte(tag), []byte(tag+`<script src="csp-probe.js"></script><script src="probe.js"></script>`), 1)
			res.Body = io.NopCloser(bytes.NewReader(body))
			res.ContentLength = int64(len(body))
			res.Header.Set("Content-Length", strconv.Itoa(len(body)))
			return nil
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/searchgraph/probe.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = io.WriteString(w, probeJS)
	})
	offOrigin := closedURL() + "/csp-control"
	mux.HandleFunc("GET /ui/searchgraph/csp-probe.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = fmt.Fprintf(w, cspProbeJS, offOrigin)
	})
	mux.Handle("/", proxy)
	front := httptest.NewServer(mux)
	t.Cleanup(front.Close)

	dom := dumpViewer(t, e, front.URL+"/ui/searchgraph/?q=deployment")
	assertRendered(t, dom, "deployment")
	if !strings.Contains(dom, `data-probe="clicked"`) {
		t.Fatalf("probe never clicked an object: %s", regexp.MustCompile(`<body[^>]*>`).FindString(dom))
	}
	root := parseDOM(t, dom)

	if got, want := attrOf(byTag(t, root, "body"), "data-csp"), "connect-src "+offOrigin; got != want {
		t.Errorf("CSP violations = %q, want only the off-origin control %q", got, want)
	}

	id := text(byID(t, root, "details-id"))
	ids := make([]string, 0, len(suite.corpus.ids))
	for _, v := range suite.corpus.ids {
		ids = append(ids, v)
	}
	if !slices.Contains(ids, id) {
		t.Fatalf("clicked object id %q is not a fixture object", id)
	}
	if hasAttr(byID(t, root, "details-link-row"), "hidden") {
		t.Error("link row hidden: the page is not in link mode")
	}
	href := attrOf(byID(t, root, "details-link"), "href")
	if want := front.URL + "/ui/objects/" + id; href != want {
		t.Fatalf("object link = %q, want %q", href, want)
	}

	// Following the link lands on the web UI, straight from the daemon.
	res, err := http.Get(d.url + strings.TrimPrefix(href, front.URL))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `<div id="root">`) {
		t.Errorf("GET %s: %d, want the web UI shell\n%.300s", href, res.StatusCode, body)
	}
}

func parseDOM(t *testing.T, dom string) *html.Node {
	t.Helper()
	root, err := html.Parse(strings.NewReader(dom))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// byTag returns the first element named tag, or fails.
func byTag(t *testing.T, root *html.Node, tag string) *html.Node {
	t.Helper()
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			found = n
		}
		for c := n.FirstChild; c != nil && found == nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if found == nil {
		t.Fatalf("no <%s> element", tag)
	}
	return found
}
