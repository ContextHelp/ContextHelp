//go:build e2e && unix

package searchgraph_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"golang.org/x/net/html"

	"github.com/ideacrafterslabs/ctxt/internal/browser/launch"
	viewerpkg "github.com/ideacrafterslabs/ctxt/internal/searchgraph/viewer"
)

// These smoke tests host the embedded viewer the way a server other than
// the CLI would, in-process, and check what the page does with its host
// config: where it fetches the graph and what a click on an object shows.
// A same-origin probe script clicks the first object node once the graph
// renders, then marks the body, so the dumped DOM holds the details panel.

// hostedObjectID needs encoding in a URL and quoting in a shell.
const hostedObjectID = "01J/../x y?z#w"

const probeJS = `(function () {
  function click() {
    var v = window.ctxtSearchGraph;
    if (!v || document.body.dataset.probe) return;
    try {
      var n = v.data.nodes.find(function (n) { return n.kind === "object"; });
      v.graph.onNodeClick()(n, new MouseEvent("click"));
      document.body.dataset.probe = "clicked";
    } catch (err) {
      document.body.dataset.probe = "error: " + err;
    }
  }
  // A mutation callback runs as a microtask, before Chrome can dump the
  // DOM between tasks; a timer might not.
  new MutationObserver(click).observe(document.body, { attributes: true, attributeFilter: ["data-renderer"] });
  click();
})();`

func hostedDoc(t *testing.T) []byte {
	t.Helper()
	doc, err := json.Marshal(map[string]any{"graph": map[string]any{
		"label":    "deploy x",
		"directed": true,
		"metadata": map[string]any{"vocabulary": "ctxt.search-graph/v1", "query": "deploy x"},
		"nodes": map[string]any{
			"query": map[string]any{"label": "deploy x", "metadata": map[string]any{"kind": "query"}},
			"obj:1": map[string]any{"label": "Deploy runbook", "metadata": map[string]any{
				"kind": "object", "object_id": hostedObjectID, "stage": "returned", "rank": 1,
			}},
		},
		"edges": []any{map[string]any{"source": "query", "target": "obj:1", "relation": "matched"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// hostedSite serves assets under prefix, with the probe added to the
// page, and the graph document at dataPath. It records every query string
// dataPath is fetched with; a fetch whose q is not wantQ gets a 400.
type hostedSite struct {
	srv *httptest.Server

	mu      sync.Mutex
	fetches []string
}

func newHostedSite(t *testing.T, assets fs.FS, prefix, dataPath, wantQ string) *hostedSite {
	t.Helper()
	files := map[string][]byte{}
	entries, err := fs.ReadDir(assets, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := fs.ReadFile(assets, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = b
	}
	const tag = `<script src="viewer.js"></script>`
	if bytes.Count(files[viewerpkg.IndexFile], []byte(tag)) != 1 {
		t.Fatalf("%s: viewer script tag not found", viewerpkg.IndexFile)
	}
	files[viewerpkg.IndexFile] = bytes.Replace(files[viewerpkg.IndexFile], []byte(tag),
		[]byte(tag+`<script src="probe.js"></script>`), 1)
	files["probe.js"] = []byte(probeJS)
	doc := hostedDoc(t)

	s := &hostedSite{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+prefix+"{file...}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("file")
		if name == "" {
			name = viewerpkg.IndexFile
		}
		b, ok := files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		ct := map[string]string{".html": "text/html", ".js": "text/javascript", ".css": "text/css"}
		for ext, v := range ct {
			if strings.HasSuffix(name, ext) {
				w.Header().Set("Content-Type", v+"; charset=utf-8")
			}
		}
		_, _ = w.Write(b)
	})
	mux.HandleFunc("GET "+dataPath, func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.fetches = append(s.fetches, r.URL.RawQuery)
		s.mu.Unlock()
		if r.URL.Query().Get("q") != wantQ {
			http.Error(w, "unexpected q", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(doc)
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *hostedSite) fetched() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.fetches...)
}

var errorShown = func(dom string) bool { return strings.Contains(dom, `<p id="error" role="alert">`) }

// renderHosted dumps target once the probe has clicked, or the page
// shows an error.
func renderHosted(t *testing.T, target string) *html.Node {
	t.Helper()
	chrome := chromePath(t)
	e := newEnv(t)
	dom, err := launch.DumpDOM(context.Background(), target, launch.Options{
		Chrome:   chrome,
		Timeout:  chromeDeadline,
		Attempts: chromeAttempts,
		Accept: func(dom string) bool {
			return strings.Contains(dom, `data-probe=`) || errorShown(dom)
		},
		Env:     e.environ(),
		Dir:     e.dir,
		TempDir: e.dir,
	})
	if err != nil && !errors.Is(err, launch.ErrRejected) {
		t.Fatalf("headless chrome, %d attempts: %v", chromeAttempts, err)
	}
	assertRendered(t, dom, "deploy x")
	if !strings.Contains(dom, `data-probe="clicked"`) {
		t.Fatalf("probe never clicked an object: %s\n%s",
			regexp.MustCompile(`<body[^>]*>`).FindString(dom), dom[max(strings.Index(dom, `<section id="details"`), 0):])
	}
	root, err := html.Parse(strings.NewReader(dom))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// byID returns the element with id, or fails.
func byID(t *testing.T, root *html.Node, id string) *html.Node {
	t.Helper()
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && attrOf(n, "id") == id {
			found = n
		}
		for c := n.FirstChild; c != nil && found == nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if found == nil {
		t.Fatalf("no element #%s", id)
	}
	return found
}

func attrOf(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func text(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

// Unconfigured assets, as the CLI serves them: graph.json next to the
// page, fetched without the page's query, and `ctxt show <id>` on click.
func TestHeadless_HostedDefaults(t *testing.T) {
	site := newHostedSite(t, viewerpkg.Assets(), "/tok/", "/tok/"+viewerpkg.DataFile, "")
	root := renderHosted(t, site.srv.URL+"/tok/?q=not-forwarded")

	if got := site.fetched(); len(got) == 0 || got[0] != "" {
		t.Errorf("graph.json fetched with queries %q, want one plain fetch", got)
	}
	if !hasAttr(byID(t, root, "details-link-row"), "hidden") {
		t.Error("default mode shows a link row")
	}
	if hasAttr(byID(t, root, "details-cmd-row"), "hidden") {
		t.Error("default mode hides the command row")
	}
	if got, want := text(byID(t, root, "details-cmd")), `ctxt show '01J/../x y?z#w'`; got != want {
		t.Errorf("command = %q, want %q", got, want)
	}
	if got := text(byID(t, root, "details-id")); got != hostedObjectID {
		t.Errorf("object id = %q, want %q", got, hostedObjectID)
	}
}

// A host config: the data URL carries the page's query, and a click
// links to the host's object page on the same origin.
func TestHeadless_HostedLink(t *testing.T) {
	assets, err := viewerpkg.AssetsWith(viewerpkg.Config{
		DataURL:      "/api/v1/search/graph",
		ForwardQuery: true,
		ObjectAction: viewerpkg.ObjectLink,
		ObjectHref:   "/ui/objects/{id}",
	})
	if err != nil {
		t.Fatal(err)
	}
	site := newHostedSite(t, assets, "/ui/searchgraph/", "/api/v1/search/graph", "deploy x")
	root := renderHosted(t, site.srv.URL+"/ui/searchgraph/?q=deploy%20x&limit=3")

	got := site.fetched()
	if len(got) == 0 {
		t.Fatal("data URL never fetched")
	}
	q, err := url.ParseQuery(got[0])
	if err != nil || q.Get("q") != "deploy x" || q.Get("limit") != "3" || len(q) != 2 {
		t.Errorf("data URL query = %q, want q=deploy x&limit=3", got[0])
	}
	if !hasAttr(byID(t, root, "details-cmd-row"), "hidden") {
		t.Error("link mode shows the CLI command row")
	}
	if hasAttr(byID(t, root, "details-link-row"), "hidden") {
		t.Error("link mode hides the link row")
	}
	href := attrOf(byID(t, root, "details-link"), "href")
	if want := site.srv.URL + "/ui/objects/01J%2F..%2Fx%20y%3Fz%23w"; href != want {
		t.Errorf("object link = %q, want %q", href, want)
	}
	if u, err := url.Parse(href); err != nil || "http://"+u.Host != site.srv.URL {
		t.Errorf("object link %q leaves the page origin %s", href, site.srv.URL)
	}
}
