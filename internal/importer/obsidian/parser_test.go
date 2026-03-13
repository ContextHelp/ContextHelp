package obsidian

import (
	"strings"
	"testing"
	"time"
)

// --- Frontmatter parsing ---

func TestParseFrontmatter_Complete(t *testing.T) {
	raw := "---\ntitle: My Note\ntags:\n  - go\n  - testing\n---\n\nBody here."
	note, err := ParseNote([]byte(raw), "notes/my-note.md", "/vault/notes/my-note.md", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if note.Title != "My Note" {
		t.Errorf("title: want %q, got %q", "My Note", note.Title)
	}
	if len(note.Tags) < 2 {
		t.Errorf("tags: want 2, got %d: %v", len(note.Tags), note.Tags)
	}
	if !strings.Contains(note.Content, "Body here.") {
		t.Errorf("body missing: %q", note.Content)
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	raw := "# Just a heading\n\nSome content."
	note, err := ParseNote([]byte(raw), "plain.md", "/vault/plain.md", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(note.Content, "Just a heading") {
		t.Errorf("content should contain heading, got: %q", note.Content)
	}
}

func TestParseFrontmatter_NoClosingDelimiter(t *testing.T) {
	raw := "---\ntitle: Unterminated"
	note, err := ParseNote([]byte(raw), "x.md", "/vault/x.md", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Full file treated as body.
	if !strings.Contains(note.Content, "---") {
		t.Errorf("body should contain raw content, got: %q", note.Content)
	}
}

func TestParseFrontmatter_InlineTagsInFrontmatter(t *testing.T) {
	raw := "---\ntags: projects, work\n---\nBody"
	note, err := ParseNote([]byte(raw), "t.md", "/vault/t.md", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tagMap := make(map[string]bool)
	for _, tg := range note.Tags {
		tagMap[strings.ToLower(tg)] = true
	}
	for _, want := range []string{"projects", "work"} {
		if !tagMap[want] {
			t.Errorf("missing tag %q in %v", want, note.Tags)
		}
	}
}

// --- Wikilinks ---

func TestExtractWikilinks(t *testing.T) {
	body := "See [[Project Alpha]] and [[Project Beta|alias]] and [[Note#Section]]."
	links := extractWikilinks(body)
	if len(links) != 3 {
		t.Fatalf("want 3 links, got %d: %v", len(links), links)
	}
	for _, want := range []string{"Project Alpha", "Project Beta", "Note"} {
		found := false
		for _, l := range links {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing wikilink %q in %v", want, links)
		}
	}
}

func TestExtractWikilinks_Deduplication(t *testing.T) {
	body := "[[Alpha]] and [[Alpha]] again."
	links := extractWikilinks(body)
	if len(links) != 1 {
		t.Errorf("want 1 unique link, got %d: %v", len(links), links)
	}
}

func TestExtractWikilinks_Empty(t *testing.T) {
	links := extractWikilinks("No links here.")
	if len(links) != 0 {
		t.Errorf("want 0 links, got %v", links)
	}
}

// --- Inline tags ---

func TestExtractInlineTags(t *testing.T) {
	body := "This note is about #golang and #testing/unit. Also #CI-CD."
	tags := extractInlineTags(body)
	tagSet := make(map[string]bool)
	for _, tg := range tags {
		tagSet[strings.ToLower(tg)] = true
	}
	for _, want := range []string{"golang", "testing/unit"} {
		if !tagSet[want] {
			t.Errorf("missing tag %q in %v", want, tags)
		}
	}
}

// --- Attachments ---

func TestExtractAttachments(t *testing.T) {
	body := "![image](assets/img.png)\n![[embed.pdf]]\n![alt](other.jpg)"
	atts := extractAttachments(body)
	if len(atts) != 3 {
		t.Errorf("want 3 attachments, got %d: %v", len(atts), atts)
	}
}

func TestExtractAttachments_Deduplication(t *testing.T) {
	body := "![a](file.png) ![b](file.png)"
	atts := extractAttachments(body)
	if len(atts) != 1 {
		t.Errorf("want 1 unique attachment, got %d: %v", len(atts), atts)
	}
}

// --- ExternalID stability ---

func TestStableExternalID_Stable(t *testing.T) {
	id1 := stableExternalID("notes/my-note.md")
	id2 := stableExternalID("notes/my-note.md")
	if id1 != id2 {
		t.Errorf("IDs differ: %q vs %q", id1, id2)
	}
	if !strings.HasPrefix(id1, "obs-") {
		t.Errorf("want obs- prefix, got %q", id1)
	}
}

func TestStableExternalID_Distinct(t *testing.T) {
	id1 := stableExternalID("notes/a.md")
	id2 := stableExternalID("notes/b.md")
	if id1 == id2 {
		t.Errorf("distinct paths should yield distinct IDs")
	}
}

// --- ContentHash ---

func TestContentHash_Changes(t *testing.T) {
	n1, _ := ParseNote([]byte("content A"), "x.md", "/v/x.md", time.Time{})
	n2, _ := ParseNote([]byte("content B"), "x.md", "/v/x.md", time.Time{})
	if n1.ContentHash == n2.ContentHash {
		t.Error("different content should produce different hashes")
	}
}

// --- RenderContent ---

func TestRenderContent_ContainsFields(t *testing.T) {
	note := Note{
		ExternalID: "obs-abc123",
		RelPath:    "notes/hello.md",
		Title:      "Hello World",
		Content:    "Some body text.",
		Tags:       []string{"go", "test"},
		Wikilinks:  []string{"Linked Note"},
		ModTime:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	rendered := RenderContent(note)
	for _, want := range []string{
		"# Hello World",
		"Provider: Obsidian",
		"External ID: obs-abc123",
		"Vault Path: notes/hello.md",
		"Tags: go, test",
		"Links: Linked Note",
		"Some body text.",
		"2026-01-01T00:00:00Z",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered missing %q\ngot:\n%s", want, rendered)
		}
	}
}

// --- Golden fixture: typical Obsidian note ---

var goldenObsidianNote = `---
title: Daily Note 2026-01-15
tags:
  - daily
  - work
aliases:
  - "Jan 15"
---

# Daily Note 2026-01-15

Met with [[Alice]] about the [[Project Roadmap]].

#standup recap:
- Review PR from [[Bob]]
- Update [[Architecture Decisions]]

![screenshot](assets/screenshot.png)
![[design.pdf]]
`

func TestGoldenObsidianNote(t *testing.T) {
	note, err := ParseNote([]byte(goldenObsidianNote), "daily/2026-01-15.md", "/vault/daily/2026-01-15.md", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if note.Title != "Daily Note 2026-01-15" {
		t.Errorf("title: want %q, got %q", "Daily Note 2026-01-15", note.Title)
	}

	tagSet := make(map[string]bool)
	for _, tg := range note.Tags {
		tagSet[strings.ToLower(tg)] = true
	}
	for _, want := range []string{"daily", "work", "standup"} {
		if !tagSet[want] {
			t.Errorf("missing tag %q in %v", want, note.Tags)
		}
	}

	wikilinkSet := make(map[string]bool)
	for _, l := range note.Wikilinks {
		wikilinkSet[l] = true
	}
	for _, want := range []string{"Alice", "Project Roadmap", "Bob", "Architecture Decisions"} {
		if !wikilinkSet[want] {
			t.Errorf("missing wikilink %q in %v", want, note.Wikilinks)
		}
	}

	attSet := make(map[string]bool)
	for _, a := range note.Attachments {
		attSet[a] = true
	}
	for _, want := range []string{"assets/screenshot.png", "design.pdf"} {
		if !attSet[want] {
			t.Errorf("missing attachment %q in %v", want, note.Attachments)
		}
	}

	if note.ExternalID == "" {
		t.Error("ExternalID should not be empty")
	}
	if note.ContentHash == "" {
		t.Error("ContentHash should not be empty")
	}
}
