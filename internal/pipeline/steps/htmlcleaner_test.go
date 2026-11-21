package steps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// boilerplatePage has its content in <main>; the cookie notice sits outside
// it, so it is excluded by the main preference, not by any banner heuristic.
const boilerplatePage = `<!DOCTYPE html>
<html><head><title>  The Real
 Title </title><style>body{color:red}</style><script>var tracking = 1;</script></head>
<body>
<header>Site Header Brand</header>
<nav><a href="/">Home</a> <a href="/about">About Us</a></nav>
<div class="cookie-banner" role="dialog">We use cookies to improve your visit</div>
<aside>Related Sidebar Links</aside>
<main><article><h1>Heading One</h1><p>First paragraph of the article.</p><p>Second paragraph.</p></article></main>
<footer>Copyright Footer Text</footer>
<noscript>Noscript Fallback Text</noscript>
</body></html>`

func runCleaner(t *testing.T, content, ctype string) *storage.KnowledgeObject {
	t.Helper()
	d := &storage.KnowledgeObject{RawContent: content, ContentType: ctype}
	got, err := NewHTMLCleaner().Run(context.Background(), d)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return got
}

func TestHTMLCleaner_StripsChromeOutsideMain(t *testing.T) {
	got := runCleaner(t, boilerplatePage, "text/html")
	for _, bad := range []string{"The Real", "Site Header", "Home", "About Us", "Sidebar", "Copyright", "Noscript", "cookies", "tracking", "color:red"} {
		if strings.Contains(got.RawContent, bad) {
			t.Errorf("body still has %q: %q", bad, got.RawContent)
		}
	}
	for _, want := range []string{"Heading One", "First paragraph of the article.", "Second paragraph."} {
		if !strings.Contains(got.RawContent, want) {
			t.Errorf("body lost %q: %q", want, got.RawContent)
		}
	}
	if got.Metadata["title"] != "The Real Title" {
		t.Errorf("title = %v", got.Metadata["title"])
	}
	if !strings.Contains(got.RawContent, "First paragraph of the article.\n\nSecond paragraph.") {
		t.Errorf("paragraph break lost: %q", got.RawContent)
	}
}

func TestHTMLCleaner_Table(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string
		notWant []string
		exact   string
		hasExac bool
	}{
		{name: "main wins over article",
			in:      `<body><article>ArtOnly</article><main>MainText <article>InnerArt</article></main></body>`,
			want:    []string{"MainText", "InnerArt"},
			notWant: []string{"ArtOnly"}},
		{name: "multiple articles concatenated in order",
			in:      `<body><p>Outside</p><article>One</article><article>Two <article>Nested</article></article></body>`,
			notWant: []string{"Outside"},
			exact:   "One\n\nTwo\n\nNested", hasExac: true},
		{name: "role based removal",
			in:      `<body><div role="banner">B</div><div role="navigation">N</div><div role="contentinfo">C</div><div role="complementary">S</div><p>Keep</p></body>`,
			exact:   "Keep", hasExac: true},
		{name: "role token list case insens",
			in:    `<body><div role="Presentation NAVIGATION">gone</div><p>stay</p></body>`,
			exact: "stay", hasExac: true},
		{name: "role main is main",
			in:    `<body><div>outside</div><div role="main">inside <b>bold</b></div><div>after</div></body>`,
			exact: "inside bold", hasExac: true},
		{name: "entities",
			in:    `<body><p>a&nbsp;b &amp; c &lt;d&gt; it&#39;s &#x41; &quot;q&quot; &copy;</p></body>`,
			exact: `a b & c <d> it's A "q" ©`, hasExac: true},
		{name: "inline tags do not glue words",
			in:    `<body><p>one<b>two</b> <i>three</i><span>four</span></p><div>five</div><div>six</div>x<br>y</body>`,
			exact: "onetwo threefour\n\nfive\nsix\nx\ny", hasExac: true},
		{name: "block boundaries",
			in:    `<body><ul><li>a</li><li>b</li></ul><table><tr><td>c</td></tr><tr><td>d</td></tr></table></body>`,
			exact: "a\nb\n\nc\nd", hasExac: true},
		{name: "table cells separated",
			in:    `<body><table><tr><td>a</td><td>b</td><td>c</td></tr><tr><th>d</th><th>e</th></tr></table></body>`,
			exact: "a b c\nd e", hasExac: true},
		{name: "malformed html",
			in:   `<html><body><div><p>unclosed <b>bold <i>both</p><span>tail`,
			want: []string{"unclosed", "bold", "both", "tail"}},
		{name: "garbage tags",
			in:   `<!doctype html><<>> <p <p>text</div></span>`,
			want: []string{"text"}},
		{name: "no main page keeps body minus chrome",
			in:      `<html><head><title>T</title></head><body><nav>menu</nav><h2>Head</h2><p>Body text</p><footer>foot</footer></body></html>`,
			exact:   "Head\n\nBody text", hasExac: true,
			notWant: []string{"T\n"}},
		{name: "no class heuristics: cookie banner outside landmarks survives",
			in:    `<body><div class="cookie-banner">We use cookies. Accept all</div><p>Real text</p></body>`,
			want:  []string{"We use cookies. Accept all", "Real text"},
			exact: "", hasExac: false},
		{name: "empty main falls back to body",
			in:    `<body><main>   </main><p>Fallback</p></body>`,
			exact: "Fallback", hasExac: true},
		{name: "controls dropped, form text kept",
			in:    `<body><form>formtext<input type="text" value="v"><button>btn</button><select><option>optA</option></select><textarea>ta</textarea></form><iframe>i</iframe><svg><title>svgt</title><text>s</text></svg><template>t</template><p>ok</p></body>`,
			exact: "formtext\n\nok", hasExac: true},
		{name: "aspnet form wrapping the page",
			in: `<html><head><title>T</title></head><body><form id="aspnetForm" method="post"><input type="hidden" name="__VIEWSTATE" value="x"><div><h1>Heading</h1><p>Real content here</p><input type="submit" value="Go"></div></form></body></html>`,
			exact: "Heading\n\nReal content here", hasExac: true},
		{name: "hidden and aria-hidden dropped",
			in:    `<body><p hidden>h</p><p aria-hidden="true">ah</p><div hidden><p>nested</p></div><p aria-hidden="false">shown</p><p>ok</p></body>`,
			exact: "shown\n\nok", hasExac: true},
		{name: "pre keeps line breaks",
			in:    `<body><pre>line1
line2


line3</pre></body>`,
			exact: "line1\nline2\n\nline3", hasExac: true},
		{name: "header and footer inside article kept",
			in: `<body><nav>menu</nav><header>site head</header><article><header><h1>Title H1</h1><p>By Author</p></header><p>Body</p><aside>Pull quote</aside><footer>Tags: x</footer></article><footer>site foot</footer></body>`,
			want: []string{"Title H1", "By Author", "Body", "Pull quote", "Tags: x"},
			notWant: []string{"menu", "site head", "site foot"}},
		{name: "header and footer inside main kept",
			in:      `<body><header>site head</header><main><header><h1>H</h1></header><p>x</p><footer>main foot</footer></main><footer>site foot</footer></body>`,
			want:    []string{"H", "x", "main foot"},
			notWant: []string{"site head", "site foot"}},
		{name: "section header kept, body-level header dropped",
			in:      `<body><header>site head</header><section><header>Sec head</header><p>text</p></section></body>`,
			want:    []string{"Sec head", "text"},
			notWant: []string{"site head"}},
		{name: "everything in chrome: minimal fallback keeps text",
			in:      `<body><script>bad()</script><header><p>Only content lives here</p></header><footer>and here</footer></body>`,
			want:    []string{"Only content lives here", "and here"},
			notWant: []string{"bad()"}},
		{name: "far shorter than minimal pass falls back when no content root",
			in:      `<body><nav>` + strings.Repeat("section links words ", 80) + `</nav><p>tiny</p></body>`,
			want:    []string{"tiny", "section links words"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := runCleaner(t, tc.in, "text/html").RawContent
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in %q", w, got)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(got, w) {
					t.Errorf("unexpected %q in %q", w, got)
				}
			}
			if tc.hasExac && got != tc.exact {
				t.Errorf("got %q, want %q", got, tc.exact)
			}
		})
	}
}

func TestHTMLCleaner_ShortArticleBesideBigNavKeepsRootOnly(t *testing.T) {
	in := `<body><nav>` + strings.Repeat("section links words ", 80) + `</nav><main><p>tiny</p></main></body>`
	if got := runCleaner(t, in, "text/html").RawContent; got != "tiny" {
		t.Errorf("got %q, want only main text", got)
	}
}

func TestHTMLCleaner_DeepNestingFallsBackToTokenizer(t *testing.T) {
	const depth = 600 // x/net/html rejects element nesting beyond 512
	in := `<html><head><title>Deep &amp; Wide</title><script>bad()</script></head><body>` +
		strings.Repeat("<div>", depth) +
		`<nav>menu</nav><style>.x{}</style>Deep text &amp; more<p>para</p><template>tmpl</template>` +
		strings.Repeat("</div>", depth) + `</body></html>`
	d := &storage.KnowledgeObject{RawContent: in, ContentType: "text/html", Source: "https://example.com/deep"}
	got, err := NewHTMLCleaner().Run(context.Background(), d)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"Deep text & more", "para"} {
		if !strings.Contains(got.RawContent, want) {
			t.Errorf("missing %q in %q", want, got.RawContent)
		}
	}
	for _, bad := range []string{"<div", "</", "bad()", "menu", ".x{}", "tmpl", "Deep &"} {
		if strings.Contains(got.RawContent, bad) {
			t.Errorf("unexpected %q in %q", bad, got.RawContent)
		}
	}
	if got.Metadata["title"] != "Deep & Wide" {
		t.Errorf("title = %v", got.Metadata["title"])
	}
}

func TestHTMLCleaner_EmptyInput(t *testing.T) {
	got := runCleaner(t, "", "text/html")
	if got.RawContent != "" {
		t.Errorf("RawContent = %q", got.RawContent)
	}
	if _, ok := got.Metadata["title"]; ok {
		t.Errorf("title set on empty input")
	}
}

func TestHTMLCleaner_NonHTMLPassthrough(t *testing.T) {
	in := "# markdown <b>kept</b> &amp; raw"
	if got := runCleaner(t, in, "text/plain"); got.RawContent != in || got.Metadata != nil {
		t.Errorf("not passed through: %q %v", got.RawContent, got.Metadata)
	}
}

func TestHTMLCleaner_DoctypeSniffWithoutContentType(t *testing.T) {
	got := runCleaner(t, "<!DOCTYPE html><title>X</title><p>hi</p>", "")
	if got.RawContent != "hi" || got.Metadata["title"] != "X" {
		t.Errorf("got %q %v", got.RawContent, got.Metadata)
	}
}

func TestHTMLCleaner_KeepsExistingTitle(t *testing.T) {
	d := &storage.KnowledgeObject{RawContent: "<title>Page</title><p>x</p>", ContentType: "text/html",
		Metadata: map[string]any{"title": "Operator"}}
	got, _ := NewHTMLCleaner().Run(context.Background(), d)
	if got.Metadata["title"] != "Operator" {
		t.Errorf("title overwritten: %v", got.Metadata["title"])
	}
}

// Real GitHub pages, fetched once and saved as published. Issue and PR bodies
// and comments are server-rendered, so they survive; page chrome does not.
func TestHTMLCleaner_GitHubPages(t *testing.T) {
	tests := []struct {
		file      string
		title     string
		wantInTxt []string
	}{
		{"testdata/github/issue.html", "cmd/gc: optimize switch statements with string literal cases",
			[]string{"Consider the following benchmark", "Started, but waiting for switch code cleanup before mailing to avoid churn."}},
		{"testdata/github/pull.html", "Print ActiveHelp for bash along other completions",
			[]string{"In the bash shell we used to print ActiveHelp messages on every tab-press."}},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			b, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			got := runCleaner(t, string(b), "text/html")
			if title, _ := got.Metadata["title"].(string); !strings.Contains(title, tc.title) {
				t.Errorf("title = %q, want it to contain %q", title, tc.title)
			}
			for _, w := range tc.wantInTxt {
				if !strings.Contains(got.RawContent, w) {
					t.Errorf("body lost %q", w)
				}
			}
			for _, bad := range []string{"Skip to content", "Navigation Menu", "Terms", "Privacy", "<div", "<nav", "<script"} {
				if strings.Contains(got.RawContent, bad) {
					t.Errorf("chrome %q survived", bad)
				}
			}
		})
	}
}

// url.generic's fetch -> route -> clean -> textclean chain stores clean text.
func TestURLGenericChain_StoresCleanText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(boilerplatePage))
	}))
	defer srv.Close()

	d := &storage.KnowledgeObject{Source: srv.URL}
	ctx := context.Background()
	var err error
	for _, step := range []interface {
		Run(context.Context, *storage.KnowledgeObject) (*storage.KnowledgeObject, error)
	}{NewURLFetcher(WithURLHTTPClient(srv.Client())), NewContentTypeRouter(), NewHTMLCleaner(), NewTextCleaner()} {
		if d, err = step.Run(ctx, d); err != nil {
			t.Fatalf("%T: %v", step, err)
		}
	}
	want := "Heading One\n\nFirst paragraph of the article.\n\nSecond paragraph."
	if strings.TrimSpace(d.RawContent) != want {
		t.Errorf("RawContent = %q, want %q", d.RawContent, want)
	}
	if d.Metadata["title"] != "The Real Title" {
		t.Errorf("title = %v", d.Metadata["title"])
	}
}
