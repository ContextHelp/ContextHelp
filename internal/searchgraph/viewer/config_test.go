package viewer

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var hostedConfig = Config{
	DataURL:      "/api/v1/search/graph",
	ForwardQuery: true,
	ObjectAction: ObjectLink,
	ObjectHref:   "/ui/objects/{id}",
}

// The served page carries no config, so the page runs on the defaults the
// JS side declares; DefaultConfig must describe exactly those.
func TestDefaultConfigMatchesServedPage(t *testing.T) {
	page, err := fs.ReadFile(Assets(), IndexFile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(page, []byte("viewer-config")) {
		t.Fatalf("%s carries a config element; the CLI must run on the page defaults", IndexFile)
	}
	want := Config{DataURL: "graph.json", ForwardQuery: false, ObjectAction: "copy-cli"}
	if got := DefaultConfig(); got != want {
		t.Fatalf("DefaultConfig() = %+v, want %+v", got, want)
	}
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig invalid: %v", err)
	}
	// The script's built-in defaults, as minified by the bundler.
	script, _ := fs.ReadFile(Assets(), ScriptFile)
	for _, frag := range []string{`dataUrl:"graph.json"`, `forwardQuery:!1`, `objectAction:"copy-cli"`} {
		if !bytes.Contains(script, []byte(frag)) {
			t.Errorf("%s: default %s not found", ScriptFile, frag)
		}
	}
}

func TestConfigValidate(t *testing.T) {
	valid := []Config{
		DefaultConfig(),
		hostedConfig,
		{DataURL: "graph.json", ObjectAction: ObjectLink, ObjectHref: "../objects/{id}"},
		{DataURL: "/g?format=jgf", ObjectAction: ObjectLink, ObjectHref: "/ui/objects?id={id}"},
	}
	for _, c := range valid {
		if err := c.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", c, err)
		}
	}
	invalid := map[string]Config{
		"empty data":            {ObjectAction: ObjectCopyCLI},
		"absolute data":         {DataURL: "https://evil.example/g", ObjectAction: ObjectCopyCLI},
		"protocol-relative":     {DataURL: "//evil.example/g", ObjectAction: ObjectCopyCLI},
		"backslash authority":   {DataURL: `/\evil.example/g`, ObjectAction: ObjectCopyCLI},
		"double backslash":      {DataURL: `\\evil.example/g`, ObjectAction: ObjectCopyCLI},
		"newline authority":     {DataURL: "/\n/evil.example/g", ObjectAction: ObjectCopyCLI},
		"tab authority":         {DataURL: "/\t/evil.example/g", ObjectAction: ObjectCopyCLI},
		"leading space":         {DataURL: " //evil.example/g", ObjectAction: ObjectCopyCLI},
		"javascript data":       {DataURL: "javascript:alert(1)", ObjectAction: ObjectCopyCLI},
		"data scheme":           {DataURL: "data:application/json,{}", ObjectAction: ObjectCopyCLI},
		"userinfo":              {DataURL: "http://u@127.0.0.1/g", ObjectAction: ObjectCopyCLI},
		"empty action":          {DataURL: "graph.json"},
		"unknown action":        {DataURL: "graph.json", ObjectAction: "exec"},
		"link without template": {DataURL: "graph.json", ObjectAction: ObjectLink},
		"link without id":       {DataURL: "graph.json", ObjectAction: ObjectLink, ObjectHref: "/ui/objects/"},
		"absolute link":         {DataURL: "graph.json", ObjectAction: ObjectLink, ObjectHref: "https://evil.example/{id}"},
		"relative-proto link":   {DataURL: "graph.json", ObjectAction: ObjectLink, ObjectHref: "//evil.example/{id}"},
		"javascript link":       {DataURL: "graph.json", ObjectAction: ObjectLink, ObjectHref: "javascript:alert('{id}')"},
		"scheme from id":        {DataURL: "graph.json", ObjectAction: ObjectLink, ObjectHref: "{id}:x"},
	}
	for name, c := range invalid {
		if err := c.Validate(); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: Validate(%+v) = %v, want ErrInvalidConfig", name, c, err)
		}
		if _, err := Index(c); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: Index = %v, want ErrInvalidConfig", name, err)
		}
		if _, err := AssetsWith(c); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: AssetsWith = %v, want ErrInvalidConfig", name, err)
		}
	}
}

func TestIndexInjectsConfig(t *testing.T) {
	page, err := Index(hostedConfig)
	if err != nil {
		t.Fatal(err)
	}
	raw, n := configElement(t, page)
	if n != 1 {
		t.Fatalf("want one viewer-config element, got %d", n)
	}
	var got Config
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("config element is not JSON: %v", err)
	}
	if got != hostedConfig {
		t.Fatalf("config round-trip = %+v, want %+v", got, hostedConfig)
	}
	base, _ := fs.ReadFile(Assets(), IndexFile)
	cfgAt, scriptAt := bytes.Index(page, []byte(`id="viewer-config"`)), bytes.Index(page, []byte(scriptSrcTag))
	if cfgAt < 0 || scriptAt < cfgAt {
		t.Fatalf("config element must precede the viewer script (config at %d, script at %d)", cfgAt, scriptAt)
	}
	// Apart from the element, the page is untouched: same CSP, same links.
	stripped := bytes.Replace(page, []byte(`<script type="application/json" id="viewer-config">`+raw+"</script>\n"), nil, 1)
	if !bytes.Equal(stripped, base) {
		t.Fatalf("Index changed more than the config element")
	}
	if !bytes.Contains(page, []byte(cspServed)) {
		t.Fatalf("Index changed the Content Security Policy")
	}
}

// Values a host builds from its own settings, e.g. a path prefix, cannot
// close the element or smuggle markup.
func TestIndexEscapesConfig(t *testing.T) {
	cfg := Config{
		DataURL:      "/g?x=</script><script>alert(1)</script>&y=<!--",
		ObjectAction: ObjectLink,
		ObjectHref:   "/o/{id}?z=</SCRIPT/><b>&amp;\u2028",
	}
	page, err := Index(cfg)
	if err != nil {
		t.Fatal(err)
	}
	raw, n := configElement(t, page)
	if n != 1 {
		t.Fatalf("want one viewer-config element, got %d", n)
	}
	var got Config
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("config element is not JSON: %v\n%s", err, raw)
	}
	if got != cfg {
		t.Fatalf("round-trip = %+v, want %+v", got, cfg)
	}
	for _, bad := range []string{"<", ">", "&"} {
		if strings.Contains(raw, bad) {
			t.Errorf("config element holds a raw %q: %s", bad, raw)
		}
	}
	if p := parsePage(t, page); len(p.scripts) != 2 {
		t.Fatalf("want 2 script elements (config + viewer), got %d", len(p.scripts))
	}
}

func TestAssetsWith(t *testing.T) {
	fsys, err := AssetsWith(hostedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(fsys, IndexFile, ScriptFile, StyleFile, NoticesFile); err != nil {
		t.Fatal(err)
	}
	want, _ := Index(hostedConfig)
	got, err := fs.ReadFile(fsys, IndexFile)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("AssetsWith index differs from Index (err %v)", err)
	}
	for _, name := range []string{ScriptFile, StyleFile, NoticesFile} {
		a, _ := fs.ReadFile(Assets(), name)
		b, _ := fs.ReadFile(fsys, name)
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs from Assets()", name)
		}
	}
	if _, err := fs.Stat(fsys, DataFile); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s must be served by the host (stat err=%v)", DataFile, err)
	}

	// The shape a host mounts it in.
	srv := httptest.NewServer(http.StripPrefix("/ui/searchgraph", http.FileServerFS(fsys)))
	defer srv.Close()
	for path, frag := range map[string]string{
		"/ui/searchgraph/":          `id="viewer-config"`,
		"/ui/searchgraph/viewer.js": `objectAction:"copy-cli"`,
	} {
		res, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(frag)) {
			t.Errorf("GET %s: %d, want 200 with %q", path, res.StatusCode, frag)
		}
	}
}

// configElement returns the text of script#viewer-config and how many
// such elements the page has.
func configElement(t *testing.T, page []byte) (string, int) {
	t.Helper()
	root, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var n int
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.DataAtom == atom.Script {
			var id, typ string
			for _, a := range node.Attr {
				switch a.Key {
				case "id":
					id = a.Val
				case "type":
					typ = a.Val
				}
			}
			if id == "viewer-config" {
				n++
				if typ != "application/json" {
					t.Errorf("viewer-config type = %q, want application/json", typ)
				}
				text = textOf(node)
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return text, n
}
