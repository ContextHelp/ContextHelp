package bookmarks

import (
	"fmt"
	"html"
	"os"
	"strconv"
	"strings"
)

// Bookmark represents a single bookmark item from Netscape-style HTML exports.
type Bookmark struct {
	URL          string
	Title        string
	FolderPath   string
	AddDate      int64
	LastModified int64
}

// ParseBookmarksFile parses a bookmarks HTML export file.
func ParseBookmarksFile(path string) ([]Bookmark, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bookmarks file: %w", err)
	}
	return ParseBookmarks(data)
}

// ParseBookmarks parses bookmarks from Netscape bookmark HTML.
func ParseBookmarks(data []byte) ([]Bookmark, error) {
	input := string(data)
	if strings.TrimSpace(input) == "" {
		return nil, fmt.Errorf("bookmarks input is empty")
	}

	var (
		bookmarks     []Bookmark
		folders       []string
		pendingFolder string
		pos           int
	)

	lowerInput := strings.ToLower(input)

	for pos < len(input) {
		lt := strings.IndexByte(input[pos:], '<')
		if lt < 0 {
			break
		}
		lt += pos

		gt := strings.IndexByte(input[lt:], '>')
		if gt < 0 {
			break
		}
		gt += lt

		tag := input[lt : gt+1]
		lowerTag := strings.ToLower(tag)

		switch {
		case isTagStart(lowerTag, "h3"):
			inner, end, ok := readElementInner(input, lowerInput, gt+1, "h3")
			if ok {
				pendingFolder = cleanText(inner)
				pos = end
				continue
			}
		case isTagStart(lowerTag, "dl"):
			if pendingFolder != "" {
				folders = append(folders, pendingFolder)
				pendingFolder = ""
			}
		case isTagEnd(lowerTag, "dl"):
			if len(folders) > 0 {
				folders = folders[:len(folders)-1]
			}
		case isTagStart(lowerTag, "a"):
			inner, end, ok := readElementInner(input, lowerInput, gt+1, "a")
			if ok {
				href := html.UnescapeString(strings.TrimSpace(parseTagAttr(tag, "href")))
				if href != "" {
					title := cleanText(inner)
					if title == "" {
						title = href
					}
					bookmarks = append(bookmarks, Bookmark{
						URL:          href,
						Title:        title,
						FolderPath:   strings.Join(folders, "/"),
						AddDate:      parseIntAttr(tag, "add_date"),
						LastModified: parseIntAttr(tag, "last_modified"),
					})
				}
				pos = end
				continue
			}
		}

		pos = gt + 1
	}

	return bookmarks, nil
}

func readElementInner(input, lowerInput string, start int, tagName string) (inner string, end int, ok bool) {
	closing := "</" + strings.ToLower(tagName) + ">"
	idx := strings.Index(lowerInput[start:], closing)
	if idx < 0 {
		return "", start, false
	}
	inner = input[start : start+idx]
	end = start + idx + len(closing)
	return inner, end, true
}

func cleanText(s string) string {
	if s == "" {
		return ""
	}
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
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(text)
}

func parseIntAttr(tag, key string) int64 {
	raw := strings.TrimSpace(parseTagAttr(tag, key))
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func parseTagAttr(tag, attr string) string {
	lowerTag := strings.ToLower(tag)
	lowerAttr := strings.ToLower(attr)
	searchFrom := 0

	for {
		idx := strings.Index(lowerTag[searchFrom:], lowerAttr)
		if idx < 0 {
			return ""
		}
		idx += searchFrom

		if idx > 0 {
			prev := lowerTag[idx-1]
			if isAttrNameChar(prev) {
				searchFrom = idx + len(lowerAttr)
				continue
			}
		}

		p := idx + len(lowerAttr)
		for p < len(tag) && isSpace(tag[p]) {
			p++
		}
		if p >= len(tag) || tag[p] != '=' {
			searchFrom = idx + len(lowerAttr)
			continue
		}
		p++
		for p < len(tag) && isSpace(tag[p]) {
			p++
		}
		if p >= len(tag) {
			return ""
		}

		if tag[p] == '"' || tag[p] == '\'' {
			quote := tag[p]
			p++
			start := p
			for p < len(tag) && tag[p] != quote {
				p++
			}
			return tag[start:p]
		}

		start := p
		for p < len(tag) && !isSpace(tag[p]) && tag[p] != '>' {
			p++
		}
		return tag[start:p]
	}
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func isAttrNameChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' || b == '-'
}

func isTagStart(tag, name string) bool {
	prefix := "<" + name
	if !strings.HasPrefix(tag, prefix) {
		return false
	}
	if len(tag) == len(prefix) {
		return true
	}
	c := tag[len(prefix)]
	return c == '>' || isSpace(c)
}

func isTagEnd(tag, name string) bool {
	prefix := "</" + name
	if !strings.HasPrefix(tag, prefix) {
		return false
	}
	if len(tag) == len(prefix) {
		return true
	}
	c := tag[len(prefix)]
	return c == '>' || isSpace(c)
}
