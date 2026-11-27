package steps

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Private-use markers that carry structure through cleanDocument's text
// extraction: a real <hN> element and the slot of a <pre> block.
const (
	readmeHeadingMark = ""
	readmePreOpen     = ""
	readmePreClose    = ""
)

var (
	readmeATXLikeRe    = regexp.MustCompile(`^( {0,3})(#{1,6})(\s|$)`)
	readmeSetextLikeRe = regexp.MustCompile(`^ {0,3}(?:=+|-+)\s*$`)
	readmeFenceLikeRe  = regexp.MustCompile("^ {0,3}(?:`{3,}|~{3,})")
	readmePreSlotRe    = regexp.MustCompile(`^` + readmePreOpen + `(\d+)` + readmePreClose + `$`)
)

// htmlBlockEdits replaces an HTML block by its text. A list or quote marker
// before the block stays when the block still yields text, which then
// follows the marker on the same line. It goes, with the line, when the
// block yields none; an empty replacement never takes the line's newline.
func htmlBlockEdits(v *ast.HTMLBlock, b []byte, pulled map[ast.Node]bool) []textEdit {
	start, stop, raw := htmlBlockRange(v, b)
	if start < 0 {
		return nil
	}
	body, ok := htmlBlockText(raw)
	if !ok {
		return nil // keep the block as written
	}
	if body != "" {
		return []textEdit{htmlTextEdit(body, b, start, stop, pulled[v])}
	}
	return emptyHTMLBlockEdits(v, b, start, stop, pulled)
}

// htmlBlockRange is the byte range of an HTML block and its source text,
// without the prefixes of the containers it sits in. start is -1 for a
// block without lines.
func htmlBlockRange(v *ast.HTMLBlock, b []byte) (start, stop int, raw string) {
	start = -1
	var sb strings.Builder
	lines := v.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		if start < 0 {
			start = seg.Start
		}
		stop = seg.Stop
		sb.Write(seg.Value(b))
	}
	if v.HasClosure() {
		cl := v.ClosureLine
		if start < 0 {
			start = cl.Start
		}
		if cl.Stop > stop {
			stop = cl.Stop
		}
		sb.Write(cl.Value(b))
	}
	return start, stop, sb.String()
}

// htmlTextEdit replaces a block that yields body. Text after a list or
// quote marker, or nested by indentation, keeps that prefix on each line.
func htmlTextEdit(body string, b []byte, start, stop int, pulled bool) textEdit {
	prefix := b[lineStart(b, start):start]
	isList := listMarkerRe.Match(prefix) || pulled
	isQuote := quoteMarkerRe.Match(prefix)
	indented := len(prefix) > 0 && !isList && !isQuote && len(bytes.TrimLeft(prefix, " \t")) == 0
	if !isList && !isQuote && !indented {
		return textEdit{start, stop, "\n" + body + "\n\n"}
	}
	cont := strings.Repeat(" ", len(prefix))
	if isQuote {
		cont = string(prefix)
	}
	rows := strings.Split(body, "\n")
	for i := range rows {
		switch {
		case rows[i] == "":
			rows[i] = strings.TrimRight(cont, " \t")
		case i > 0 || indented:
			rows[i] = cont + rows[i]
		}
	}
	if indented {
		// a block nested by indentation, apart from what precedes it
		return textEdit{start, stop, "\n" + strings.Join(rows, "\n") + "\n\n"}
	}
	return textEdit{start, stop, strings.Join(rows, "\n") + "\n"}
}

// emptyHTMLBlockEdits removes a block that yields no text, with a bare
// marker before it; the newline of its line stays.
func emptyHTMLBlockEdits(v *ast.HTMLBlock, b []byte, start, stop int, pulled map[ast.Node]bool) []textEdit {
	from, removed := start, false
	if ls, ok := markerLineStart(v, b, start); ok {
		from, removed = ls, true
	}
	end := stop
	for end > from && (b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	edits := []textEdit{{from, end, ""}}
	if !removed {
		edits = append(edits, listItemTail(v, b, start, stop, pulled)...)
	}
	return edits
}

// htmlBlockText is the Markdown text of an HTML block: its text content,
// <hN> elements as "#" headings, <pre> as fenced code. Every other line
// that Markdown would read as a heading is escaped to stay literal.
func htmlBlockText(raw string) (string, bool) {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return "", false
	}
	headingsToMarkdown(doc)
	pres := preToSlots(doc)
	lines := strings.Split(cleanDocument(doc), "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, readmeHeadingMark):
			level, title, _ := strings.Cut(strings.TrimPrefix(ln, readmeHeadingMark), " ")
			n, _ := strconv.Atoi(level)
			out = append(out, strings.Repeat("#", n)+" "+escapeTagOpen(title))
		case readmePreSlotRe.MatchString(ln):
			m := readmePreSlotRe.FindStringSubmatch(ln)
			if i, _ := strconv.Atoi(m[1]); i < len(pres) && pres[i] != "" {
				out = append(out, "", fenceCode(pres[i]), "")
			}
		default:
			out = append(out, escapeLiteralLine(ln))
		}
	}
	return stripReadmeMarkers(strings.TrimSpace(strings.Join(out, "\n"))), true
}

func isReadmeMarker(r rune) bool { return r >= '' && r <= '' }

// stripReadmeMarkers removes the private-use marker runes, so they can
// never reach stored text.
func stripReadmeMarkers(s string) string {
	return strings.Map(func(r rune) rune {
		if isReadmeMarker(r) {
			return -1
		}
		return r
	}, s)
}

var readmeTagOpenRe = regexp.MustCompile(`\\*<[A-Za-z/!?]`)

// escapeLiteralLine escapes a line of text that came out of HTML so that
// Markdown reads it as text: a heading, setext underline or fence opener
// gets a backslash, and a "<" that could open an HTML tag is escaped.
func escapeLiteralLine(ln string) string {
	switch {
	case readmeATXLikeRe.MatchString(ln):
		ln = readmeATXLikeRe.ReplaceAllString(ln, `$1\$2$3`)
	case readmeSetextLikeRe.MatchString(ln), readmeFenceLikeRe.MatchString(ln):
		ln = `\` + strings.TrimLeft(ln, " ")
	}
	return escapeTagOpen(ln)
}

func escapeTagOpen(s string) string {
	return readmeTagOpenRe.ReplaceAllStringFunc(s, func(m string) string {
		// a backslash already in front escapes the "<" when their count is odd
		run := len(m) - len(strings.TrimLeft(m, `\`))
		if run%2 == 1 {
			return m
		}
		return m[:run] + `\` + m[run:]
	})
}

// fenceCode wraps code in a backtick fence longer than any backtick run in it.
func fenceCode(code string) string {
	fence := "```"
	for strings.Contains(code, fence) {
		fence += "`"
	}
	return fence + "\n" + code + "\n" + fence
}

// preToSlots replaces each <pre> by a paragraph holding a slot marker and
// returns the code of each, with entities decoded and lines intact.
func preToSlots(doc *html.Node) []string {
	var pres []*html.Node
	var find func(n *html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Pre {
			pres = append(pres, n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	codes := make([]string, len(pres))
	for i, p := range pres {
		var sb strings.Builder
		var walk func(n *html.Node)
		walk = func(n *html.Node) {
			switch {
			case n.Type == html.TextNode:
				sb.WriteString(n.Data)
			case n.Type == html.ElementNode && n.DataAtom == atom.Br:
				sb.WriteString("\n")
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(p)
		codes[i] = strings.TrimRight(sb.String(), "\r\n")
		for p.FirstChild != nil {
			p.RemoveChild(p.FirstChild)
		}
		p.DataAtom, p.Data = atom.P, "p"
		p.AppendChild(&html.Node{Type: html.TextNode, Data: readmePreOpen + strconv.Itoa(i) + readmePreClose})
	}
	return codes
}

// headingsToMarkdown rewrites each <h1>..<h6> as a paragraph holding the
// heading marker, its level and its text.
func headingsToMarkdown(doc *html.Node) {
	levels := map[atom.Atom]int{atom.H1: 1, atom.H2: 2, atom.H3: 3, atom.H4: 4, atom.H5: 5, atom.H6: 6}
	var heads []*html.Node
	var find func(n *html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && levels[n.DataAtom] > 0 {
			heads = append(heads, n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	for _, h := range heads {
		level := levels[h.DataAtom]
		title := strings.Join(strings.Fields(stripReadmeMarkers(extractText([]*html.Node{h}, true))), " ")
		for h.FirstChild != nil {
			h.RemoveChild(h.FirstChild)
		}
		h.DataAtom, h.Data = atom.P, "p"
		if title != "" {
			h.AppendChild(&html.Node{Type: html.TextNode, Data: readmeHeadingMark + strconv.Itoa(level) + " " + title})
		}
	}
}
