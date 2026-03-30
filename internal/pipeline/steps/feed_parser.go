package steps

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// FeedParser parses RSS 2.0, Atom 1.0, or JSON Feed from draft.RawContent.
type FeedParser struct {
	pipeline.BaseContract
}

// NewFeedParser creates a FeedParser.
func NewFeedParser() *FeedParser {
	return &FeedParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Metadata", "Sections"},
		}),
	}
}

func (s *FeedParser) Name() string { return "feed_parser" }

func (s *FeedParser) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	content := draft.RawContent
	if content == "" {
		return draft, nil
	}

	trimmed := strings.TrimSpace(content)

	var feedFormat string
	var feedTitle string
	var items []map[string]any

	switch {
	case strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, "jsonfeed.org"):
		feedFormat = "json1.1"
		title, parsed, err := parseJSONFeed(trimmed)
		if err != nil {
			return nil, fmt.Errorf("feed_parser: parse JSON Feed: %w", err)
		}
		feedTitle = title
		items = parsed
	case strings.Contains(trimmed, "<rss"):
		feedFormat = "rss2.0"
		title, parsed, err := parseRSS(trimmed)
		if err != nil {
			return nil, fmt.Errorf("feed_parser: parse RSS: %w", err)
		}
		feedTitle = title
		items = parsed
	case strings.Contains(trimmed, "<feed"):
		feedFormat = "atom1.0"
		title, parsed, err := parseAtom(trimmed)
		if err != nil {
			return nil, fmt.Errorf("feed_parser: parse Atom: %w", err)
		}
		feedTitle = title
		items = parsed
	default:
		return nil, fmt.Errorf("feed_parser: unrecognized feed format")
	}

	draft.Metadata["feed_format"] = feedFormat
	draft.Metadata["feed_title"] = feedTitle
	draft.Metadata["feed_items"] = items

	// Emit canonical graph nodes when ID is set.
	// Each feed item → one section node.
	if draft.ID == "" {
		return draft, nil
	}
	if draft.Graph == nil {
		draft.Graph = &pluginapi.ObjectGraph{}
	}
	for i, item := range items {
		title, _ := item["title"].(string)
		content, _ := item["content"].(string)
		link, _ := item["link"].(string)
		guid, _ := item["guid"].(string)
		published, _ := item["published"].(string)

		secID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeSection, i)
		draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
			ID:       secID,
			NodeType: pluginapi.NodeTypeSection,
			Label:    title,
			Content:  content,
			Order:    i,
			Metadata: map[string]any{
				"guid":        guid,
				"link":        link,
				"published":   published,
				"feed_format": feedFormat,
			},
		})
		// Flat sections mirror for projection fallback.
		draft.Sections = append(draft.Sections, storage.Section{
			Title:   title,
			Content: content,
			Order:   i,
			Metadata: map[string]any{
				"guid":      guid,
				"link":      link,
				"published": published,
			},
		})
	}

	return draft, nil
}

// --- RSS 2.0 ---

type rssRoot struct {
	XMLName xml.Name    `xml:"rss"`
	Channel rssChannel  `xml:"channel"`
}

type rssChannel struct {
	Title string    `xml:"title"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	GUID        string `xml:"guid"`
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

func parseRSS(content string) (string, []map[string]any, error) {
	var root rssRoot
	if err := xml.Unmarshal([]byte(content), &root); err != nil {
		return "", nil, err
	}
	items := make([]map[string]any, 0, len(root.Channel.Items))
	for _, item := range root.Channel.Items {
		guid := item.GUID
		if guid == "" {
			guid = item.Link
		}
		items = append(items, map[string]any{
			"guid":      guid,
			"title":     item.Title,
			"link":      item.Link,
			"content":   item.Description,
			"published": item.PubDate,
		})
	}
	return root.Channel.Title, items, nil
}

// --- Atom 1.0 ---

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID        string      `xml:"id"`
	Title     string      `xml:"title"`
	Links     []atomLink  `xml:"link"`
	Summary   string      `xml:"summary"`
	Content   string      `xml:"content"`
	Published string      `xml:"published"`
	Updated   string      `xml:"updated"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

func parseAtom(content string) (string, []map[string]any, error) {
	var feed atomFeed
	if err := xml.Unmarshal([]byte(content), &feed); err != nil {
		return "", nil, err
	}
	items := make([]map[string]any, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		link := ""
		for _, l := range entry.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				link = l.Href
				break
			}
		}
		body := entry.Content
		if body == "" {
			body = entry.Summary
		}
		published := entry.Published
		if published == "" {
			published = entry.Updated
		}
		items = append(items, map[string]any{
			"guid":      entry.ID,
			"title":     entry.Title,
			"link":      link,
			"content":   body,
			"published": published,
		})
	}
	return feed.Title, items, nil
}

// --- JSON Feed 1.1 ---

type jsonFeedDoc struct {
	Version string         `json:"version"`
	Title   string         `json:"title"`
	Items   []jsonFeedItem `json:"items"`
}

type jsonFeedItem struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	Title         string `json:"title"`
	ContentHTML   string `json:"content_html"`
	ContentText   string `json:"content_text"`
	DatePublished string `json:"date_published"`
}

func parseJSONFeed(content string) (string, []map[string]any, error) {
	var doc jsonFeedDoc
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return "", nil, err
	}
	items := make([]map[string]any, 0, len(doc.Items))
	for _, item := range doc.Items {
		body := item.ContentHTML
		if body == "" {
			body = item.ContentText
		}
		items = append(items, map[string]any{
			"guid":      item.ID,
			"title":     item.Title,
			"link":      item.URL,
			"content":   body,
			"published": item.DatePublished,
		})
	}
	return doc.Title, items, nil
}
