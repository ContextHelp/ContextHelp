package steps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const boilerplatePage = `<!DOCTYPE html>
<html><head><title>  The Real
 Title </title><style>body{color:red}</style><script>var tracking = 1;</script></head>
<body>
<header>Site Header Brand</header>
<nav><a href="/">Home</a> <a href="/about">About Us</a></nav>
<div class="cookie-banner" role="dialog"><noscript>Enable JS please</noscript></div>
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

func TestHTMLCleaner_StripsBoilerplate(t *testing.T) {
	got := runCleaner(t, boilerplatePage, "text/html")
	for _, bad := range []string{"The Real", "Site Header", "Home", "About Us", "Sidebar", "Copyright", "Noscript", "Enable JS", "tracking", "color:red"} {
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
	}{
		{name: "main wins over article",
			in:      `<body><article>ArtOnly</article><main>MainText <article>InnerArt</article></main></body>`,
			want:    []string{"MainText", "InnerArt"},
			notWant: []string{"ArtOnly"}},
		{name: "multiple articles concatenated in order",
			in:      `<body><p>Outside</p><article>One</article><article>Two <article>Nested</article></article></body>`,
			want:    []string{"One", "Two", "Nested"},
			notWant: []string{"Outside"},
			exact:   "One\n\nTwo\n\nNested"},
		{name: "role based removal",
			in:      `<body><div role="banner">B</div><div role="navigation">N</div><div role="contentinfo">C</div><div role="complementary">S</div><div role="main">Keep</div></body>`,
			want:    []string{"Keep"},
			notWant: []string{"B\n", "N\n", "C\n", "S\n"},
			exact:   "Keep"},
		{name: "role token list case insens",
			in:    `<body><div role="Presentation NAVIGATION">gone</div><p>stay</p></body>`,
			exact: "stay"},
		{name: "entities",
			in:    `<body><p>a&nbsp;b &amp; c &lt;d&gt; it&#39;s &#x41; &quot;q&quot; &copy;</p></body>`,
			exact: `a b & c <d> it's A "q" ©`},
		{name: "inline tags do not glue words",
			in:    `<body><p>one<b>two</b> <i>three</i><span>four</span></p><div>five</div><div>six</div>x<br>y</body>`,
			exact: "onetwo threefour\n\nfive\nsix\nx\ny"},
		{name: "block boundaries",
			in:    `<body><ul><li>a</li><li>b</li></ul><table><tr><td>c</td></tr><tr><td>d</td></tr></table></body>`,
			exact: "a\nb\n\nc\nd"},
		{name: "malformed html",
			in:   `<html><body><div><p>unclosed <b>bold <i>both</p><span>tail`,
			want: []string{"unclosed", "bold", "both", "tail"}},
		{name: "garbage tags",
			in:   `<!doctype html><<>> <p <p>text</div></span>`,
			want: []string{"text"}},
		{name: "no main page keeps body minus chrome",
			in:      `<html><head><title>T</title></head><body><nav>menu</nav><h2>Head</h2><p>Body text</p><footer>foot</footer></body></html>`,
			exact:   "Head\n\nBody text",
			notWant: []string{"T\n"}},
		{name: "empty main falls back to body",
			in:    `<body><main>   </main><p>Fallback</p></body>`,
			exact: "Fallback"},
		{name: "form iframe svg template dropped",
			in:    `<body><form>f</form><iframe>i</iframe><svg><title>svgt</title><text>s</text></svg><template>t</template><p>ok</p></body>`,
			exact: "ok"},
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
			if tc.exact != "" || (len(tc.want) == 0 && len(tc.notWant) == 0) {
				if got != tc.exact {
					t.Errorf("got %q, want %q", got, tc.exact)
				}
			}
		})
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
