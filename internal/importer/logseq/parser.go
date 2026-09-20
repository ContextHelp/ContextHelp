// Package logseq implements the Logseq graph importer (P-110).
// It walks a local Logseq graph directory, parses journal and page markdown files,
// extracts block hierarchies, page references, and properties, and emits
// normalized Page records ready for enqueueing into dPKMS/ctxt.
package logseq

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// PageKind distinguishes Logseq page types.
type PageKind string

const (
	PageKindJournal PageKind = "journal"
	PageKindPage    PageKind = "page"
)

// Block represents a single Logseq block (bullet or paragraph).
type Block struct {
	// UUID is the block's stable identifier if present (property:: value syntax).
	UUID string
	// Content is the block text (without leading bullet marker).
	Content string
	// Level is the indentation depth (0 = top-level).
	Level int
	// Properties holds block-level key:: value pairs.
	Properties map[string]string
	// Children contains nested sub-blocks.
	Children []*Block
}

// Page is a normalized Logseq page ready for import.
type Page struct {
	// ExternalID is a stable dedup key derived from the graph-relative path.
	ExternalID string
	// Path is the absolute path to the source file.
	Path string
	// RelPath is the graph-relative path (forward slashes, no leading slash).
	RelPath string
	// Title is the page title derived from the filename or journal date.
	Title string
	// Kind is "journal" or "page".
	Kind PageKind
	// JournalDate is set for journal pages (YYYY-MM-DD parsed from filename).
	JournalDate string
	// Content is the raw markdown body.
	Content string
	// Properties holds top-level page properties (key:: value pairs in the first block).
	Properties map[string]string
	// Tags combines page-level [[tags]] and #tags.
	Tags []string
	// PageRefs lists every [[page reference]] in the content.
	PageRefs []string
	// Blocks is the parsed block tree.
	Blocks []*Block
	// ContentHash is the SHA-256 hex digest of the raw file bytes.
	ContentHash string
	// ModTime is the file's last-modified time.
	ModTime time.Time
}

// WalkGraph returns all Logseq pages found under graphDir (recursive).
// Both the pages/ and journals/ subdirectories are included.
// Symlinked directories are skipped to avoid duplicate traversal.
func WalkGraph(graphDir string) ([]Page, error) {
	graphDir = filepath.Clean(graphDir)
	info, err := os.Stat(graphDir)
	if err != nil {
		return nil, fmt.Errorf("logseq: stat graph dir %q: %w", graphDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("logseq: graph path %q is not a directory", graphDir)
	}

	var pages []Page
	err = filepath.WalkDir(graphDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// Skip Logseq config/cache folders.
		if d.IsDir() {
			name := d.Name()
			if name == ".logseq" || name == "logseq" || name == ".git" {
				return filepath.SkipDir
			}
			// Skip symlinked directories.
			fi, err := os.Lstat(path)
			if err == nil && fi.Mode()&os.ModeSymlink != 0 {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".md" && ext != ".markdown" {
			return nil
		}

		data, err := os.ReadFile(path) // #nosec G122 -- reading user's own graph; symlink TOCTOU not a threat
		if err != nil {
			return fmt.Errorf("logseq: read %q: %w", path, err)
		}

		fi, err := d.Info()
		if err != nil {
			return fmt.Errorf("logseq: stat %q: %w", path, err)
		}

		rel, err := filepath.Rel(graphDir, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		page, err := ParsePage(data, rel, path, fi.ModTime())
		if err != nil {
			return fmt.Errorf("logseq: parse %q: %w", path, err)
		}
		pages = append(pages, page)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pages, nil
}

// ParsePage parses a single Logseq markdown file into a Page.
func ParsePage(data []byte, relPath, absPath string, modTime time.Time) (Page, error) {
	raw := string(data)
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	kind, title, journalDate := classifyPage(relPath)
	blocks := parseBlocks(raw)
	topProps := extractPageProperties(blocks)
	pageRefs := extractPageRefs(raw)
	tags := extractPageTags(raw, topProps)
	extID := stableExternalID(relPath)

	// Override title from page properties if present.
	if t, ok := topProps["title"]; ok && strings.TrimSpace(t) != "" {
		title = strings.TrimSpace(t)
	}

	return Page{
		ExternalID:  extID,
		Path:        absPath,
		RelPath:     relPath,
		Title:       title,
		Kind:        kind,
		JournalDate: journalDate,
		Content:     raw,
		Properties:  topProps,
		Tags:        tags,
		PageRefs:    pageRefs,
		Blocks:      blocks,
		ContentHash: hash,
		ModTime:     modTime,
	}, nil
}

// RenderContent formats a Page into the text content submitted to the ctxt pipeline.
func RenderContent(page Page) string {
	var b strings.Builder

	b.WriteString("# ")
	b.WriteString(page.Title)
	b.WriteString("\n\n")

	b.WriteString("Provider: Logseq\n")
	b.WriteString("Kind: ")
	b.WriteString(string(page.Kind))
	b.WriteString("\n")
	if page.ExternalID != "" {
		b.WriteString("External ID: ")
		b.WriteString(page.ExternalID)
		b.WriteString("\n")
	}
	if page.RelPath != "" {
		b.WriteString("Graph Path: ")
		b.WriteString(page.RelPath)
		b.WriteString("\n")
	}
	if page.JournalDate != "" {
		b.WriteString("Journal Date: ")
		b.WriteString(page.JournalDate)
		b.WriteString("\n")
	}
	if !page.ModTime.IsZero() {
		b.WriteString("Modified: ")
		b.WriteString(page.ModTime.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	if len(page.Tags) > 0 {
		b.WriteString("Tags: ")
		b.WriteString(strings.Join(page.Tags, ", "))
		b.WriteString("\n")
	}
	if len(page.PageRefs) > 0 {
		b.WriteString("References: ")
		b.WriteString(strings.Join(page.PageRefs, ", "))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	body := strings.TrimSpace(renderBlocks(page.Blocks))
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.String()
}

// renderBlocks converts the block tree back into readable text.
func renderBlocks(blocks []*Block) string {
	var b strings.Builder
	for _, block := range blocks {
		renderBlock(&b, block, 0)
	}
	return b.String()
}

func renderBlock(b *strings.Builder, block *Block, depth int) {
	indent := strings.Repeat("  ", depth)
	content := strings.TrimSpace(block.Content)
	if content != "" {
		b.WriteString(indent)
		if depth > 0 {
			b.WriteString("- ")
		}
		b.WriteString(content)
		b.WriteString("\n")
	}
	for _, child := range block.Children {
		renderBlock(b, child, depth+1)
	}
}

// classifyPage returns the kind, title, and optional journal date for a relative path.
func classifyPage(relPath string) (PageKind, string, string) {
	parts := strings.Split(relPath, "/")
	isJournal := false
	for _, p := range parts[:len(parts)-1] {
		if strings.ToLower(p) == "journals" {
			isJournal = true
			break
		}
	}

	base := filepath.Base(relPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	// Logseq journal format: YYYY_MM_DD or YYYY-MM-DD.
	journalDate := parseJournalDate(stem)
	if journalDate != "" {
		isJournal = true
	}

	if isJournal {
		title := stem
		if journalDate != "" {
			title = journalDate
		}
		return PageKindJournal, title, journalDate
	}

	// Page: unescape Logseq URL-encoded filenames and replace underscores.
	title := decodeName(stem)
	return PageKindPage, title, ""
}

// parseJournalDate tries to parse common Logseq journal filename patterns.
func parseJournalDate(stem string) string {
	// Try YYYY_MM_DD and YYYY-MM-DD.
	normalized := strings.ReplaceAll(stem, "_", "-")
	layouts := []string{"2006-01-02", "Jan 2nd, 2006", "January 2nd, 2006"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, normalized); err == nil {
			return t.Format("2006-01-02")
		}
	}
	// Try yyyyMMdd pattern (8 digits).
	if len(normalized) == 8 {
		if _, err := time.Parse("20060102", normalized); err == nil {
			return normalized[:4] + "-" + normalized[4:6] + "-" + normalized[6:8]
		}
	}
	return ""
}

func decodeName(name string) string {
	// Logseq encodes some characters; replace %2F with / and underscores with spaces.
	name = strings.ReplaceAll(name, "%2F", "/")
	name = strings.ReplaceAll(name, "%2f", "/")
	return strings.ReplaceAll(name, "___", "/")
}

var (
	propLineRe  = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_-]*)::[ \t]*(.*)$`)
	bulletRe    = regexp.MustCompile(`^(\s*)-[ \t]+(.*)$`)
	pageRefRe   = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)
	inlineTagRe = regexp.MustCompile(`(?:^|[\s(])#([A-Za-z][A-Za-z0-9_/-]*)`)
)

// parseBlocks parses a Logseq markdown file into a flat list of top-level blocks
// with nested children reconstructed from indentation.
func parseBlocks(raw string) []*Block {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	var root []*Block
	// Stack tracks []*Block slices at each indent level.
	// stack[0] = root children slice pointer is tracked via root directly.
	type frame struct {
		level  int
		parent *Block
	}
	stack := []frame{{level: -1, parent: nil}}

	// currentBlock tracks the most recently created block for continuation lines.
	var currentBlock *Block

	for scanner.Scan() {
		line := scanner.Text()

		m := bulletRe.FindStringSubmatch(line)
		if m == nil {
			// Non-bullet continuation line: merge into the current block.
			if currentBlock != nil {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					// Check if this continuation line is a property.
					contProps := extractInlineProperties(trimmed)
					for k, v := range contProps {
						currentBlock.Properties[k] = v
					}
					// Append to content (properties will be stripped when rendering).
					currentBlock.Content += "\n" + trimmed
				}
			}
			continue
		}

		indent := len(m[1]) // number of leading spaces/tabs
		content := m[2]

		// Parse properties from content.
		props := extractInlineProperties(content)
		uuid := props["id"]

		block := &Block{
			UUID:       uuid,
			Content:    stripProperties(content),
			Level:      indent,
			Properties: props,
		}

		// Find correct parent.
		// Pop stack until we find a frame with smaller indent.
		for len(stack) > 1 && stack[len(stack)-1].level >= indent {
			stack = stack[:len(stack)-1]
		}

		top := stack[len(stack)-1]
		if top.parent == nil {
			root = append(root, block)
		} else {
			top.parent.Children = append(top.parent.Children, block)
		}
		stack = append(stack, frame{level: indent, parent: block})
		currentBlock = block
	}

	return root
}

// extractInlineProperties extracts key:: value pairs from a block content line.
func extractInlineProperties(content string) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		if m := propLineRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			props[strings.ToLower(m[1])] = strings.TrimSpace(m[2])
		}
	}
	return props
}

// stripProperties removes key:: value lines from block content.
func stripProperties(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	for _, line := range lines {
		if propLineRe.MatchString(strings.TrimSpace(line)) {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// extractPageProperties returns top-level page properties from the first block.
func extractPageProperties(blocks []*Block) map[string]string {
	if len(blocks) == 0 {
		return map[string]string{}
	}
	return blocks[0].Properties
}

func extractPageRefs(raw string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range pageRefRe.FindAllStringSubmatch(raw, -1) {
		ref := strings.TrimSpace(m[1])
		if ref == "" {
			continue
		}
		if _, exists := seen[ref]; exists {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func extractPageTags(raw string, props map[string]string) []string {
	seen := map[string]struct{}{}
	var out []string

	add := func(tag string) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return
		}
		lc := strings.ToLower(tag)
		if _, exists := seen[lc]; exists {
			return
		}
		seen[lc] = struct{}{}
		out = append(out, tag)
	}

	// tags:: property.
	if t, ok := props["tags"]; ok {
		for _, part := range strings.Split(t, ",") {
			add(strings.TrimSpace(part))
		}
	}

	// Inline #tags.
	for _, m := range inlineTagRe.FindAllStringSubmatch(raw, -1) {
		add(m[1])
	}

	return out
}

// stableExternalID returns a deterministic ID for a graph-relative path.
func stableExternalID(relPath string) string {
	sum := sha256.Sum256([]byte("logseq:" + relPath))
	return "lsq-" + hex.EncodeToString(sum[:8])
}
