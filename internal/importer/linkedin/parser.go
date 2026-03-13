// Package linkedin provides a parser for LinkedIn data export packages.
//
// LinkedIn data exports contain CSV files for different content types:
//   - Posts.csv   — posts and updates published on LinkedIn
//   - Articles.csv — long-form articles published via LinkedIn Pulse
//   - Profile.csv — profile section data (summary, positions, education, etc.)
//
// Each file uses a comma-separated format with a header row.
package linkedin

import (
	"encoding/csv"
	"fmt"
	"strings"
	"time"
)

// PostRecord is a normalised record from Posts.csv.
type PostRecord struct {
	// Date is the publication date of the post.
	Date time.Time

	// ShareCommentary is the primary post text.
	ShareCommentary string

	// SharedURL is the URL attached to the post (if any).
	SharedURL string

	// ShareMediaCategory indicates the media type (e.g. "IMAGE", "ARTICLE").
	ShareMediaCategory string
}

// ArticleRecord is a normalised record from Articles.csv.
type ArticleRecord struct {
	// PublishedAt is when the article was published.
	PublishedAt time.Time

	// Title is the article headline.
	Title string

	// URL is the canonical article URL.
	URL string

	// Summary is the short article description or first paragraph.
	Summary string
}

// ParsePosts parses the raw CSV content of Posts.csv and returns normalised
// PostRecord values.
func ParsePosts(content string) ([]PostRecord, error) {
	rows, headers, err := parseCSV(content)
	if err != nil {
		return nil, fmt.Errorf("linkedin posts parser: %w", err)
	}

	out := make([]PostRecord, 0, len(rows))
	for _, row := range rows {
		m := rowToMap(headers, row)
		rec := PostRecord{
			ShareCommentary:    strings.TrimSpace(m["ShareCommentary"]),
			SharedURL:          strings.TrimSpace(m["SharedUrl"]),
			ShareMediaCategory: strings.TrimSpace(m["ShareMediaCategory"]),
		}
		// LinkedIn exports dates as "YYYY-MM-DD HH:MM:SS UTC" or "YYYY-MM-DD".
		if dateStr := strings.TrimSpace(m["Date"]); dateStr != "" {
			if ts, err := parseLinkedInTime(dateStr); err == nil {
				rec.Date = ts
			}
		}
		// Skip entries with no meaningful content.
		if rec.ShareCommentary == "" && rec.SharedURL == "" {
			continue
		}
		out = append(out, rec)
	}

	return out, nil
}

// ParseArticles parses the raw CSV content of Articles.csv and returns
// normalised ArticleRecord values.
func ParseArticles(content string) ([]ArticleRecord, error) {
	rows, headers, err := parseCSV(content)
	if err != nil {
		return nil, fmt.Errorf("linkedin articles parser: %w", err)
	}

	out := make([]ArticleRecord, 0, len(rows))
	for _, row := range rows {
		m := rowToMap(headers, row)
		rec := ArticleRecord{
			Title:   strings.TrimSpace(m["Title"]),
			URL:     strings.TrimSpace(m["Url"]),
			Summary: strings.TrimSpace(m["Description"]),
		}
		if dateStr := strings.TrimSpace(m["PublishedAt"]); dateStr != "" {
			if ts, err := parseLinkedInTime(dateStr); err == nil {
				rec.PublishedAt = ts
			}
		}
		// Skip entries with no meaningful content.
		if rec.Title == "" && rec.URL == "" {
			continue
		}
		out = append(out, rec)
	}

	return out, nil
}

// RenderPost converts a PostRecord to a human-readable markdown string suitable
// for ingestion into dPKMS.
func RenderPost(p PostRecord) string {
	var b strings.Builder

	title := p.ShareCommentary
	if len(title) > 80 {
		title = title[:80] + "…"
	}
	if title == "" {
		title = "LinkedIn Post"
	}
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	b.WriteString("Provider: LinkedIn\n")
	b.WriteString("Content Type: Post\n")

	if !p.Date.IsZero() {
		b.WriteString("Published: ")
		b.WriteString(p.Date.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	if p.ShareMediaCategory != "" {
		b.WriteString("Media Category: ")
		b.WriteString(p.ShareMediaCategory)
		b.WriteString("\n")
	}
	if p.SharedURL != "" {
		b.WriteString("Shared URL: ")
		b.WriteString(p.SharedURL)
		b.WriteString("\n")
	}

	if p.ShareCommentary != "" {
		b.WriteString("\n")
		b.WriteString(p.ShareCommentary)
		b.WriteString("\n")
	}

	return b.String()
}

// RenderArticle converts an ArticleRecord to a human-readable markdown string
// suitable for ingestion into dPKMS.
func RenderArticle(a ArticleRecord) string {
	var b strings.Builder

	title := a.Title
	if title == "" {
		title = "LinkedIn Article"
	}
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	b.WriteString("Provider: LinkedIn\n")
	b.WriteString("Content Type: Article\n")

	if !a.PublishedAt.IsZero() {
		b.WriteString("Published: ")
		b.WriteString(a.PublishedAt.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	if a.URL != "" {
		b.WriteString("URL: ")
		b.WriteString(a.URL)
		b.WriteString("\n")
	}

	if a.Summary != "" {
		b.WriteString("\n")
		b.WriteString(a.Summary)
		b.WriteString("\n")
	}

	return b.String()
}

// PostDedupeKey returns a stable deduplication key for a LinkedIn post.
// Because LinkedIn posts don't always have IDs in the export, we key by
// content hash (date + first 200 chars of commentary).
func PostDedupeKey(p PostRecord) string {
	text := p.ShareCommentary
	if len(text) > 200 {
		text = text[:200]
	}
	return "linkedin:post:" + p.Date.UTC().Format("20060102T150405Z") + ":" + strings.TrimSpace(text)
}

// ArticleDedupeKey returns a stable deduplication key for a LinkedIn article.
func ArticleDedupeKey(a ArticleRecord) string {
	if a.URL != "" {
		return "linkedin:article:" + strings.TrimSpace(a.URL)
	}
	return "linkedin:article:" + a.PublishedAt.UTC().Format("20060102T150405Z") + ":" + strings.TrimSpace(a.Title)
}

// FilterPostsSince returns only posts published at or after the given time.
// If since is nil all records are returned.
func FilterPostsSince(posts []PostRecord, since *time.Time) []PostRecord {
	if since == nil {
		return posts
	}
	out := make([]PostRecord, 0, len(posts))
	for _, p := range posts {
		if !p.Date.IsZero() && p.Date.Before(*since) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// FilterArticlesSince returns only articles published at or after the given time.
// If since is nil all records are returned.
func FilterArticlesSince(articles []ArticleRecord, since *time.Time) []ArticleRecord {
	if since == nil {
		return articles
	}
	out := make([]ArticleRecord, 0, len(articles))
	for _, a := range articles {
		if !a.PublishedAt.IsZero() && a.PublishedAt.Before(*since) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// parseCSV parses raw CSV text and returns rows and the header slice.
func parseCSV(content string) ([][]string, []string, error) {
	r := csv.NewReader(strings.NewReader(content))
	r.FieldsPerRecord = -1 // Allow variable field counts.
	r.LazyQuotes = true

	rows, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, nil
	}
	headers := rows[0]
	return rows[1:], headers, nil
}

func rowToMap(headers []string, row []string) map[string]string {
	m := make(map[string]string, len(headers))
	for i, h := range headers {
		if i < len(row) {
			m[strings.TrimSpace(h)] = row[i]
		}
	}
	return m
}

// parseLinkedInTime tries several date formats used in LinkedIn exports.
func parseLinkedInTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	formats := []string{
		"2006-01-02 15:04:05 UTC",
		"2006-01-02 15:04:05",
		"2006-01-02",
		time.RFC3339,
	}
	for _, f := range formats {
		if ts, err := time.Parse(f, raw); err == nil {
			return ts.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable time %q", raw)
}
