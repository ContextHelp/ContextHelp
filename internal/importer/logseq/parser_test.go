package logseq

import (
	"strings"
	"testing"
	"time"
)

// --- Page classification ---

func TestClassifyPage_Journal(t *testing.T) {
	kind, title, date := classifyPage("journals/2026_01_15.md")
	if kind != PageKindJournal {
		t.Errorf("kind: want journal, got %q", kind)
	}
	if date != "2026-01-15" {
		t.Errorf("date: want 2026-01-15, got %q", date)
	}
	if title != "2026-01-15" {
		t.Errorf("title: want 2026-01-15, got %q", title)
	}
}

func TestClassifyPage_JournalDashFormat(t *testing.T) {
	kind, _, date := classifyPage("journals/2026-03-13.md")
	if kind != PageKindJournal {
		t.Errorf("kind: want journal, got %q", kind)
	}
	if date != "2026-03-13" {
		t.Errorf("date: want 2026-03-13, got %q", date)
	}
}

func TestClassifyPage_Page(t *testing.T) {
	kind, title, date := classifyPage("pages/project-alpha.md")
	if kind != PageKindPage {
		t.Errorf("kind: want page, got %q", kind)
	}
	if date != "" {
		t.Errorf("date: want empty, got %q", date)
	}
	if title != "project-alpha" {
		t.Errorf("title: want project-alpha, got %q", title)
	}
}

func TestClassifyPage_PageEncodedSlash(t *testing.T) {
	_, title, _ := classifyPage("pages/foo___bar.md")
	if title != "foo/bar" {
		t.Errorf("title: want foo/bar, got %q", title)
	}
}

// --- Block parsing ---

func TestParseBlocks_Simple(t *testing.T) {
	raw := "- Block one\n- Block two\n"
	blocks := parseBlocks(raw)
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Content != "Block one" {
		t.Errorf("block 0: want %q, got %q", "Block one", blocks[0].Content)
	}
}

func TestParseBlocks_NestedChildren(t *testing.T) {
	raw := "- Parent\n  - Child one\n  - Child two\n"
	blocks := parseBlocks(raw)
	if len(blocks) != 1 {
		t.Fatalf("want 1 top-level block, got %d", len(blocks))
	}
	if len(blocks[0].Children) != 2 {
		t.Errorf("want 2 children, got %d", len(blocks[0].Children))
	}
}

func TestParseBlocks_Properties(t *testing.T) {
	raw := "- id:: abc123\n  content here\n"
	blocks := parseBlocks(raw)
	if len(blocks) == 0 {
		t.Fatal("no blocks parsed")
	}
	if blocks[0].UUID != "abc123" {
		t.Errorf("uuid: want abc123, got %q", blocks[0].UUID)
	}
}

func TestParseBlocks_Empty(t *testing.T) {
	blocks := parseBlocks("")
	if len(blocks) != 0 {
		t.Errorf("want 0 blocks, got %d", len(blocks))
	}
}

// --- Page refs ---

func TestExtractPageRefs(t *testing.T) {
	raw := "Met with [[Alice]] and [[Bob]]. See [[Project Roadmap]]."
	refs := extractPageRefs(raw)
	refSet := make(map[string]bool)
	for _, r := range refs {
		refSet[r] = true
	}
	for _, want := range []string{"Alice", "Bob", "Project Roadmap"} {
		if !refSet[want] {
			t.Errorf("missing ref %q in %v", want, refs)
		}
	}
}

func TestExtractPageRefs_Deduplication(t *testing.T) {
	raw := "[[Alpha]] and [[Alpha]] again."
	refs := extractPageRefs(raw)
	if len(refs) != 1 {
		t.Errorf("want 1 unique ref, got %d: %v", len(refs), refs)
	}
}

// --- Tags ---

func TestExtractPageTags_InlineAndProperty(t *testing.T) {
	props := map[string]string{"tags": "work, projects"}
	tags := extractPageTags("Body #standup recap", props)
	tagSet := make(map[string]bool)
	for _, tg := range tags {
		tagSet[strings.ToLower(tg)] = true
	}
	for _, want := range []string{"work", "projects", "standup"} {
		if !tagSet[want] {
			t.Errorf("missing tag %q in %v", want, tags)
		}
	}
}

// --- ExternalID stability ---

func TestStableExternalID_Stable(t *testing.T) {
	id1 := stableExternalID("journals/2026_01_15.md")
	id2 := stableExternalID("journals/2026_01_15.md")
	if id1 != id2 {
		t.Errorf("IDs differ: %q vs %q", id1, id2)
	}
	if !strings.HasPrefix(id1, "lsq-") {
		t.Errorf("want lsq- prefix, got %q", id1)
	}
}

func TestStableExternalID_Distinct(t *testing.T) {
	id1 := stableExternalID("pages/a.md")
	id2 := stableExternalID("pages/b.md")
	if id1 == id2 {
		t.Errorf("distinct paths should yield distinct IDs")
	}
}

// --- RenderContent ---

func TestRenderContent_ContainsFields(t *testing.T) {
	page := Page{
		ExternalID:  "lsq-abc123",
		RelPath:     "journals/2026-01-15.md",
		Title:       "2026-01-15",
		Kind:        PageKindJournal,
		JournalDate: "2026-01-15",
		Tags:        []string{"work"},
		PageRefs:    []string{"Alice"},
		Blocks:      []*Block{{Content: "Met with Alice about roadmap."}},
		ModTime:     time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
	}
	rendered := RenderContent(page)
	for _, want := range []string{
		"# 2026-01-15",
		"Provider: Logseq",
		"Kind: journal",
		"External ID: lsq-abc123",
		"Journal Date: 2026-01-15",
		"Tags: work",
		"References: Alice",
		"Met with Alice about roadmap.",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered missing %q\ngot:\n%s", want, rendered)
		}
	}
}

// --- Golden fixture: typical Logseq journal ---

var goldenLogseqJournal = `- type:: journal
  date:: 2026-01-15
- Standup #standup
  - Reviewed PR from [[Bob]]
  - Updated [[Architecture Decisions]]
  - id:: block-uuid-001
- Meeting with [[Alice]] about [[Project Roadmap]]
  - Decision: move to weekly cadence
  - tags:: decision, planning
`

func TestGoldenLogseqJournal(t *testing.T) {
	page, err := ParsePage([]byte(goldenLogseqJournal), "journals/2026_01_15.md", "/graph/journals/2026_01_15.md", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if page.Kind != PageKindJournal {
		t.Errorf("kind: want journal, got %q", page.Kind)
	}
	if page.JournalDate != "2026-01-15" {
		t.Errorf("journal date: want 2026-01-15, got %q", page.JournalDate)
	}

	refSet := make(map[string]bool)
	for _, r := range page.PageRefs {
		refSet[r] = true
	}
	for _, want := range []string{"Bob", "Architecture Decisions", "Alice", "Project Roadmap"} {
		if !refSet[want] {
			t.Errorf("missing ref %q in %v", want, page.PageRefs)
		}
	}

	if page.ExternalID == "" {
		t.Error("ExternalID should not be empty")
	}
	if page.ContentHash == "" {
		t.Error("ContentHash should not be empty")
	}
}

// --- Golden fixture: Logseq page ---

var goldenLogseqPage = `- title:: Project Alpha
  tags:: work, engineering
- ## Goals
  - Ship MVP by Q2
  - Integrate with [[dPKMS]]
- ## Risks
  - Scope creep
  - Dependency on [[External API]]
`

func TestGoldenLogseqPage(t *testing.T) {
	page, err := ParsePage([]byte(goldenLogseqPage), "pages/project-alpha.md", "/graph/pages/project-alpha.md", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if page.Kind != PageKindPage {
		t.Errorf("kind: want page, got %q", page.Kind)
	}
	// Title overridden from page properties.
	if page.Title != "Project Alpha" {
		t.Errorf("title: want %q, got %q", "Project Alpha", page.Title)
	}

	tagSet := make(map[string]bool)
	for _, tg := range page.Tags {
		tagSet[strings.ToLower(tg)] = true
	}
	for _, want := range []string{"work", "engineering"} {
		if !tagSet[want] {
			t.Errorf("missing tag %q in %v", want, page.Tags)
		}
	}

	refSet := make(map[string]bool)
	for _, r := range page.PageRefs {
		refSet[r] = true
	}
	for _, want := range []string{"dPKMS", "External API"} {
		if !refSet[want] {
			t.Errorf("missing ref %q in %v", want, page.PageRefs)
		}
	}
}

// --- Idempotency: parse twice, same hash ---

func TestParsePage_Idempotent(t *testing.T) {
	data := []byte(goldenLogseqPage)
	p1, err := ParsePage(data, "pages/proj.md", "/g/pages/proj.md", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := ParsePage(data, "pages/proj.md", "/g/pages/proj.md", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if p1.ContentHash != p2.ContentHash {
		t.Error("same data should yield same ContentHash")
	}
	if p1.ExternalID != p2.ExternalID {
		t.Error("same path should yield same ExternalID")
	}
}
