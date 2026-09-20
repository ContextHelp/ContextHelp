// Package rssfeed — RSS/Atom XML parsing (stdlib only).
package rssfeed

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

// feedItem is a normalised representation of a single entry from any feed format.
type feedItem struct {
	Title     string
	Link      string
	GUID      string
	Published time.Time
	Content   string
	Summary   string
	Author    string
}

// ─── RSS 2.0 ──────────────────────────────────────────────────────────────────

type rssRoot struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title string    `xml:"title"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
	Content     string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	Author      string `xml:"author"`
	Creator     string `xml:"http://purl.org/dc/elements/1.1/ creator"`
}

// ─── Atom 1.0 ─────────────────────────────────────────────────────────────────

type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Entries []atomEntry `xml:"http://www.w3.org/2005/Atom entry"`
}

type atomEntry struct {
	Title   atomText     `xml:"http://www.w3.org/2005/Atom title"`
	ID      string       `xml:"http://www.w3.org/2005/Atom id"`
	Updated string       `xml:"http://www.w3.org/2005/Atom updated"`
	Links   []atomLink   `xml:"http://www.w3.org/2005/Atom link"`
	Summary atomText     `xml:"http://www.w3.org/2005/Atom summary"`
	Content atomText     `xml:"http://www.w3.org/2005/Atom content"`
	Authors []atomPerson `xml:"http://www.w3.org/2005/Atom author"`
}

type atomText struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

type atomPerson struct {
	Name string `xml:"http://www.w3.org/2005/Atom name"`
}

// parseFeed parses RSS 2.0 or Atom 1.0 from r and returns normalised items.
func parseFeed(r io.Reader) ([]feedItem, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("rss-feed: read body: %w", err)
	}

	// Try Atom first (has explicit namespace).
	var atom atomFeed
	if err := xml.Unmarshal(data, &atom); err == nil && len(atom.Entries) > 0 {
		return normaliseAtom(atom), nil
	}

	// Fall back to RSS 2.0.
	var rss rssRoot
	if err := xml.Unmarshal(data, &rss); err != nil {
		return nil, fmt.Errorf("rss-feed: parse XML: %w", err)
	}
	return normaliseRSS(rss), nil
}

func normaliseRSS(rss rssRoot) []feedItem {
	items := make([]feedItem, 0, len(rss.Channel.Items))
	for _, it := range rss.Channel.Items {
		fi := feedItem{
			Title:   strings.TrimSpace(it.Title),
			Link:    strings.TrimSpace(it.Link),
			GUID:    strings.TrimSpace(it.GUID),
			Summary: strings.TrimSpace(it.Description),
			Author:  firstNonEmpty(it.Author, it.Creator),
		}
		if it.Content != "" {
			fi.Content = strings.TrimSpace(it.Content)
		}
		if fi.GUID == "" {
			fi.GUID = fi.Link
		}
		fi.Published = parseRSSDate(it.PubDate)
		items = append(items, fi)
	}
	return items
}

func normaliseAtom(atom atomFeed) []feedItem {
	items := make([]feedItem, 0, len(atom.Entries))
	for _, e := range atom.Entries {
		fi := feedItem{
			Title:   strings.TrimSpace(e.Title.Value),
			GUID:    strings.TrimSpace(e.ID),
			Summary: strings.TrimSpace(e.Summary.Value),
			Content: strings.TrimSpace(e.Content.Value),
		}
		for _, ln := range e.Links {
			if ln.Rel == "alternate" || ln.Rel == "" {
				fi.Link = strings.TrimSpace(ln.Href)
				break
			}
		}
		if len(e.Authors) > 0 {
			fi.Author = strings.TrimSpace(e.Authors[0].Name)
		}
		fi.Published = parseAtomDate(e.Updated)
		items = append(items, fi)
	}
	return items
}

// parseRSSDate attempts to parse common RSS date formats.
func parseRSSDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 02 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 02 Jan 2006 15:04:05 MST",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseAtomDate parses RFC3339/ISO8601 Atom dates.
func parseAtomDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, f := range []string{time.RFC3339, time.RFC3339Nano} {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
