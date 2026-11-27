package steps

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/github"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestDistillREADME(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		repo    string // repository name
		contain []string
		absent  []string
		exact   *string // when set, output must equal it
	}{
		{
			name:    "html block to text",
			in:      "# Tool\n\n<p align=\"center\">\n  <b>Fast</b> and <a href=\"https://x.dev\">small</a>\n</p>\n\nMore prose.\n",
			contain: []string{"Fast", "small", "More prose."},
			absent:  []string{"<p", "<b>", "</a>", "href", "https://x.dev"},
		},
		{
			name:    "inline html keeps text",
			in:      "Use <kbd>Ctrl</kbd> and <span style=\"x\">more</span> text.\n",
			contain: []string{"Ctrl", "more", "text."},
			absent:  []string{"<kbd>", "</span>", "style"},
		},
		{
			name:    "br is a line break",
			in:      "first<br>second<br/>third\n",
			contain: []string{"first\nsecond\nthird"},
			absent:  []string{"<br"},
		},
		{
			name:    "html comment dropped",
			in:      "before\n\n<!-- hidden note -->\n\nafter <!-- inline note --> end\n",
			contain: []string{"before", "after", "end"},
			absent:  []string{"hidden", "inline note", "<!--"},
		},
		{
			name:    "markdown image dropped",
			in:      "Intro ![diagram](https://img.example/d.png) text.\n\n![](https://img.example/e.png)\n",
			contain: []string{"Intro", "text."},
			absent:  []string{"diagram", "img.example", "!["},
		},
		{
			name:    "img tag dropped",
			in:      "Look <img src=\"https://img.example/a.png\" alt=\"pic\"> here.\n\n<img src=\"https://img.example/b.png\">\n",
			contain: []string{"Look", "here."},
			absent:  []string{"img", "pic", "<"},
		},
		{
			name:    "badge link dropped entirely",
			in:      "# Tool\n\n[![build](https://ci.example/b.svg)](https://ci.example/run) [![ver](https://v.example/v.svg)](https://v.example)\n\nReal text.\n",
			contain: []string{"# Tool", "Real text."},
			absent:  []string{"build", "ci.example", "ver", "v.example", "[", "]"},
		},
		{
			name:    "reference style image dropped",
			in:      "Text ![logo][l] and ![other][] end.\n\n[![b][badge]][run]\n\nkept\n\n[l]: https://img.example/l.png\n[other]: https://img.example/o.png\n[badge]: https://img.example/b.svg\n[run]: https://ci.example/run\n",
			contain: []string{"Text", "end.", "kept"},
			absent:  []string{"![", "logo][", "img.example", "ci.example", "[l]:"},
		},
		{
			name:    "html h2 noise heading dropped with content",
			in:      "## Use\n\nrun it\n\n<h2 align=\"center\">Sponsors</h2>\n\n<a href=\"https://sp.example\">Acme</a>\n\n## Next\n\nn\n",
			contain: []string{"## Use", "run it", "## Next"},
			absent:  []string{"Sponsors", "Acme", "sp.example", "<h2"},
		},
		{
			name:    "html h2 normal heading kept as markdown heading",
			in:      "<h1 align=\"center\"><img src=\"l.png\"> Tool</h1>\n\nintro\n\n<h2 id=\"x\"><a href=\"#x\">Install</a></h2>\n\nrun\n\n<h3>Deep  <b>dive</b></h3>\n\nmore\n",
			contain: []string{"# Tool\n", "\n## Install\n", "\n### Deep dive\n", "run", "more"},
			absent:  []string{"<h", "href"},
		},
		{
			name:    "unused reference definitions dropped, used kept",
			in:      "See [docs][d] and [site].\n\n![logo][l]\n\n[d]: https://docs.example\n[site]: https://site.example\n[l]: https://img.example/l.png\n[Unused]: https://u.example\n",
			contain: []string{"See [docs][d] and [site].", "[d]: https://docs.example", "[site]: https://site.example"},
			absent:  []string{"img.example", "[l]:", "Unused", "u.example"},
		},
		{
			name:    "definition under noise section kept when prose uses it",
			in:      "# T\n\nSee [docs][d] and [site].\n\n## License\n\nMIT\n\n[d]: https://docs.example\n[site]: https://site.example\n",
			contain: []string{"See [docs][d] and [site].", "[d]: https://docs.example", "[site]: https://site.example"},
			absent:  []string{"License", "MIT"},
		},
		{
			name:    "definition used only by dropped section is dropped",
			in:      "# T\n\nintro\n\n## Community\n\nsee [x]\n\n[x]: https://x.example\n",
			contain: []string{"intro"},
			absent:  []string{"x.example", "[x]", "Community"},
		},
		{
			name:  "pre becomes fenced code and keeps headings intact",
			in:    "## Install\n\n<pre>\n# clone\ngit clone x\n    cd x &amp;&amp; make\n</pre>\n\n## Usage\n\nrun\n",
			exact: strp("## Install\n\n```\n# clone\ngit clone x\n    cd x && make\n```\n\n## Usage\n\nrun\n"),
		},
		{
			name:  "html text line that looks like a heading is escaped",
			in:    "## Intro\n\n<div align=\"center\">\n# Fast\n</div>\n\ntext\n",
			exact: strp("## Intro\n\n\\# Fast\n\ntext\n"),
		},
		{
			name:  "entity decoded heading marker stays literal",
			in:    "## A\n\n<p>&#35;&#35; x</p>\n\ntext\n",
			exact: strp("## A\n\n\\## x\n\ntext\n"),
		},
		{
			name:  "html setext-like line is escaped",
			in:    "## A\n\n<p>foo</p>\n\n<p>---</p>\n",
			exact: strp("## A\n\nfoo\n\n\\---\n"),
		},
		{
			name:    "noise word as document title keeps the readme",
			in:      "# Community\n\nThis repo holds governance docs.\n\n## Meetings\n\nweekly\n",
			contain: []string{"# Community", "governance docs", "## Meetings", "weekly"},
		},
		{
			name:    "support headings",
			repo:    "archify",
			in:      "## Support matrix\n\nm\n\n## Support for Windows\n\nw\n\n## Support Archify\n\nthanks\n\n## Support us\n\nu\n\n## Support the project\n\np\n\n## Support this project\n\np2\n\n## Support development\n\nd\n",
			contain: []string{"## Support matrix", "## Support for Windows"},
			absent:  []string{"Support Archify", "thanks", "Support us", "Support the project", "Support this project", "Support development"},
		},
		{
			name:    "multi-line setext noise heading",
			in:      "# Title\n\nt\n\nCode of\nConduct\n====\n\nrules\n",
			contain: []string{"# Title"},
			absent:  []string{"Conduct", "rules"},
		},
		{
			name:    "br in table row becomes space",
			in:      "| one<br>two | x |\n|---|---|\n| a | b |\n",
			contain: []string{"| one two | x |", "| a | b |"},
		},
		{
			name:  "list items emptied by images are removed whole",
			in:    "## Install\n\nSupported on:\n- ![linux](l.svg)\n- ![mac](m.svg)\n\n## Usage\n\nrun\n",
			exact: strp("## Install\n\nSupported on:\n\n## Usage\n\nrun\n"),
		},
		{
			name:  "list item with text keeps its marker",
			in:    "- ![a](b)\n- real item\n",
			exact: strp("- real item\n"),
		},
		{
			name:    "shortcut reference image dropped with its definition",
			in:      "Logo ![logo] here.\n\n[logo]: l.png\n",
			contain: []string{"Logo", "here."},
			absent:  []string{"[logo]", "l.png"},
		},
		{
			name:  "image url with balanced parentheses",
			in:    "A ![x](https://e/a_(b).png) B\n",
			exact: strp("A B\n"),
		},
		{
			name:  "image alt with nested brackets",
			in:    "A ![a [b] c](x.png) B\n",
			exact: strp("A B\n"),
		},
		{
			name:    "escaped image syntax left intact beside a real image",
			in:      "\\![not](img) and ![real](r.png) end\n",
			contain: []string{"\\![not](img)", "end"},
			absent:  []string{"real", "r.png"},
		},
		{
			name:    "code span with image syntax kept beside a real image",
			in:      "Clone `![x](y)` then ![real](r.png)\n",
			contain: []string{"`![x](y)`", "then"},
			absent:  []string{"real", "r.png"},
		},
		{
			name:    "html h4 to h6",
			in:      "<h4>Four</h4>\n\nf\n\n<h5>Five</h5>\n\nv\n\n<h6>Six</h6>\n\ns\n",
			contain: []string{"#### Four\n", "##### Five\n", "###### Six\n"},
			absent:  []string{"<h"},
		},
		{
			name:    "inline html in heading",
			in:      "## Fast <b>and</b> small<br>ish\n\ntext\n",
			contain: []string{"## Fast and small ish"},
		},
		{
			name:  "html image list items removed whole",
			in:    "## Install\n\nSupported on:\n- <img src=\"l.svg\">\n- <img src=\"m.svg\">\n\n## Usage\n\nrun\n",
			exact: strp("## Install\n\nSupported on:\n\n## Usage\n\nrun\n"),
		},
		{
			name:  "html comment list item removed",
			in:    "- <!-- c -->\n- b\n",
			exact: strp("- b\n"),
		},
		{
			name:  "html comment quote line removed",
			in:    "> <!-- c -->\n> quote\n",
			exact: strp("> quote\n"),
		},
		{
			name:  "emptied html list item does not join next paragraph",
			in:    "- <img src=\"a\">\nnext para\n",
			exact: strp("next para\n"),
		},
		{
			name:  "pre inside heading folds into title",
			in:    "# A\n\n<h2><pre>code\n  x</pre></h2>\n\ntext\n",
			exact: strp("# A\n\n## code x\n\ntext\n"),
		},
		{
			name:  "br followed by atx heading text is escaped",
			in:    "# T\n\nLine one<br>## Sponsors\n\nKeep me\n\n## Usage\n\nrun\n",
			exact: strp("# T\n\nLine one\n\\## Sponsors\n\nKeep me\n\n## Usage\n\nrun\n"),
		},
		{
			name:  "br followed by setext underline is escaped",
			in:    "# T\n\nFoo<br>---\n\nbar\n",
			exact: strp("# T\n\nFoo\n\\---\n\nbar\n"),
		},
		{
			name:    "marker runes in input cannot forge structure",
			in:      "# T\n\n<p>2 Sponsors</p>\n\nKeep me\n\n## Use\n\nrun\n",
			contain: []string{"Keep me", "## Use", "run", "�2 Sponsors"},
			absent:  []string{"## Sponsors"},
		},
		{
			name:  "emptied list item pulls its later paragraph up",
			in:    "1.  ![x](y)\n\n    more text\n",
			exact: strp("1.  more text\n"),
		},
		{
			name:  "empty pre emits nothing",
			in:    "## A\n\n<pre></pre>\n\ntext\n",
			exact: strp("## A\n\ntext\n"),
		},
		{
			name:  "setext heading starting with hash is empty when only underlined",
			in:    "# T\n\nintro\n\n#tag\n===\n",
			exact: strp("# T\n\nintro\n"),
		},
		{
			name:  "section holding only referenced definitions is empty",
			in:    "See [d].\n\n## Links\n\n[d]: https://d\n",
			exact: strp("See [d].\n\n[d]: https://d\n"),
		},
		{
			name:    "support repo heading kept for another repo",
			repo:    "other",
			in:      "## Support Archify\n\nthanks\n",
			contain: []string{"## Support Archify", "thanks"},
		},
		{
			name:  "emptied list item with text in html keeps the bullet",
			in:    "- <div>x</div>\n- y\n",
			exact: strp("- x\n- y\n"),
		},
		{
			name:  "star list item with html paragraph keeps the bullet",
			in:    "* <p>para</p>\n* z\n",
			exact: strp("* para\n* z\n"),
		},
		{
			name:    "quoted html keeps the quote",
			in:      "> <div>quoted html</div>\n> more\n",
			contain: []string{"> quoted html", "> more"},
			absent:  []string{"\nmore", "\nquoted"},
		},
		{
			name:  "br followed by fence is escaped",
			in:    "# T\n\nintro<br>```sh\nrun\n\n## License\n\nMIT\n",
			exact: strp("# T\n\nintro\n\\```sh\nrun\n"),
		},
		{
			name:  "emptied item pulls paragraph up under a paragraph line",
			in:    "Intro text\n- ![badge](x)\n\n  more\n",
			exact: strp("Intro text\n- more\n"),
		},
		{
			name:  "emptied html item pulls paragraph up under a paragraph line",
			in:    "Intro text\n- <img src=a>\n\n  more\n",
			exact: strp("Intro text\n- more\n"),
		},
		{
			name:  "emptied item pulls nested list up",
			in:    "Intro text\n- ![x](y)\n  - nested\n",
			exact: strp("Intro text\n- - nested\n"),
		},
		{
			name:    "details in list item under a paragraph line",
			in:      "Notes\n- <details>\n  <summary>S</summary>\n\n  body\n  </details>\n",
			contain: []string{"Notes\n- S\n", "  body"},
		},
		{
			name:  "star item emptied by image keeps caption",
			in:    "Screenshots:\n* ![x](y)\n\n  caption\n",
			exact: strp("Screenshots:\n* caption\n"),
		},
		{
			name:    "heading in an emptied item stays in the item",
			in:      "# Proj\n\nFeatures\n- ![x](y)\n\n  ## Sponsors\n\n  text\n\n## Usage\n\nrun\n",
			contain: []string{"Features", "text", "## Usage", "run"},
		},
		{
			name:  "multi-line html item with blank lines stays one item",
			in:    "- <details>\n  <summary>S</summary>\n\n  body\n\n  </details>\n- next\n",
			exact: strp("- S\n\n  body\n\n- next\n"),
		},
		{
			name:  "multi-line html paragraph in item",
			in:    "- <p>\n  hello world\n  </p>\n- next\n",
			exact: strp("- hello world\n- next\n"),
		},
		{
			name:    "details in quote keeps summary in the quote",
			in:      "> <details>\n> <summary>S</summary>\n>\n> body\n> </details>\n",
			contain: []string{"> S", "> body"},
		},
		{
			name:    "html fence-like line is escaped",
			in:      "<div>\n```\n</div>\n\n## Usage\n\ntext\n",
			contain: []string{"## Usage", "text", "\\```"},
		},
		{
			name:  "html text with tag-like entities stays text",
			in:    "<p>Use &lt;img src=x&gt; and &lt;h2&gt;Title&lt;/h2&gt; tags</p>\n",
			exact: strp("Use \\<img src=x> and \\<h2>Title\\</h2> tags\n"),
		},
		{
			name:  "indented code after an emptied item is fenced with its body intact",
			in:    "1.  ![x](y)\n\n        code line\n",
			exact: strp("1.  ```\n    code line\n    ```\n"),
		},
		{
			name:    "bare marker under a list line gets a blank line before it",
			in:      "- a\n-\n- b\n",
			contain: []string{"- a\n\n-\n"},
		},
		{
			name:    "ordinary link kept",
			in:      "See [the docs](https://docs.example/x) now.\n",
			contain: []string{"See [the docs](https://docs.example/x) now."},
		},
		{
			name:    "fenced code verbatim",
			in:      "Run:\n\n```html\n<div class=\"a\"><br>\n![x](y)\n## Sponsors\n\n\n\n<!-- c -->\n```\n\nDone.\n",
			contain: []string{"```html\n<div class=\"a\"><br>\n![x](y)\n## Sponsors\n\n\n\n<!-- c -->\n```", "Done."},
		},
		{
			name:    "tilde fence verbatim",
			in:      "~~~\n<b>x</b> ![a](b)\n~~~\n",
			contain: []string{"~~~\n<b>x</b> ![a](b)\n~~~"},
		},
		{
			name:    "indented code verbatim",
			in:      "Para\n\n    <b>raw</b>\n    ![a](b)\n\n    tail   \n\nNext\n",
			contain: []string{"    <b>raw</b>\n    ![a](b)\n\n    tail   \n", "Next"},
		},
		{
			name:    "inline code span with angle brackets kept",
			in:      "Clone `<owner>/<repo>` and run `![x](y)` or ``a `<b>` c``.\n",
			contain: []string{"`<owner>/<repo>`", "`![x](y)`", "``a `<b>` c``"},
		},
		{
			name:    "noise section dropped with subsections",
			in:      "# Tool\n\nIntro.\n\n## Sponsors\n\nThanks!\n\n### Gold\n\nAcme\n\n## Usage\n\nRun it.\n",
			contain: []string{"# Tool", "Intro.", "## Usage", "Run it."},
			absent:  []string{"Sponsors", "Thanks", "Gold", "Acme"},
		},
		{
			name:    "noise section ends at same or higher level",
			in:      "## A\n\na text\n\n### Community\n\nchat\n\n#### Deep\n\ndeep\n\n### B\n\nb text\n\n## C\n\nc text\n",
			contain: []string{"## A", "a text", "### B", "b text", "## C", "c text"},
			absent:  []string{"Community", "chat", "Deep", "deep"},
		},
		{
			name:    "all noise headings",
			repo:    "acme",
			in:      "## Sponsor\nx1\n## Backers\nx2\n## Supporters\nx3\n## Community\nx4\n## License\nx5\n## Contributing\nx6\n## Contributors\nx7\n## Star History\nx8\n## Stargazers\nx9\n## Support\nx10\n## Support Acme\nx11\n## Support us\nx15\n## Acknowledgements\nx12\n## Acknowledgments\nx13\n## Code of Conduct\nx14\n## Keep\nkept\n",
			contain: []string{"## Keep", "kept"},
			absent:  []string{"x1", "x2", "x3", "x4", "x5", "x6", "x7", "x8", "x9", "x10", "x11", "x12", "x13", "x14", "x15"},
		},
		{
			name:    "noise heading with decoration",
			in:      "## ❤️ Sponsors\n\nthanks\n\n## [Star-History](https://h.example)\n\nchart\n\n## **License**\n\nMIT\n\n## Keep\n\nkept\n",
			contain: []string{"## Keep", "kept"},
			absent:  []string{"Sponsors", "thanks", "chart", "MIT", "History"},
		},
		{
			name:    "setext noise heading",
			in:      "Sponsors\n--------\n\nthanks\n\nUsage\n=====\n\nrun\n",
			contain: []string{"Usage", "run"},
			absent:  []string{"Sponsors", "thanks"},
		},
		{
			name:    "heading containing noise word kept",
			in:      "## Community plugins API\n\nplugins here\n\n## Supporting files\n\nfiles\n\n## License checks\n\nchecks\n",
			contain: []string{"## Community plugins API", "plugins here", "## Supporting files", "## License checks", "checks"},
		},
		{
			name:    "noise heading inside code not a heading",
			in:      "```\n## Sponsors\n```\n\n## Keep\n\nkept\n",
			contain: []string{"## Sponsors", "## Keep"},
		},
		{
			name:    "empty section dropped",
			in:      "## Install\n\nrun it\n\n## Empty\n\n## Badges only\n\n[![a](b)](c)\n\n## Last\n\ntext\n",
			contain: []string{"## Install", "run it", "## Last", "text"},
			absent:  []string{"Empty", "Badges only"},
		},
		{
			name:    "section with non-empty subsection kept",
			in:      "## Parent\n\n### Child\n\nchild text\n\n### Hollow\n\n## Next\n\nn\n",
			contain: []string{"## Parent", "### Child", "child text", "## Next"},
			absent:  []string{"Hollow"},
		},
		{
			name:    "blank runs collapse",
			in:      "a\n\n\n\n\nb\n\n   \n\nc\n",
			contain: []string{"a\n\nb\n\nc"},
		},
		{
			name:    "headings preserve levels",
			in:      "# One\n\ntext\n\n## Two\n\ntext\n\n### Three\n\ntext\n",
			contain: []string{"# One\n", "\n## Two\n", "\n### Three\n"},
		},
		{
			name:    "lists quotes tables kept",
			in:      "- item <b>one</b>\n- item two\n\n> quoted <i>text</i>\n\n| a | b |\n|---|---|\n| 1 | 2 |\n",
			contain: []string{"- item one", "- item two", "> quoted text", "| a | b |", "|---|---|", "| 1 | 2 |"},
			absent:  []string{"<b>", "<i>"},
		},
		{
			name:    "unclosed and malformed constructs",
			in:      "<div><p>open\n\n<<<>>> <a href=\"x\n\n![unterminated](http://x\n\n```\nunclosed fence <b>\n",
			contain: []string{"unclosed fence <b>"},
		},
		{
			name:  "empty input",
			in:    "",
			exact: strp(""),
		},
		{
			name:  "whitespace only",
			in:    " \n\n\t\n",
			exact: strp(""),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := distillREADME(tc.in, tc.repo)
			if tc.exact != nil && got != *tc.exact {
				t.Fatalf("got %q, want %q", got, *tc.exact)
			}
			for _, w := range tc.contain {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			for _, w := range tc.absent {
				if strings.Contains(got, w) {
					t.Errorf("unexpected %q in:\n%s", w, got)
				}
			}
			if strings.ContainsAny(got, "") {
				t.Errorf("private-use marker leaked into output: %q", got)
			}
			if strings.Contains(got, "\n\n\n") && !strings.Contains(tc.in, "```") && !strings.Contains(tc.in, "~~~") {
				t.Errorf("blank run not collapsed in:\n%q", got)
			}
		})
	}
}

func strp(s string) *string { return &s }

// A panic in the distiller returns the input and logs the panic value, not
// the README text.
func TestGuardDistill_PanicKeepsInput(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(old)

	src := "# secret readme body\n"
	got := guardDistill(src, func() string { panic("boom") })
	if got != src {
		t.Fatalf("got %q, want the input", got)
	}
	if !strings.Contains(logs.String(), "boom") || strings.Contains(logs.String(), "secret") {
		t.Fatalf("log should carry the panic value only: %s", logs.String())
	}
}

// A README with nothing to distill comes back unchanged.
func TestDistillREADME_CleanInputUnchanged(t *testing.T) {
	in := "## Survey\nRangers counted forty numbats.\n\n- one\n- two\n\n```go\nx := 1\n```\n"
	if got := distillREADME(in, ""); got != in {
		t.Fatalf("got %q, want %q", got, in)
	}
}

func TestDistillREADME_Deterministic(t *testing.T) {
	in := "# T\n\n<p><b>x</b></p>\n\n## Sponsors\n\ns\n\n## Use\n\n```\n<a>\n```\n"
	a := distillREADME(in, "")
	for i := 0; i < 5; i++ {
		if b := distillREADME(in, ""); a != b {
			t.Fatalf("non-deterministic output: %q vs %q", a, b)
		}
	}
}

func TestDistillREADME_Idempotent(t *testing.T) {
	in := "# T\n\n<p><b>x</b></p>\n\n## Sponsors\n\ns\n\n## Use\n\n```\n<a>\n```\n\ntext <br> more\n"
	once := distillREADME(in, "")
	if twice := distillREADME(once, ""); twice != once {
		t.Fatalf("not idempotent:\n%q\n%q", once, twice)
	}
}

// Distilled output still splits on "## " headings in the sectioner.
func TestDistillREADME_SectionerStillSplits(t *testing.T) {
	in := "# Tool\n\n<p align=\"center\"><img src=\"x.png\"></p>\n\nIntro text.\n\n## Install\n\nInstall text.\n\n## Sponsors\n\nthanks\n\n## Usage\n\nUsage text.\n"
	d := &storage.KnowledgeObject{RawContent: distillREADME(in, "")}
	out, err := NewSectioner().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, s := range out.Sections {
		titles = append(titles, s.Title)
	}
	joined := strings.Join(titles, "|")
	for _, w := range []string{"Install", "Usage"} {
		if !strings.Contains(joined, w) {
			t.Errorf("sections %q lack %q", joined, w)
		}
	}
	if strings.Contains(joined, "Sponsors") {
		t.Errorf("sections %q include Sponsors", joined)
	}
}

func TestRepoText_DistillsREADME(t *testing.T) {
	snap := &github.RepoSnapshot{
		FullName:    "o/r",
		Description: "A tool",
		Topics:      []string{"go", "cli"},
		README: "<h1 align=\"center\"><img src=\"logo.png\"></h1>\n\n" +
			"[![ci](https://ci.example/b.svg)](https://ci.example)\n\n" +
			"## Features\n\nFast <b>and</b> small. See [docs](https://docs.example).\n\n" +
			"```sh\ncurl <url> | sh\n```\n\n" +
			"## 💖 Sponsors\n\n<a href=\"https://sp.example\"><img src=\"sp.png\"></a>\n\n" +
			"## Contributors\n\n![c](https://contrib.example/c.png)\n\n" +
			"## License\n\nMIT\n",
	}
	got := repoText(snap)
	for _, w := range []string{"# o/r", "A tool", "Topics: go, cli", "## Features", "Fast and small.", "[docs](https://docs.example)", "curl <url> | sh"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
	for _, w := range []string{"<b>", "<img", "ci.example", "Sponsors", "sp.example", "Contributors", "contrib.example", "License", "MIT"} {
		if strings.Contains(got, w) {
			t.Errorf("unexpected %q in:\n%s", w, got)
		}
	}
}
