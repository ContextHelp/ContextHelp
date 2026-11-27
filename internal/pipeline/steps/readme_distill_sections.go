package steps

import (
	"sort"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
)

// readmeNoiseHeadings are normalised heading texts (see normalizeHeading)
// whose section carries no knowledge about the project. "support <repo
// name>" is noise too; the name comes from the caller.
var readmeNoiseHeadings = map[string]bool{
	"sponsors": true, "sponsor": true, "backers": true, "supporters": true,
	"community": true, "license": true, "contributing": true,
	"contributors": true, "star history": true, "stargazers": true,
	"support": true, "support us": true, "support the project": true,
	"support this project": true, "support development": true,
	"acknowledgements": true, "acknowledgments": true, "code of conduct": true,
}

// normalizeHeading lowercases s and turns everything but letters and digits
// (punctuation, emoji, markup) into single spaces.
func normalizeHeading(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(unicode.ToLower(r))
		} else {
			sb.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

type readmeHeading struct {
	level      int
	start, end int // line range of the heading itself, end exclusive
	title      string
}

type readmeRefDef struct {
	first, last int // line range
	label       string
}

// restructureREADME drops noise and empty sections and unused link
// reference definitions, and collapses blank runs, working on whole lines.
// Lines of code blocks are never altered. The first heading of the
// document, when level 1, is the title and never noise.
func restructureREADME(src, repoName string) string {
	sc := scanREADME(src)
	deleted := make([]bool, len(sc.lines))
	sc.dropNoise(deleted, repoName)
	isDef := settleRefDefs(sc.defs, sc.lines, sc.protected, deleted)
	sc.dropEmpty(deleted, isDef)
	return emitREADMELines(sc.lines, sc.protected, deleted)
}

// readmeScan is the line-level structure of a README.
type readmeScan struct {
	lines     []string
	starts    []int  // byte offset of each line
	protected []bool // lines of code blocks
	heads     []readmeHeading
	ends      []int // ends[i]: first heading after i at the same or a higher level
	defs      []readmeRefDef
}

func scanREADME(src string) *readmeScan {
	b := []byte(src)
	sc := &readmeScan{lines: strings.Split(src, "\n")}
	sc.starts = make([]int, len(sc.lines))
	for i, off := 0, 0; i < len(sc.lines); i++ {
		sc.starts[i] = off
		off += len(sc.lines[i]) + 1
	}
	sc.protected = make([]bool, len(sc.lines))
	doc := parseMarkdown(b)
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			sc.visit(n, doc, b)
		}
		return ast.WalkContinue, nil
	})
	sc.ends = make([]int, len(sc.heads))
	for i := range sc.heads {
		sc.ends[i] = len(sc.heads)
		for j := i + 1; j < len(sc.heads); j++ {
			if sc.heads[j].level <= sc.heads[i].level {
				sc.ends[i] = j
				break
			}
		}
	}
	return sc
}

func (sc *readmeScan) lineOf(off int) int {
	return sort.Search(len(sc.starts), func(i int) bool { return sc.starts[i] > off }) - 1
}

// bound is the first line of heading j, or the line count.
func (sc *readmeScan) bound(j int) int {
	if j < len(sc.heads) {
		return sc.heads[j].start
	}
	return len(sc.lines)
}

func markLines(flags []bool, from, to int) {
	for i := max(from, 0); i < to && i < len(flags); i++ {
		flags[i] = true
	}
}

func (sc *readmeScan) visit(n ast.Node, doc ast.Node, b []byte) {
	switch v := n.(type) {
	case *ast.FencedCodeBlock:
		sc.markFence(v)
	case *ast.CodeBlock:
		if l := v.Lines(); l.Len() > 0 {
			markLines(sc.protected, sc.lineOf(l.At(0).Start), sc.lineOf(l.At(l.Len()-1).Stop-1)+1)
		}
	case *ast.LinkReferenceDefinition:
		if l := v.Lines(); l.Len() > 0 {
			sc.defs = append(sc.defs, readmeRefDef{
				first: sc.lineOf(l.At(0).Start),
				last:  sc.lineOf(l.At(l.Len()-1).Stop - 1),
				label: normalizeRefLabel(string(v.Label)),
			})
		}
	case *ast.Heading:
		sc.addHeading(v, doc, b)
	}
}

// markFence protects a fenced block with its fence lines.
func (sc *readmeScan) markFence(v *ast.FencedCodeBlock) {
	l := v.Lines()
	if l.Len() == 0 {
		return
	}
	first, last := sc.lineOf(l.At(0).Start), sc.lineOf(l.At(l.Len()-1).Stop-1)
	if last+1 < len(sc.lines) && isFenceLine(sc.lines[last+1]) {
		last++
	}
	markLines(sc.protected, first-1, last+1)
}

// addHeading records a top-level heading.
func (sc *readmeScan) addHeading(v *ast.Heading, doc ast.Node, b []byte) {
	l := v.Lines()
	if v.Parent() != doc || l.Len() == 0 {
		return
	}
	li := sc.lineOf(l.At(0).Start)
	end := li + 1
	if !readmeATXLikeRe.MatchString(sc.lines[li]) {
		end = li + l.Len() + 1 // setext: text lines and underline
	}
	sc.heads = append(sc.heads, readmeHeading{v.Level, li, end, headingText(v, b)})
}

// dropNoise marks the sections of noise headings as deleted.
func (sc *readmeScan) dropNoise(deleted []bool, repoName string) {
	repoNorm := normalizeHeading(repoName)
	for i, h := range sc.heads {
		if i == 0 && h.level == 1 {
			continue
		}
		norm := normalizeHeading(h.title)
		if readmeNoiseHeadings[norm] || (repoNorm != "" && norm == "support "+repoNorm) {
			markLines(deleted, h.start, sc.bound(sc.ends[i]))
		}
	}
}

// dropEmpty marks sections with no content of their own and no non-empty
// subsection as deleted. Referenced definitions in such a section stay;
// only the heading and blank lines go.
func (sc *readmeScan) dropEmpty(deleted, isDef []bool) {
	own := make([]bool, len(sc.heads))
	for i, h := range sc.heads {
		if !deleted[h.start] {
			own[i] = sc.hasOwnContent(h, sc.bound(i+1), deleted, isDef)
		}
	}
	for i, h := range sc.heads {
		if deleted[h.start] || anyTrue(own[i:sc.ends[i]]) {
			continue
		}
		markLines(deleted, h.start, h.end)
		for l := h.end; l < sc.bound(i+1); l++ {
			if !isDef[l] {
				deleted[l] = true
			}
		}
	}
}

func (sc *readmeScan) hasOwnContent(h readmeHeading, bound int, deleted, isDef []bool) bool {
	for l := h.end; l < bound; l++ {
		if !deleted[l] && !isDef[l] && strings.TrimSpace(sc.lines[l]) != "" {
			return true
		}
	}
	return false
}

func anyTrue(flags []bool) bool {
	for _, f := range flags {
		if f {
			return true
		}
	}
	return false
}

// emitREADMELines joins the kept lines: trailing space trimmed outside
// code, blank runs collapsed, and a blank line before a bare list marker
// that would otherwise read as a setext underline.
func emitREADMELines(lines []string, protected, deleted []bool) string {
	var out []string
	pendingBlank := false
	for i, ln := range lines {
		if deleted[i] {
			continue
		}
		if !protected[i] {
			if ln = strings.TrimRight(ln, " \t\r"); ln == "" {
				pendingBlank = true
				continue
			}
		}
		if pendingBlank && len(out) > 0 {
			out = append(out, "")
		} else if !protected[i] && bareMarkerRe.MatchString(ln) && len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
		pendingBlank = false
		out = append(out, ln)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

func normalizeRefLabel(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// settleRefDefs keeps a link reference definition when the prose that
// survives mentions its label as "[label]", wherever the definition sits
// (even inside a dropped section), and deletes it otherwise. It returns the
// definition lines.
func settleRefDefs(defs []readmeRefDef, lines []string, protected, deleted []bool) (isDef []bool) {
	isDef = make([]bool, len(lines))
	for _, d := range defs {
		for i := max(d.first, 0); i <= d.last && i < len(lines); i++ {
			isDef[i] = true
		}
	}
	var rest strings.Builder
	for i, ln := range lines {
		if !deleted[i] && !protected[i] && !isDef[i] {
			rest.WriteString(ln + " ")
		}
	}
	prose := normalizeRefLabel(rest.String())
	for _, d := range defs {
		used := d.label != "" && strings.Contains(prose, "["+d.label+"]")
		for i := max(d.first, 0); i <= d.last && i < len(lines); i++ {
			deleted[i] = !used
		}
	}
	return isDef
}

func isFenceLine(l string) bool {
	l = strings.TrimSpace(l)
	return strings.HasPrefix(l, "```") || strings.HasPrefix(l, "~~~")
}

// headingText is the text of a heading, the lines of a setext heading
// joined by a space.
func headingText(h *ast.Heading, b []byte) string {
	var sb strings.Builder
	_ = ast.Walk(h, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if t, ok := n.(*ast.Text); ok && entering {
			sb.Write(t.Segment.Value(b))
			if t.SoftLineBreak() || t.HardLineBreak() {
				sb.WriteByte(' ')
			}
		}
		return ast.WalkContinue, nil
	})
	return sb.String()
}
