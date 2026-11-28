package steps

import (
	"context"
	"log/slog"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// HTMLCleaner turns an HTML document in RawContent into readable text. It
// parses the document, drops page chrome (head, scripts, styles, navigation,
// page-level headers and footers, sidebars, form controls, hidden elements
// and elements with a landmark role of banner, navigation, contentinfo or
// complementary), prefers the <main> (or role="main") subtree when the page
// has one, else the outermost <article> subtrees, and records the <title> as
// Metadata["title"] instead of body text. Non-HTML input passes through.
//
// Chrome removal is structural only: there are no class or id heuristics,
// so a cookie notice that sits in the body outside every landmark survives
// unless a <main> or <article> excludes it.
type HTMLCleaner struct {
	pipeline.BaseContract
}

func NewHTMLCleaner() *HTMLCleaner {
	return &HTMLCleaner{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"RawContent", "Metadata"},
			// 2: parses the page and keeps its main text; cookie and
			// navigation text outside the main content is dropped when the
			// page marks its main content (main, article). The title goes
			// to metadata.
			Version: 2,
		}),
	}
}

func (s *HTMLCleaner) Name() string { return "html_cleaner" }

// droppedElements are removed with their whole subtree.
var droppedElements = map[atom.Atom]bool{
	atom.Head: true, atom.Script: true, atom.Style: true, atom.Noscript: true,
	atom.Template: true, atom.Nav: true, atom.Iframe: true, atom.Svg: true,
	// Form controls carry no prose; the <form> itself stays, because some
	// sites wrap the whole page in one.
	atom.Input: true, atom.Select: true, atom.Textarea: true, atom.Button: true,
	atom.Option: true, atom.Optgroup: true, atom.Datalist: true,
}

// minimalDropped is the safety-net set: only elements that never hold prose.
var minimalDropped = map[atom.Atom]bool{
	atom.Head: true, atom.Script: true, atom.Style: true,
	atom.Template: true, atom.Noscript: true,
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
	atom.Header: true, atom.Footer: true, atom.Aside: true, atom.Form: true,
}

const (
	// fallbackMinLen and fallbackRatio trigger the minimal-drop retry when
	// no <main>/<article> was found and the cleaned text is under 5% of what
	// the minimal pass keeps: the page's prose lives in elements we dropped.
	fallbackMinLen = 1000
	fallbackRatio  = 20
)

func (s *HTMLCleaner) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if !strings.Contains(strings.ToLower(draft.ContentType), "html") && !strings.HasPrefix(strings.TrimSpace(draft.RawContent), "<!") {
		return draft, nil
	}

	var title, text string
	doc, err := html.Parse(strings.NewReader(draft.RawContent))
	if err != nil {
		// html.Parse rejects documents nested more than 512 elements deep.
		// Malformed input must never fail the job, so fall back to the
		// tokenizer, which has no depth limit.
		slog.Warn("html_cleaner: parse failed; using tokenizer text pass", "source", draft.Source, "err", err)
		title, text = tokenizeText(draft.RawContent)
	} else {
		title = findTitle(doc)
		text = cleanDocument(doc)
	}

	if title = collapseSpace(title); title != "" {
		if draft.Metadata == nil {
			draft.Metadata = make(map[string]any)
		}
		if existing, _ := draft.Metadata["title"].(string); strings.TrimSpace(existing) == "" {
			draft.Metadata["title"] = title
		}
	}

	draft.RawContent = text
	return draft, nil
}

// cleanDocument extracts the readable text of a parsed document: from the
// content roots when there are any, else from the whole body. An empty
// result, or one far shorter than the minimal pass keeps when there was no
// content root, is retried with the minimal drop set.
func cleanDocument(doc *html.Node) string {
	roots := contentRoots(doc)
	text := ""
	if len(roots) > 0 {
		text = extractText(roots, false)
	}
	if text == "" {
		text = extractText([]*html.Node{doc}, false)
	}
	if text == "" || len(roots) == 0 {
		minimal := extractText([]*html.Node{doc}, true)
		if text == "" || (len(minimal) >= fallbackMinLen && len(text)*fallbackRatio < len(minimal)) {
			return minimal
		}
	}
	return text
}

// walkCtx is where a node sits: inside a sectioning element (article, aside,
// main, nav, section) its header and footer scope to that section, not the
// page, and an aside inside an article is part of the article.
type walkCtx struct {
	sectioned bool
	article   bool
}

func (c walkCtx) into(n *html.Node) walkCtx {
	if n.Type != html.ElementNode {
		return c
	}
	switch n.DataAtom {
	case atom.Article:
		c.article, c.sectioned = true, true
	case atom.Aside, atom.Main, atom.Nav, atom.Section:
		c.sectioned = true
	}
	if hasMainRole(n) {
		c.sectioned = true
	}
	return c
}

// isDropped reports whether n is removed with its subtree.
func isDropped(n *html.Node, c walkCtx, minimal bool) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if minimal {
		return minimalDropped[n.DataAtom]
	}
	switch {
	case droppedElements[n.DataAtom]:
		return true
	case n.DataAtom == atom.Header || n.DataAtom == atom.Footer:
		if !c.sectioned {
			return true
		}
	case n.DataAtom == atom.Aside:
		if !c.article {
			return true
		}
	}
	for _, a := range n.Attr {
		switch a.Key {
		case "hidden":
			return true
		case "aria-hidden":
			if strings.EqualFold(strings.TrimSpace(a.Val), "true") {
				return true
			}
		case "role":
			for _, role := range strings.Fields(strings.ToLower(a.Val)) {
				if droppedRoles[role] {
					return true
				}
			}
		}
	}
	return false
}

func hasMainRole(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Key == "role" {
			for _, role := range strings.Fields(strings.ToLower(a.Val)) {
				if role == "main" {
					return true
				}
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

// contentRoots returns the <main> (or role="main") elements if any, else the
// outermost <article> elements in document order, skipping dropped subtrees.
func contentRoots(doc *html.Node) []*html.Node {
	var mains, articles []*html.Node
	var walk func(n *html.Node, c walkCtx)
	walk = func(n *html.Node, c walkCtx) {
		if isDropped(n, c, false) {
			return
		}
		if n.Type == html.ElementNode {
			if n.DataAtom == atom.Main || hasMainRole(n) {
				mains = append(mains, n)
				return
			}
			if n.DataAtom == atom.Article && !c.article {
				articles = append(articles, n)
			}
		}
		c = c.into(n)
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch, c)
		}
	}
	walk(doc, walkCtx{})
	if len(mains) > 0 {
		return mains
	}
	return articles
}

// extractText renders the text of roots with block boundaries as newlines:
// one for line-level blocks, two (a blank line) for paragraph-level ones,
// and a space between table cells.
func extractText(roots []*html.Node, minimal bool) string {
	var sb strings.Builder
	pending := 0 // newlines owed before the next text
	space := false
	brk := func(n int) {
		if n > pending {
			pending = n
		}
	}
	var walk func(n *html.Node, c walkCtx)
	walk = func(n *html.Node, c walkCtx) {
		if isDropped(n, c, minimal) {
			return
		}
		switch n.Type {
		case html.TextNode:
			if strings.TrimSpace(strings.ReplaceAll(n.Data, " ", " ")) == "" {
				if n.Data != "" && sb.Len() > 0 && pending == 0 {
					sb.WriteString(" ")
				}
				return
			}
			if sb.Len() > 0 {
				if pending > 0 {
					sb.WriteString(strings.Repeat("\n", pending))
				} else if space {
					sb.WriteString(" ")
				}
			}
			pending, space = 0, false
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
			c = c.into(n)
			for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
				walk(ch, c)
			}
			brk(level)
			if n.DataAtom == atom.Td || n.DataAtom == atom.Th {
				space = true
			}
			return
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch, c)
		}
	}
	for _, r := range roots {
		brk(2)
		c := walkCtx{}
		if r.Type == html.ElementNode {
			c = walkCtx{sectioned: true, article: r.DataAtom == atom.Article}
		}
		walk(r, c)
	}
	return normalizeText(sb.String())
}

// tokenizeSkipTags are the elements whose content tokenizeText drops.
var tokenizeSkipTags = map[string]bool{
	"head": true, "script": true, "style": true, "noscript": true,
	"template": true, "nav": true, "header": true, "footer": true,
	"aside": true, "iframe": true, "svg": true, "select": true,
	"textarea": true, "button": true, "datalist": true,
}

// tokenState is the running state of tokenizeText.
type tokenState struct {
	text, title strings.Builder
	pending     int    // newlines owed before the next text
	skip        string // element whose content is being dropped
	skipDepth   int
	inTitle     bool
	haveTitle   bool
}

// tokenizeText is the depth-unlimited fallback for documents the parser
// rejects. It keeps the text outside script/style/head/noscript/template,
// navigation, headers, footers, asides, iframes, svg and form controls, with
// the same line and paragraph breaks, and returns the <title> text. It does
// not apply roles, hidden attributes or the sectioning exceptions, nor does
// it prefer <main> or <article>.
func tokenizeText(src string) (title, text string) {
	var st tokenState
	z := html.NewTokenizer(strings.NewReader(src))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		tok := z.Token()
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			st.startTag(tok.Data, tt == html.StartTagToken)
		case html.EndTagToken:
			st.endTag(tok.Data)
		case html.TextToken:
			st.textToken(tok.Data)
		}
	}
	return st.title.String(), normalizeText(st.text.String())
}

func (st *tokenState) startTag(name string, open bool) {
	switch {
	case name == "title" && !st.haveTitle && (st.skip == "" || st.skip == "head"):
		st.inTitle = open
	case st.skip != "":
		if open && name == st.skip {
			st.skipDepth++
		}
	case open && tokenizeSkipTags[name]:
		st.skip, st.skipDepth = name, 1
	default:
		st.blockBreak(name)
	}
}

func (st *tokenState) endTag(name string) {
	switch {
	case name == "title" && st.inTitle:
		st.inTitle, st.haveTitle = false, true
	case st.skip != "":
		if name == st.skip {
			if st.skipDepth--; st.skipDepth == 0 {
				st.skip = ""
			}
		}
	default:
		st.blockBreak(name)
	}
}

// blockBreak owes the newlines a block element puts around its content.
func (st *tokenState) blockBreak(name string) {
	a := atom.Lookup([]byte(name))
	switch {
	case paragraphElements[a]:
		st.pending = max(st.pending, 2)
	case lineElements[a]:
		st.pending = max(st.pending, 1)
	}
}

func (st *tokenState) textToken(data string) {
	switch {
	case st.inTitle:
		st.title.WriteString(data)
	case st.skip != "":
	case strings.TrimSpace(strings.ReplaceAll(data, "\u00a0", " ")) == "":
		if data != "" && st.text.Len() > 0 && st.pending == 0 {
			st.text.WriteString(" ")
		}
	default:
		if st.text.Len() > 0 {
			st.text.WriteString(strings.Repeat("\n", st.pending))
		}
		st.pending = 0
		st.text.WriteString(data)
	}
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
