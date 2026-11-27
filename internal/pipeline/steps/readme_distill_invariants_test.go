package steps

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"github.com/yuin/goldmark/ast"
	"golang.org/x/net/html"
)

// An emptied list item keeps its later children inside the item: the
// output parses as an item holding them, with no code block.
func TestDistillREADME_EmptiedItemKeepsChildren(t *testing.T) {
	for _, in := range []string{
		"1.  ![x](y)\n\n    more text\n",
		"1. ![x](y)\n\n   more text\n",
		"- <img src=a>\n\n  continued para\n- z\n",
	} {
		out := distillREADME(in, "")
		doc := parseMarkdown([]byte(out))
		lists, items, inItem, code := 0, 0, false, false
		_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			switch n.(type) {
			case *ast.List:
				lists++
			case *ast.ListItem:
				items++
			case *ast.CodeBlock, *ast.FencedCodeBlock:
				code = true
			case *ast.Paragraph, *ast.TextBlock:
				txt := inlineText(n, []byte(out))
				if (strings.Contains(txt, "more text") || strings.Contains(txt, "continued para")) && inListItem(n) {
					inItem = true
				}
			}
			return ast.WalkContinue, nil
		})
		if code || !inItem || lists != 1 {
			t.Errorf("input %q -> %q: lists=%d items=%d childInItem=%v codeBlock=%v", in, out, lists, items, inItem, code)
		}
		if strings.Contains(in, "- z") && items != 2 {
			t.Errorf("input %q -> %q: want 2 items, got %d", in, out, items)
		}
	}
}

func inListItem(n ast.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if _, ok := p.(*ast.ListItem); ok {
			return true
		}
	}
	return false
}

// invLeaf is a text-bearing block with how deeply it sits in list items and
// blockquotes.
type invLeaf struct {
	text    string
	li, bq  int
	heading bool
}

type invCode struct {
	body   string
	li, bq int
}

type invHeading struct {
	level int
	text  string
}

type invDoc struct {
	leaves   []invLeaf
	codes    []invCode
	headings []invHeading
	rawHTML  int // RawHTML, HTMLBlock and Image nodes
}

func depth(n ast.Node) (li, bq int) {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.(type) {
		case *ast.ListItem:
			li++
		case *ast.Blockquote:
			bq++
		}
	}
	return li, bq
}

func linesText(n ast.Node, src []byte) string {
	var sb strings.Builder
	l := n.Lines()
	for i := 0; i < l.Len(); i++ {
		seg := l.At(i)
		sb.Write(seg.Value(src))
	}
	return sb.String()
}

// htmlPlainText is the text of raw HTML without comments or tags.
func htmlPlainText(raw string) string {
	var sb strings.Builder
	z := html.NewTokenizer(strings.NewReader(raw))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return sb.String()
		case html.TextToken:
			sb.WriteString(" " + z.Token().Data)
		}
	}
}

// inlineText is the text of a leaf block, without image alt text.
func inlineText(n ast.Node, src []byte) string {
	var sb strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := c.(type) {
		case *ast.Image:
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			sb.Write(v.Segment.Value(src))
			if v.SoftLineBreak() || v.HardLineBreak() {
				sb.WriteByte(' ')
			}
		case *ast.String:
			sb.Write(v.Value)
		}
		return ast.WalkContinue, nil
	})
	return sb.String()
}

func readInv(s string) invDoc {
	src := []byte(s)
	var d invDoc
	_ = ast.Walk(parseMarkdown(src), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		li, bq := depth(n)
		switch v := n.(type) {
		case *ast.RawHTML, *ast.Image:
			d.rawHTML++
		case *ast.HTMLBlock:
			d.rawHTML++
			d.leaves = append(d.leaves, invLeaf{text: htmlPlainText(linesText(v, src)), li: li, bq: bq})
		case *ast.Heading:
			t := inlineText(v, src)
			d.headings = append(d.headings, invHeading{v.Level, t})
			d.leaves = append(d.leaves, invLeaf{text: t, li: li, bq: bq, heading: true})
		case *ast.Paragraph, *ast.TextBlock:
			d.leaves = append(d.leaves, invLeaf{text: inlineText(v, src), li: li, bq: bq})
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			d.codes = append(d.codes, invCode{linesText(v, src), li, bq})
		}
		return ast.WalkContinue, nil
	})
	return d
}

func words(s string) []string {
	var out []string
	for _, w := range strings.Fields(s) {
		if strings.IndexFunc(w, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
			out = append(out, strings.Trim(w, `\`))
		}
	}
	return out
}

// invViolations reports the invariants distillREADME breaks for in -> out.
func invViolations(in, out string) []string {
	is, os := readInv(in), readInv(out)
	var bad []string
	bad = append(bad, headingViolations(in, out, is, os)...)
	bad = append(bad, wordViolations(is, os)...)
	bad = append(bad, codeViolations(is, os)...)
	bad = append(bad, outputViolations(out, os)...)
	return bad
}

// 1: no forged headings; a heading with content around it stays.
func headingViolations(in, out string, is, os invDoc) []string {
	var bad []string
	inHeads := map[string]bool{}
	for _, h := range is.headings {
		inHeads[strings.TrimSpace(h.text)] = true
	}
	outHeads := map[invHeading]bool{}
	for _, h := range os.headings {
		outHeads[invHeading{h.level, strings.TrimSpace(h.text)}] = true
		if !inHeads[strings.TrimSpace(h.text)] {
			bad = append(bad, fmt.Sprintf("forged heading %q", h.text))
		}
	}
	if strings.Contains(in, "## Next") && !outHeads[invHeading{2, "Next"}] {
		bad = append(bad, "heading Next lost")
	}
	if strings.Contains(in, "## Lead") {
		head := out
		if i := strings.Index(out, "## Next"); i >= 0 {
			head = out[:i]
		}
		hasContent := strings.TrimSpace(strings.Replace(head, "## Lead", "", 1)) != ""
		if hasContent && !outHeads[invHeading{2, "Lead"}] {
			bad = append(bad, "heading Lead lost though its section has content")
		}
	}
	return bad
}

// 2 and 7: every word survives, at least as deep in lists and quotes.
func wordViolations(is, os invDoc) []string {
	var bad []string
	for _, l := range is.leaves {
		if l.heading {
			continue // headings are checked above: an empty section may go
		}
		for _, w := range words(l.text) {
			if !wordKept(w, l, os) {
				bad = append(bad, fmt.Sprintf("word %q lost or moved out of its list/quote (in li=%d bq=%d)", w, l.li, l.bq))
			}
		}
	}
	return bad
}

func wordKept(w string, l invLeaf, os invDoc) bool {
	for _, o := range os.leaves {
		if strings.Contains(o.text, w) && o.li >= l.li && o.bq >= l.bq {
			return true
		}
	}
	for _, c := range os.codes {
		if strings.Contains(c.body, w) && c.li >= l.li && c.bq >= l.bq {
			return true
		}
	}
	return false
}

// 3 and 7: code bodies byte for byte, still inside their containers.
func codeViolations(is, os invDoc) []string {
	var bad []string
	for _, c := range is.codes {
		found := false
		for _, o := range os.codes {
			found = found || (o.body == c.body && o.li >= c.li && o.bq >= c.bq)
		}
		if !found {
			bad = append(bad, fmt.Sprintf("code body %q lost or changed", c.body))
		}
	}
	return bad
}

// 4, 5, 6.
func outputViolations(out string, os invDoc) []string {
	var bad []string
	if os.rawHTML > 0 {
		bad = append(bad, "output has raw HTML, HTML blocks or images")
	}
	for _, ln := range strings.Split(out, "\n") {
		if bareMarkerRe.MatchString(ln) {
			bad = append(bad, fmt.Sprintf("line %q is only a list marker", ln))
		}
	}
	if strings.ContainsAny(out, "") {
		bad = append(bad, "private-use marker in output")
	}
	return bad
}

type invContainer struct {
	name     string
	pre      string // lines before the item line
	first    string // marker (and indentation) of the item line
	cont     string // prefix of the item's continuation lines
	sibling  string // a sibling item line, "" when the container has none
	orderedN bool   // ordered list not starting at 1: cannot interrupt a paragraph
}

// TestDistillREADME_Invariants builds inputs from the cross product of a
// lead-in, a container, a first content that distilling empties or
// converts, and a continuation, and checks the output invariants on each.
//
// Skipped as not valid Markdown for the container: an ordered list not
// starting at 1 straight under a paragraph line (it cannot interrupt the
// paragraph), and a sibling item for the blockquote (it has no items).
func TestDistillREADME_Invariants(t *testing.T) {
	leads := map[string]string{
		"none": "", "para": "Intro text\n", "para-blank": "Intro text\n\n", "heading": "## Lead\n",
	}
	conts := []invContainer{
		{name: "dash", first: "- ", cont: "  ", sibling: "- sib"},
		{name: "star", first: "* ", cont: "  ", sibling: "* sib"},
		{name: "plus", first: "+ ", cont: "  ", sibling: "+ sib"},
		{name: "ol1", first: "1. ", cont: "   ", sibling: "2. sib"},
		{name: "ol10", first: "10. ", cont: "    ", sibling: "11. sib", orderedN: true},
		{name: "quote", first: "> ", cont: "> "},
		{name: "nested", pre: "- a\n", first: "  - ", cont: "    ", sibling: "  - sib"},
	}
	firsts := map[string][]string{
		"image": {"![x](y)"}, "badge": {"[![b](i)](l)"}, "img": {"<img src=a>"},
		"comment": {"<!-- c -->"}, "div": {"<div>x</div>"},
		"details": {"<details>", "<summary>S</summary>", "", "body", "", "</details>"},
		"p":       {"<p>", "hello", "</p>"},
	}
	conns := []string{"none", "para", "nested", "fenced", "indented", "sibling", "section"}
	kLines := map[string][]string{
		"para":     {"", "more text"},
		"nested":   {"- nested item"},
		"fenced":   {"", "```go", "x := 1", "```"},
		"indented": {"", "    code line"},
	}

	run, skipped, failed := 0, 0, 0
	for ln, lead := range leads {
		for _, c := range conts {
			for fn, f := range firsts {
				for _, kn := range conns {
					if c.orderedN && ln == "para" {
						skipped++
						continue
					}
					if kn == "sibling" && c.sibling == "" {
						skipped++
						continue
					}
					blank := strings.TrimRight(c.cont, " ")
					lines := []string{}
					for i, l := range f {
						switch {
						case i == 0:
							lines = append(lines, c.first+l)
						case l == "":
							lines = append(lines, blank)
						default:
							lines = append(lines, c.cont+l)
						}
					}
					for _, l := range kLines[kn] {
						if l == "" {
							lines = append(lines, blank)
						} else {
							lines = append(lines, c.cont+l)
						}
					}
					if kn == "sibling" {
						lines = append(lines, c.sibling)
					}
					in := lead + c.pre + strings.Join(lines, "\n") + "\n"
					if kn == "section" {
						in += "\n## Next\n\ntext after\n"
					}
					run++
					out := distillREADME(in, "")
					if bad := invViolations(in, out); len(bad) > 0 {
						failed++
						if failed <= 25 {
							t.Errorf("lead=%s container=%s first=%s next=%s\n in:  %q\n out: %q\n %s", ln, c.name, fn, kn, in, out, strings.Join(bad, "\n "))
						}
					}
				}
			}
		}
	}
	t.Logf("%d generated inputs run, %d skipped, %d failed", run, skipped, failed)
	if run < 1000 {
		t.Fatalf("only %d inputs generated", run)
	}
}

// Tab-indented list items: an emptied item keeps its children and no fence
// is left unclosed.
func TestDistillREADME_TabIndentedItems(t *testing.T) {
	out := distillREADME("-\t![a](b)\n\n\tmore tab\n", "")
	d := readInv(out)
	found := false
	for _, l := range d.leaves {
		found = found || (strings.Contains(l.text, "more tab") && l.li >= 1)
	}
	if !found || len(d.codes) != 0 {
		t.Errorf("more tab should be a paragraph in the item, got %q", out)
	}

	out = distillREADME("-\t![a](b)\n\n\t    code tab\n\n## Next\n\ntext after\n", "")
	d = readInv(out)
	next := false
	for _, h := range d.headings {
		next = next || (h.level == 2 && h.text == "Next")
	}
	if !next || !strings.Contains(out, "code tab") {
		t.Errorf("## Next must stay a heading and the code survive, got %q", out)
	}
}

// HTML-derived text with a backslash before a tag-like "<" never becomes
// live HTML.
func TestDistillREADME_BackslashBeforeTagLikeText(t *testing.T) {
	out := distillREADME("<p>\\&lt;b&gt; bold words</p>\n", "")
	if readInv(out).rawHTML != 0 {
		t.Errorf("output has raw HTML: %q", out)
	}
}
