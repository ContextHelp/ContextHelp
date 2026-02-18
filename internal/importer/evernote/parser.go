package evernote

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Note is a normalized Evernote note ready for import.
type Note struct {
	ExternalID    string
	GUID          string
	Title         string
	Content       string
	Created       time.Time
	Updated       time.Time
	Tags          []string
	SourceURL     string
	ResourceCount int
}

// ParseExportFile parses an Evernote export file from disk (ENEX or HTML).
func ParseExportFile(path string) ([]Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read evernote export file: %w", err)
	}
	return ParseExport(data, path)
}

// ParseExport parses Evernote export bytes and auto-detects ENEX/HTML format.
func ParseExport(data []byte, sourceName string) ([]Note, error) {
	input := strings.TrimSpace(string(data))
	if input == "" {
		return nil, fmt.Errorf("evernote export input is empty")
	}

	lower := strings.ToLower(input)
	switch {
	case strings.Contains(lower, "<en-export"):
		return ParseENEX(data)
	case strings.Contains(lower, "<html"):
		return parseHTMLExport(data, sourceName)
	default:
		return nil, fmt.Errorf("unsupported evernote export format (expected ENEX or HTML)")
	}
}

// ParseENEX parses notes from an Evernote ENEX export.
func ParseENEX(data []byte) ([]Note, error) {
	if strings.TrimSpace(string(data)) == "" {
		return nil, fmt.Errorf("enex input is empty")
	}

	var doc enExport
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse enex: %w", err)
	}

	notes := make([]Note, 0, len(doc.Notes))
	for i, raw := range doc.Notes {
		title := strings.TrimSpace(raw.Title)
		if title == "" {
			title = fmt.Sprintf("Untitled note %d", i+1)
		}

		n := Note{
			GUID:          strings.TrimSpace(raw.GUID),
			Title:         title,
			Content:       extractText(raw.Content),
			Created:       parseEvernoteTime(raw.Created),
			Updated:       parseEvernoteTime(raw.Updated),
			Tags:          normalizeTags(raw.Tags),
			SourceURL:     strings.TrimSpace(raw.NoteAttributes.SourceURL),
			ResourceCount: len(raw.Resources),
		}

		n.ExternalID = n.GUID
		if n.ExternalID == "" {
			n.ExternalID = fallbackExternalID(raw, i)
		}

		notes = append(notes, n)
	}

	return notes, nil
}

// RenderContent formats a normalized note for enqueue as text content.
func RenderContent(note Note) string {
	var b strings.Builder

	title := strings.TrimSpace(note.Title)
	if title == "" {
		title = note.ExternalID
	}
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	if note.SourceURL != "" {
		b.WriteString("Source: ")
		b.WriteString(note.SourceURL)
		b.WriteString("\n\n")
	}

	b.WriteString("Provider: Evernote\n")
	if note.ExternalID != "" {
		b.WriteString("External ID: ")
		b.WriteString(note.ExternalID)
		b.WriteString("\n")
	}
	if !note.Created.IsZero() {
		b.WriteString("Created: ")
		b.WriteString(note.Created.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	if !note.Updated.IsZero() {
		b.WriteString("Updated: ")
		b.WriteString(note.Updated.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	if len(note.Tags) > 0 {
		b.WriteString("Tags: ")
		b.WriteString(strings.Join(note.Tags, ", "))
		b.WriteString("\n")
	}
	if note.ResourceCount > 0 {
		b.WriteString("Resources: ")
		b.WriteString(fmt.Sprintf("%d", note.ResourceCount))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if strings.TrimSpace(note.Content) != "" {
		b.WriteString(strings.TrimSpace(note.Content))
		b.WriteString("\n")
	}

	return b.String()
}

func parseHTMLExport(data []byte, sourceName string) ([]Note, error) {
	input := strings.TrimSpace(string(data))
	if input == "" {
		return nil, fmt.Errorf("html export input is empty")
	}

	title := extractHTMLTitle(input)
	if title == "" && strings.TrimSpace(sourceName) != "" {
		base := filepath.Base(sourceName)
		title = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if title == "" {
		title = "Evernote HTML Export"
	}

	content := extractText(input)
	raw := enNote{
		Title:   title,
		Content: input,
	}

	n := Note{
		ExternalID: fallbackExternalID(raw, 0),
		Title:      title,
		Content:    content,
	}
	return []Note{n}, nil
}

func extractHTMLTitle(input string) string {
	lower := strings.ToLower(input)
	start := strings.Index(lower, "<title>")
	if start < 0 {
		return ""
	}
	start += len("<title>")
	end := strings.Index(lower[start:], "</title>")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(input[start : start+end]))
}

func extractText(input string) string {
	if input == "" {
		return ""
	}

	s := strings.NewReplacer(
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
		"</p>", "\n",
		"</div>", "\n",
		"</li>", "\n",
		"</tr>", "\n",
		"</table>", "\n",
		"</ul>", "\n",
		"</ol>", "\n",
		"</h1>", "\n",
		"</h2>", "\n",
		"</h3>", "\n",
	).Replace(input)

	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}

	text := html.UnescapeString(b.String())
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	outLines := make([]string, 0, len(lines))
	previousBlank := false
	for _, line := range lines {
		line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		if line == "" {
			if !previousBlank && len(outLines) > 0 {
				outLines = append(outLines, "")
				previousBlank = true
			}
			continue
		}
		outLines = append(outLines, line)
		previousBlank = false
	}

	return strings.TrimSpace(strings.Join(outLines, "\n"))
}

func normalizeTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	return out
}

func parseEvernoteTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}

	layouts := []string{
		"20060102T150405Z",
		time.RFC3339,
		"2006-01-02",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts.UTC()
		}
	}
	return time.Time{}
}

func fallbackExternalID(raw enNote, index int) string {
	payload := strings.TrimSpace(raw.Title) + "\n" +
		strings.TrimSpace(raw.Created) + "\n" +
		strings.TrimSpace(raw.Updated) + "\n" +
		strings.TrimSpace(raw.Content) + "\n" +
		fmt.Sprintf("%d", index)
	sum := sha1.Sum([]byte(payload))
	return "enex-" + hex.EncodeToString(sum[:8])
}

type enExport struct {
	XMLName xml.Name `xml:"en-export"`
	Notes   []enNote `xml:"note"`
}

type enNote struct {
	GUID           string           `xml:"guid"`
	Title          string           `xml:"title"`
	Content        string           `xml:"content"`
	Created        string           `xml:"created"`
	Updated        string           `xml:"updated"`
	Tags           []string         `xml:"tag"`
	NoteAttributes enNoteAttributes `xml:"note-attributes"`
	Resources      []enResource     `xml:"resource"`
}

type enNoteAttributes struct {
	SourceURL string `xml:"source-url"`
}

type enResource struct{}
