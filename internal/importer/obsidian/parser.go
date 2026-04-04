// Package obsidian implements the Obsidian Vault importer (P-100).
// It walks a local vault directory, parses markdown files, extracts
// YAML frontmatter, wikilinks, tags, and attachment references, and
// emits normalized Note records ready for enqueueing into dPKMS/ctxt.
package obsidian

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
	"unicode"

	"gopkg.in/yaml.v3"
)

// Note is a normalized Obsidian note ready for import.
type Note struct {
	// ExternalID is a stable dedup key derived from the vault-relative path.
	ExternalID string
	// Path is the absolute path to the source file.
	Path string
	// RelPath is the vault-relative path (forward slashes, no leading slash).
	RelPath string
	// Title is the note title: frontmatter "title" field, else the filename stem.
	Title string
	// Content is the raw markdown body (frontmatter stripped).
	Content string
	// Frontmatter holds all parsed YAML frontmatter key/value pairs.
	Frontmatter map[string]any
	// Tags combines inline #tags and frontmatter tags.
	Tags []string
	// Wikilinks lists every [[target]] found in the note body.
	Wikilinks []string
	// Attachments lists media/file references found in the note body.
	Attachments []string
	// ContentHash is the SHA-256 hex digest of the raw file bytes.
	ContentHash string
	// ModTime is the file's last-modified time.
	ModTime time.Time
}

// WalkVault returns all markdown notes found under vaultDir (recursive).
// Symlinked directories are skipped to avoid duplicate traversal.
func WalkVault(vaultDir string) ([]Note, error) {
	vaultDir = filepath.Clean(vaultDir)
	info, err := os.Stat(vaultDir)
	if err != nil {
		return nil, fmt.Errorf("obsidian: stat vault dir %q: %w", vaultDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("obsidian: vault path %q is not a directory", vaultDir)
	}

	var notes []Note
	err = filepath.WalkDir(vaultDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// Skip the hidden Obsidian config folder.
		if d.IsDir() && d.Name() == ".obsidian" {
			return filepath.SkipDir
		}
		// Skip symlinked directories to avoid duplicate traversal.
		if d.IsDir() {
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

		data, err := os.ReadFile(path) // #nosec G122 -- reading user's own vault; symlink TOCTOU not a threat
		if err != nil {
			return fmt.Errorf("obsidian: read %q: %w", path, err)
		}

		fi, err := d.Info()
		if err != nil {
			return fmt.Errorf("obsidian: stat %q: %w", path, err)
		}

		rel, err := filepath.Rel(vaultDir, path)
		if err != nil {
			rel = path
		}
		// Normalize to forward slashes.
		rel = filepath.ToSlash(rel)

		note, err := ParseNote(data, rel, path, fi.ModTime())
		if err != nil {
			return fmt.Errorf("obsidian: parse %q: %w", path, err)
		}
		notes = append(notes, note)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return notes, nil
}

// ParseNote parses a single markdown file's bytes into a Note.
func ParseNote(data []byte, relPath, absPath string, modTime time.Time) (Note, error) {
	raw := string(data)

	fm, body, err := splitFrontmatter(raw)
	if err != nil {
		// Non-fatal: treat whole file as body.
		fm = map[string]any{}
		body = raw
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	stem := filenameStem(relPath)
	title := stem
	if t, ok := fm["title"]; ok {
		if ts, ok := t.(string); ok && strings.TrimSpace(ts) != "" {
			title = strings.TrimSpace(ts)
		}
	}

	tags := extractAllTags(fm, body)
	wikilinks := extractWikilinks(body)
	attachments := extractAttachments(body)

	extID := stableExternalID(relPath)

	return Note{
		ExternalID:  extID,
		Path:        absPath,
		RelPath:     relPath,
		Title:       title,
		Content:     body,
		Frontmatter: fm,
		Tags:        tags,
		Wikilinks:   wikilinks,
		Attachments: attachments,
		ContentHash: hash,
		ModTime:     modTime,
	}, nil
}

// RenderContent formats a Note into the text content submitted to the ctxt pipeline.
func RenderContent(note Note) string {
	var b strings.Builder

	b.WriteString("# ")
	b.WriteString(note.Title)
	b.WriteString("\n\n")

	b.WriteString("Provider: Obsidian\n")
	if note.ExternalID != "" {
		b.WriteString("External ID: ")
		b.WriteString(note.ExternalID)
		b.WriteString("\n")
	}
	if note.RelPath != "" {
		b.WriteString("Vault Path: ")
		b.WriteString(note.RelPath)
		b.WriteString("\n")
	}
	if !note.ModTime.IsZero() {
		b.WriteString("Modified: ")
		b.WriteString(note.ModTime.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	if len(note.Tags) > 0 {
		b.WriteString("Tags: ")
		b.WriteString(strings.Join(note.Tags, ", "))
		b.WriteString("\n")
	}
	if len(note.Wikilinks) > 0 {
		b.WriteString("Links: ")
		b.WriteString(strings.Join(note.Wikilinks, ", "))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	body := strings.TrimSpace(note.Content)
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.String()
}

// splitFrontmatter splits raw markdown into YAML frontmatter (parsed) and body.
// Returns empty map and full input if no frontmatter is found.
func splitFrontmatter(raw string) (map[string]any, string, error) {
	if !strings.HasPrefix(raw, "---") {
		return map[string]any{}, raw, nil
	}

	// Find closing ---
	rest := raw[3:]
	// Skip optional newline immediately after opening ---
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	} else if len(rest) > 1 && rest[0] == '\r' && rest[1] == '\n' {
		rest = rest[2:]
	}

	scanner := bufio.NewScanner(strings.NewReader(rest))
	var fmLines []string
	var bodyLines []string
	inFM := true
	for scanner.Scan() {
		line := scanner.Text()
		if inFM && line == "---" {
			inFM = false
			continue
		}
		if inFM {
			fmLines = append(fmLines, line)
		} else {
			bodyLines = append(bodyLines, line)
		}
	}

	if inFM {
		// No closing --- found; treat entire file as body.
		return map[string]any{}, raw, nil
	}

	fm := map[string]any{}
	if len(fmLines) > 0 {
		if err := yaml.Unmarshal([]byte(strings.Join(fmLines, "\n")), &fm); err != nil {
			return map[string]any{}, raw, fmt.Errorf("parse frontmatter: %w", err)
		}
	}

	body := strings.Join(bodyLines, "\n")
	return fm, body, nil
}

var (
	wikilinkRe  = regexp.MustCompile(`\[\[([^\[\]|#]+)(?:[|#][^\[\]]*)?\]\]`)
	inlineTagRe = regexp.MustCompile(`(?:^|[\s(])#([A-Za-z][A-Za-z0-9_/-]*)`)
	attachRe    = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)
	embedRe     = regexp.MustCompile(`!\[\[([^\[\]]+)\]\]`)
)

func extractWikilinks(body string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range wikilinkRe.FindAllStringSubmatch(body, -1) {
		target := strings.TrimSpace(m[1])
		if target == "" {
			continue
		}
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
}

func extractAttachments(body string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}
		if _, exists := seen[ref]; exists {
			return
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	for _, m := range attachRe.FindAllStringSubmatch(body, -1) {
		add(m[1])
	}
	for _, m := range embedRe.FindAllStringSubmatch(body, -1) {
		add(m[1])
	}
	return out
}

func extractInlineTags(body string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range inlineTagRe.FindAllStringSubmatch(body, -1) {
		tag := strings.TrimSpace(m[1])
		if tag == "" {
			continue
		}
		lc := strings.ToLower(tag)
		if _, exists := seen[lc]; exists {
			continue
		}
		seen[lc] = struct{}{}
		out = append(out, tag)
	}
	return out
}

func extractAllTags(fm map[string]any, body string) []string {
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

	// Frontmatter tags: supports "tags: [a, b]" and "tags:\n  - a".
	if t, ok := fm["tags"]; ok {
		switch v := t.(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					add(s)
				}
			}
		case string:
			for _, part := range strings.Split(v, ",") {
				add(strings.TrimSpace(part))
			}
		}
	}

	// Inline body tags.
	for _, tag := range extractInlineTags(body) {
		add(tag)
	}

	return out
}

func filenameStem(relPath string) string {
	base := filepath.Base(relPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	// Replace common path separators with spaces for readability.
	stem = strings.Map(func(r rune) rune {
		if r == '_' || r == '-' {
			return ' '
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '.' {
			return r
		}
		return ' '
	}, stem)
	return strings.TrimSpace(stem)
}

// stableExternalID returns a deterministic ID for a vault-relative path.
func stableExternalID(relPath string) string {
	sum := sha256.Sum256([]byte("obsidian:" + relPath))
	return "obs-" + hex.EncodeToString(sum[:8])
}
