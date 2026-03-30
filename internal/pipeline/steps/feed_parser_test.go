package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

const testRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>My RSS Feed</title>
    <item>
      <guid>guid-1</guid>
      <title>Item One</title>
      <link>https://example.com/1</link>
      <description>Content one</description>
      <pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate>
    </item>
    <item>
      <guid>guid-2</guid>
      <title>Item Two</title>
      <link>https://example.com/2</link>
      <description>Content two</description>
    </item>
  </channel>
</rss>`

const testAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>My Atom Feed</title>
  <entry>
    <id>urn:uuid:atom-1</id>
    <title>Atom Entry One</title>
    <link href="https://example.com/atom/1" rel="alternate"/>
    <summary>Atom summary one</summary>
    <published>2024-01-01T00:00:00Z</published>
  </entry>
</feed>`

const testJSONFeed = `{
  "version": "https://jsonfeed.org/version/1.1",
  "title": "My JSON Feed",
  "items": [
    {
      "id": "json-1",
      "url": "https://example.com/json/1",
      "title": "JSON Item One",
      "content_text": "JSON content one",
      "date_published": "2024-01-01T00:00:00Z"
    }
  ]
}`

func TestFeedParserRSS(t *testing.T) {
	step := NewFeedParser()
	draft := &storage.KnowledgeObject{RawContent: testRSS}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["feed_format"] != "rss2.0" {
		t.Errorf("feed_format: got %v", got.Metadata["feed_format"])
	}
	if got.Metadata["feed_title"] != "My RSS Feed" {
		t.Errorf("feed_title: got %v", got.Metadata["feed_title"])
	}
	items, ok := got.Metadata["feed_items"].([]map[string]any)
	if !ok || len(items) != 2 {
		t.Fatalf("feed_items: got %T len=%d", got.Metadata["feed_items"], len(items))
	}
	if items[0]["guid"] != "guid-1" {
		t.Errorf("item[0].guid: got %v", items[0]["guid"])
	}
	if items[0]["title"] != "Item One" {
		t.Errorf("item[0].title: got %v", items[0]["title"])
	}
}

func TestFeedParserAtom(t *testing.T) {
	step := NewFeedParser()
	draft := &storage.KnowledgeObject{RawContent: testAtom}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["feed_format"] != "atom1.0" {
		t.Errorf("feed_format: got %v", got.Metadata["feed_format"])
	}
	if got.Metadata["feed_title"] != "My Atom Feed" {
		t.Errorf("feed_title: got %v", got.Metadata["feed_title"])
	}
	items, ok := got.Metadata["feed_items"].([]map[string]any)
	if !ok || len(items) != 1 {
		t.Fatalf("feed_items: got %T len=%d", got.Metadata["feed_items"], len(items))
	}
	if items[0]["guid"] != "urn:uuid:atom-1" {
		t.Errorf("item[0].guid: got %v", items[0]["guid"])
	}
}

func TestFeedParserJSONFeed(t *testing.T) {
	step := NewFeedParser()
	draft := &storage.KnowledgeObject{RawContent: testJSONFeed}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["feed_format"] != "json1.1" {
		t.Errorf("feed_format: got %v", got.Metadata["feed_format"])
	}
	if got.Metadata["feed_title"] != "My JSON Feed" {
		t.Errorf("feed_title: got %v", got.Metadata["feed_title"])
	}
	items, ok := got.Metadata["feed_items"].([]map[string]any)
	if !ok || len(items) != 1 {
		t.Fatalf("feed_items: expected 1 item")
	}
	if items[0]["guid"] != "json-1" {
		t.Errorf("item[0].guid: got %v", items[0]["guid"])
	}
}

func TestFeedParserEmptyContent(t *testing.T) {
	step := NewFeedParser()
	draft := &storage.KnowledgeObject{RawContent: ""}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// No format set for empty content.
	if _, ok := got.Metadata["feed_format"]; ok {
		t.Error("expected no feed_format for empty content")
	}
}

func TestFeedParserEmitsGraphNodes(t *testing.T) {
	step := NewFeedParser()
	draft := &storage.KnowledgeObject{
		ID:         "obj-feed-001",
		RawContent: testRSS,
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected nodes")
	}
	var sectionNodes []pluginapi.GraphNode
	for _, n := range got.Graph.Nodes {
		if n.NodeType == pluginapi.NodeTypeSection {
			sectionNodes = append(sectionNodes, n)
		}
	}
	if len(sectionNodes) != 2 {
		t.Errorf("expected 2 section nodes (one per RSS item), got %d", len(sectionNodes))
	}
	if sectionNodes[0].Label != "Item One" {
		t.Errorf("section[0] label: got %q", sectionNodes[0].Label)
	}
}

func TestFeedParserNoGraphWithoutID(t *testing.T) {
	step := NewFeedParser()
	draft := &storage.KnowledgeObject{RawContent: testRSS}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil && len(got.Graph.Nodes) > 0 {
		t.Error("expected no graph nodes when ID is empty")
	}
}

func TestFeedParserUnknownFormat(t *testing.T) {
	step := NewFeedParser()
	draft := &storage.KnowledgeObject{RawContent: "plain text not a feed"}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}
