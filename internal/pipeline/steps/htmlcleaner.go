package steps

import (
	"context"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// HTMLCleaner turns an HTML document in RawContent into readable text. It
// parses the document, drops page chrome (head, scripts, styles, navigation,
// headers, footers, sidebars, forms and elements with a landmark role of
// banner, navigation, contentinfo or complementary), prefers the <main> or
// <article> subtree when the page has one, and records the <title> as
// Metadata["title"] instead of body text. Non-HTML input passes through.
type HTMLCleaner struct {
	pipeline.BaseContract
}

func NewHTMLCleaner() *HTMLCleaner {
	return &HTMLCleaner{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"RawContent", "Metadata"},
		}),
	}
}

func (s *HTMLCleaner) Name() string { return "html_cleaner" }

// droppedElements are removed with their whole subtree.
var droppedElements = map[atom.Atom]bool{
	atom.Head: true, atom.Script: true, atom.Style: true, atom.Noscript: true,
	atom.Template: true, atom.Nav: true, atom.Header: true, atom.Footer: true,
	atom.Aside: true, atom.Form: true, atom.Iframe: true, atom.Svg: true,
}

// droppedRoles are landmark roles that mark page chrome.
var droppedRoles = map[string]bool{
	"banner": true, "navigation": true, "contentinfo": true, "complementary": true,
}

// paragraphElements end a paragraph (blank line); other block elements end a line.
var paragraphElements = map[atom.Atom]bool{
	atom.P: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true,
	atom.H5: true, atom.H6: true, atom.Blockquote: true, atom.Pre: true,
	atom.Ul: true, atom.Ol: true, atom.Table: true, atom.Dl: true,
	atom.Figure: true, atom.Section: true, atom.Article: true, atom.Main: true,
	atom.Hr: true,
}

var lineElements = map[atom.Atom]bool{
	atom.Div: true, atom.Li: true, atom.Tr: true, atom.Br: true, atom.Dt: true,
	atom.Dd: true, atom.Figcaption: true, atom.Address: true, atom.Details: true,
	atom.Summary: true, atom.Fieldset: true, atom.Caption: true, atom.Body: true,
}

func (s *HTMLCleaner) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if !strings.Contains(strings.ToLower(draft.ContentType), "html") && !strings.HasPrefix(strings.TrimSpace(draft.RawContent), "<!") {
		return draft, nil
	}

	doc, err := html.Parse(strings.NewReader(draft.RawContent))
	if err != nil {
		// The parser tolerates malformed markup; a read error on an
		// in-memory string is not expected. Never fail the job over it.
		return draft, nil
	}

	if title := collapseSpace(findTitle(doc)); title != "" {
		if draft.Metadata == nil {
			draft.Metadata = make(map[string]any)
		}
		if existing, _ := draft.Metadata["title"].(string); strings.TrimSpace(existing) == "" {
			draft.Metadata["title"] = title
		}
	}

	text := ""
	if roots := contentRoots(doc); len(roots) > 0 {
		text = extractText(roots)
	}
	if text == "" {
		text = extractText([]*html.Node{doc})
	}

	draft.RawContent = text
	return draft, nil
}

// isDropped reports whether n is page chrome removed with its subtree.
func isDropped(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if droppedElements[n.DataAtom] {
		return true
	}
	for _, a := range n.Attr {
		if a.Key != "role" {
			continue
		}
		for _, role := range strings.Fields(strings.ToLower(a.Val)) {
			if droppedRoles[role] {
				return true
			}
		}
	}
	return false
}

// findTitle returns the text of the first HTML <title> element.
func findTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.DataAtom == atom.Title && n.Namespace == "" {
		var sb strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				sb.WriteString(c.Data)
			}
		}
		return sb.String()
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := findTitle(c); t != "" {
			return t
		}
	}
	return ""
}

// contentRoots returns the <main> elements if any, else the outermost
// <article> elements in document order, skipping dropped subtrees.
func contentRoots(doc *html.Node) []*html.Node {
	var mains, articles []*html.Node
	var walk func(n *html.Node, inArticle bool)
	walk = func(n *html.Node, inArticle bool) {
		if isDropped(n) {
			return
		}
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Main:
				mains = append(mains, n)
				return
			case atom.Article:
				if !inArticle {
					articles = append(articles, n)
				}
				inArticle = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inArticle)
		}
	}
	walk(doc, false)
	if len(mains) > 0 {
		return mains
	}
	return articles
}

// extractText renders the text of roots with block boundaries as newlines:
// one for line-level blocks, two (a blank line) for paragraph-level ones.
func extractText(roots []*html.Node) string {
	var sb strings.Builder
	pending := 0 // newlines owed before the next text
	brk := func(n int) {
		if n > pending {
			pending = n
		}
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if isDropped(n) {
			return
		}
		switch n.Type {
		case html.TextNode:
			if strings.TrimSpace(strings.ReplaceAll(n.Data, "\u00a0", " ")) == "" {
				if n.Data != "" && sb.Len() > 0 && pending == 0 {
					sb.WriteString(" ")
				}
				return
			}
			if sb.Len() > 0 {
				sb.WriteString(strings.Repeat("\n", pending))
			}
			pending = 0
			sb.WriteString(n.Data)
			return
		case html.ElementNode:
			level := 0
			if paragraphElements[n.DataAtom] {
				level = 2
			} else if lineElements[n.DataAtom] {
				level = 1
			}
			brk(level)
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			brk(level)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, r := range roots {
		brk(2)
		walk(r)
	}
	return normalizeText(sb.String())
}

// normalizeText collapses whitespace within lines (non-breaking spaces
// included) and runs of blank lines to a single blank line.
func normalizeText(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = collapseSpace(line)
		if line == "" {
			blank = len(out) > 0
			continue
		}
		if blank {
			out = append(out, "")
			blank = false
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, " ", " ")), " ")
}
