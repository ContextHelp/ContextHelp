package steps

import (
	"bytes"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const (
	readmeAlt  = `\[(?:[^\[\]]|\[[^\[\]]*\])*\]`
	readmeDest = `\((?:[^()]|\([^()]*\))*\)`
	readmeRef  = `\[[^\[\]]*\]`
	readmeImg  = `!` + readmeAlt + `(?:` + readmeDest + `|` + readmeRef + `)`
)

var (
	// readmeImageRe matches, in order: a link whose only content is images
	// (a badge), an image with a destination or reference, and a shortcut
	// reference image (group 1 is its label). goldmark gives inline nodes no
	// source range, so this runs only on the text of blocks goldmark found
	// an image in, never across code spans.
	readmeImageRe = regexp.MustCompile(`\[\s*(?:` + readmeImg + `\s*)+\](?:` + readmeDest + `|` + readmeRef + `)|` + readmeImg + `|!(` + readmeAlt + `)`)
	readmeBrRe    = regexp.MustCompile(`(?i)^<br\s*/?>$`)
)

// distillREADME reduces a README to the text that describes the project:
// HTML becomes its text, images and badges go, noise sections (sponsors,
// license, contributors, ...) and sections left empty go, and blank runs
// collapse. Code blocks and code spans are kept byte for byte. repoName is
// the repository name, for the "support <repo>" noise heading. It never
// fails; on a problem it returns src.
func distillREADME(src, repoName string) string {
	return guardDistill(src, func() string {
		if strings.TrimSpace(src) == "" {
			return ""
		}
		// The markers carry structure through the HTML text pass; input
		// that already holds them must not be able to forge it.
		clean := strings.Map(func(r rune) rune {
			if isReadmeMarker(r) {
				return '�'
			}
			return r
		}, src)
		return stripReadmeMarkers(restructureREADME(stripREADMEInline(clean), repoName))
	})
}

// guardDistill runs f and returns src when f panics. The log line carries
// the panic value only, never README text.
func guardDistill(src string, f func() string) (out string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("readme distill failed, keeping README as fetched", "panic", fmt.Sprint(r))
			out = src
		}
	}()
	return f()
}

type textEdit struct {
	start, stop int
	repl        string
}

func parseMarkdown(b []byte) ast.Node {
	return goldmark.DefaultParser().Parse(text.NewReader(b))
}

func applyEdits(src string, edits []textEdit) string {
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var sb strings.Builder
	pos := 0
	for _, e := range edits {
		if e.start < pos || e.stop < e.start || e.stop > len(src) {
			continue
		}
		sb.WriteString(src[pos:e.start])
		sb.WriteString(e.repl)
		pos = e.stop
	}
	sb.WriteString(src[pos:])
	return sb.String()
}

// stripREADMEInline replaces HTML blocks and inline HTML by their text and
// removes images and badges.
func stripREADMEInline(src string) string {
	b := []byte(src)
	doc := parseMarkdown(b)
	defined := map[string]bool{}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if d, ok := n.(*ast.LinkReferenceDefinition); ok && entering {
			defined[normalizeRefLabel(string(d.Label))] = true
		}
		return ast.WalkContinue, nil
	})
	var edits []textEdit
	pulled := map[ast.Node]bool{}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.HTMLBlock:
			edits = append(edits, htmlBlockEdits(v, b, pulled)...)
		case *ast.RawHTML:
			edits = append(edits, rawHTMLEdits(v, b)...)
		case *ast.Paragraph, *ast.TextBlock, *ast.Heading:
			edits = append(edits, imageEdits(n, b, defined, pulled)...)
		}
		return ast.WalkContinue, nil
	})
	return applyEdits(src, edits)
}

func rawHTMLEdits(v *ast.RawHTML, b []byte) []textEdit {
	edits := make([]textEdit, 0, v.Segments.Len())
	for i := 0; i < v.Segments.Len(); i++ {
		seg := v.Segments.At(i)
		repl, stop := "", seg.Stop
		if readmeBrRe.Match(seg.Value(b)) {
			repl = "\n"
			if inHeading(v) || inTableRow(b, seg.Start) {
				repl = " "
			} else if rest := restOfLine(b, seg.Stop); readmeATXLikeRe.Match(rest) || readmeSetextLikeRe.Match(rest) || readmeFenceLikeRe.Match(rest) {
				// the break would start a heading or fence: keep the text literal
				repl = "\n\\"
				stop += len(rest) - len(bytes.TrimLeft(rest, " "))
			}
		}
		edits = append(edits, textEdit{seg.Start, stop, repl})
	}
	return edits
}

func restOfLine(b []byte, off int) []byte {
	rest := b[off:]
	if i := bytes.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func inHeading(n ast.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if _, ok := p.(*ast.Heading); ok {
			return true
		}
	}
	return false
}

// inTableRow reports whether the line holding offset is a pipe-table row.
func inTableRow(b []byte, off int) bool {
	ls := strings.LastIndexByte(string(b[:off]), '\n') + 1
	return strings.HasPrefix(strings.TrimLeft(string(b[ls:off]), " \t"), "|")
}

// imageEdits deletes the images and badges in a leaf block, outside code
// spans. When that empties a list item, the item's marker goes with it.
func imageEdits(n ast.Node, b []byte, defined map[string]bool, pulled map[ast.Node]bool) []textEdit {
	hasImage, spans := imageBlockInfo(n, b)
	lines := n.Lines()
	if !hasImage || lines.Len() == 0 {
		return nil
	}
	first, end := lines.At(0).Start, lines.At(lines.Len()-1).Stop
	var edits []textEdit
	pos := first
	for _, sp := range spans {
		edits = append(edits, imagesIn(b, pos, min(sp[0], end), defined)...)
		pos = max(pos, sp[1])
	}
	edits = append(edits, imagesIn(b, pos, end, defined)...)
	if len(edits) > 0 && emptied(b[first:end], edits, first) {
		if ls, ok := markerLineStart(n, b, first); ok {
			edits = append(edits, textEdit{ls, edits[0].start, ""})
		} else {
			edits = append(edits, listItemTail(n, b, first, end, pulled)...)
		}
	}
	return edits
}

// imageBlockInfo reports whether a leaf block holds an image and the byte
// ranges of its code spans.
func imageBlockInfo(n ast.Node, b []byte) (hasImage bool, spans [][2]int) {
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c.(type) {
		case *ast.Image:
			hasImage = true
		case *ast.CodeSpan:
			if lo, hi, ok := codeSpanRange(c, b); ok {
				spans = append(spans, [2]int{lo, hi})
			}
		}
		return ast.WalkContinue, nil
	})
	return hasImage, spans
}

// imagesIn returns the deletions for the images and badges in b[from:to].
func imagesIn(b []byte, from, to int, defined map[string]bool) []textEdit {
	if from >= to {
		return nil
	}
	var edits []textEdit
	for _, m := range readmeImageRe.FindAllSubmatchIndex(b[from:to], -1) {
		s, e := from+m[0], from+m[1]
		if escaped(b, s) {
			continue
		}
		if m[2] >= 0 && !defined[normalizeRefLabel(string(b[from+m[2]+1:from+m[3]-1]))] {
			continue // "![x]" naming no definition is not an image
		}
		if s == 0 || b[s-1] == ' ' || b[s-1] == '\n' {
			for e < len(b) && b[e] == ' ' {
				e++
			}
		}
		edits = append(edits, textEdit{s, e, ""})
	}
	return edits
}

// escaped reports whether the byte at off follows an odd run of backslashes.
func escaped(b []byte, off int) bool {
	n := 0
	for i := off - 1; i >= 0 && b[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// emptied reports whether block is blank once edits (offsets relative to
// base) are removed.
func emptied(block []byte, edits []textEdit, base int) bool {
	var rest []byte
	pos := 0
	for _, e := range edits {
		rest = append(rest, block[pos:e.start-base]...)
		pos = e.stop - base
	}
	rest = append(rest, block[min(pos, len(block)):]...)
	return strings.TrimSpace(string(rest)) == ""
}

// codeSpanRange is the byte range of a code span including its backticks.
func codeSpanRange(n ast.Node, b []byte) (lo, hi int, ok bool) {
	lo = -1
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		t, isText := c.(*ast.Text)
		if !isText {
			continue
		}
		if lo < 0 || t.Segment.Start < lo {
			lo = t.Segment.Start
		}
		if t.Segment.Stop > hi {
			hi = t.Segment.Stop
		}
	}
	if lo < 0 {
		return 0, 0, false
	}
	for lo > 0 && b[lo-1] == ' ' {
		lo--
	}
	for lo > 0 && b[lo-1] == '`' {
		lo--
	}
	for hi < len(b) && b[hi] == ' ' {
		hi++
	}
	for hi < len(b) && b[hi] == '`' {
		hi++
	}
	return lo, hi, true
}
