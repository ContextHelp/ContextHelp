package steps

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
)

var (
	listMarkerRe  = regexp.MustCompile(`^[ \t]*(?:[-*+]|\d{1,9}[.)])[ \t]+$`)
	quoteMarkerRe = regexp.MustCompile(`^[ \t]*(?:>[ \t]?)+$`)
	// bareMarkerRe matches a line holding nothing but a list marker.
	bareMarkerRe = regexp.MustCompile(`^[ \t]*(?:[-*+]|\d{1,9}[.)])[ \t]*$`)
)

func lineStart(b []byte, off int) int {
	return bytes.LastIndexByte(b[:off], '\n') + 1
}

// markerLineStart returns the start of the line holding node n's first
// byte (off) when the text before it is only a list or blockquote marker,
// and the whole line can go without changing the structure around it: the
// list item holds only n, or n opens its blockquote.
func markerLineStart(n ast.Node, b []byte, off int) (int, bool) {
	ls := lineStart(b, off)
	prefix := b[ls:off]
	switch {
	case listMarkerRe.Match(prefix):
		if li, ok := n.Parent().(*ast.ListItem); ok && li.ChildCount() == 1 {
			return ls, true
		}
	case quoteMarkerRe.Match(prefix):
		if _, ok := n.Parent().(*ast.Blockquote); ok && n.PreviousSibling() == nil {
			return ls, true
		}
	}
	return 0, false
}

// listItemTail handles a list item whose first child n lost all its text
// but has more children. The next child's first line moves up onto the
// marker line (keeping its later lines' indentation), so the item is never
// left holding a bare marker. An indented code block cannot start there
// without shifting the item's indentation, so it becomes a fenced block
// with the same body. off is where n starts, end where it ends; a child
// pulled up that is an HTML block is recorded in pulled, so its own edit
// writes its text after the marker.
func listItemTail(n ast.Node, b []byte, off, end int, pulled map[ast.Node]bool) []textEdit {
	next := n.NextSibling()
	if next == nil || !listMarkerRe.Match(b[lineStart(b, off):off]) {
		return nil
	}
	if _, ok := n.Parent().(*ast.ListItem); !ok {
		return nil
	}
	eol := end // the newline that ends n's last line
	if eol > 0 && b[eol-1] == '\n' {
		eol--
	} else if i := bytes.IndexByte(b[eol:], '\n'); i >= 0 {
		eol += i
	} else {
		return nil
	}
	q := eol + 1 // start of the next non-blank line
	for q < len(b) {
		line := b[q:]
		i := bytes.IndexByte(line, '\n')
		if i >= 0 {
			line = line[:i]
		}
		if len(bytes.TrimSpace(line)) != 0 {
			break
		}
		if i < 0 {
			q = len(b)
			break
		}
		q += i + 1
	}
	if q >= len(b) {
		return nil
	}
	if cb, ok := next.(*ast.CodeBlock); ok {
		if e, ok := indentedCodeToFence(cb, b, eol); ok {
			return []textEdit{e}
		}
		return nil
	}
	cs := q
	for cs < len(b) && (b[cs] == ' ' || b[cs] == '\t') {
		cs++
	}
	if _, ok := next.(*ast.HTMLBlock); ok {
		pulled[next] = true
	}
	return []textEdit{{eol, cs, ""}}
}

// indentedCodeToFence rewrites the indented code block cb, placed after the
// newline at eol, as a fenced block on the marker line. The body is kept
// byte for byte; each line carries the item's indentation.
func indentedCodeToFence(cb *ast.CodeBlock, b []byte, eol int) (textEdit, bool) {
	l := cb.Lines()
	if l.Len() == 0 {
		return textEdit{}, false
	}
	first := l.At(0)
	// indentation is measured in bytes: a tab before the content breaks it
	if bytes.ContainsRune(b[lineStart(b, eol):eol], '\t') || bytes.ContainsRune(b[lineStart(b, first.Start):first.Start], '\t') {
		return textEdit{}, false
	}
	indent := first.Start - lineStart(b, first.Start) - 4 // the item's content offset
	if indent < 0 {
		return textEdit{}, false
	}
	var body strings.Builder
	for i := 0; i < l.Len(); i++ {
		seg := l.At(i)
		body.Write(seg.Value(b))
	}
	code := strings.TrimRight(body.String(), "\n")
	fence := "```"
	for strings.Contains(code, fence) {
		fence += "`"
	}
	pad := strings.Repeat(" ", indent)
	rows := strings.Split(code, "\n")
	for i, r := range rows {
		if r != "" {
			rows[i] = pad + r
		}
	}
	repl := fence + "\n" + strings.Join(rows, "\n") + "\n" + pad + fence
	stop := l.At(l.Len() - 1).Stop
	if stop > 0 && b[stop-1] == '\n' {
		repl += "\n"
	}
	return textEdit{eol, stop, repl}, true
}
