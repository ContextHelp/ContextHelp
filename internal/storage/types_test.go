package storage

import (
	"encoding/json"
	"testing"
	"time"

	"hop.top/uri"
)

func TestKnowledgeObjectJSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	obj := KnowledgeObject{
		ID:          "obj-1",
		Type:        "article",
		Subtype:     "long",
		RawContent:  "hello world",
		ContentType: "text/plain",
		Metadata:    map[string]any{"key": "value"},
		Summaries:   []string{"summary one"},
		Sections:    []Section{{Title: "Intro", Content: "text", Order: 0}},
		Tags:        []Tag{{Label: "design", Weight: 1.5, Source: "auto"}},
		Mentions: []uri.URI{{Scheme: "ctxt", Space: "entity", ID: "ui/layout"}},
		Decisions:   []Decision{{Title: "Use SQLite", Status: "accepted", Impact: "high"}},
		Tasks:       []Task{{Title: "Write tests", Status: "open"}},
		Pipeline:    "text.long",
		Source:      "cli",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got KnowledgeObject
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ID != obj.ID {
		t.Errorf("ID: got %q, want %q", got.ID, obj.ID)
	}
	if got.Type != obj.Type {
		t.Errorf("Type: got %q, want %q", got.Type, obj.Type)
	}
	if got.Subtype != obj.Subtype {
		t.Errorf("Subtype: got %q, want %q", got.Subtype, obj.Subtype)
	}
	if got.RawContent != obj.RawContent {
		t.Errorf("RawContent: got %q, want %q", got.RawContent, obj.RawContent)
	}
	if len(got.Sections) != 1 || got.Sections[0].Title != "Intro" {
		t.Errorf("Sections: got %v", got.Sections)
	}
	if len(got.Tags) != 1 || got.Tags[0].Label != "design" {
		t.Errorf("Tags: got %v", got.Tags)
	}
	if len(got.Decisions) != 1 || got.Decisions[0].Title != "Use SQLite" {
		t.Errorf("Decisions: got %v", got.Decisions)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].Title != "Write tests" {
		t.Errorf("Tasks: got %v", got.Tasks)
	}
}

func TestEntityJSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	entity := Entity{
		Slug:        "ui.layout",
		Title:       "UI Layout",
		Description: "Layout patterns",
		Namespace:   "ui",
		Aliases:     []string{"layout", "ui-layout"},
		Metadata:    map[string]any{"importance": "high"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	data, err := json.Marshal(entity)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Entity
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Slug != entity.Slug {
		t.Errorf("Slug: got %q, want %q", got.Slug, entity.Slug)
	}
	if got.Namespace != entity.Namespace {
		t.Errorf("Namespace: got %q, want %q", got.Namespace, entity.Namespace)
	}
	if len(got.Aliases) != 2 {
		t.Errorf("Aliases: got %v", got.Aliases)
	}
}

func TestEdgeJSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	edge := Edge{
		ID:        "edge-1",
		FromType:  "object",
		FromID:    "obj-1",
		ToType:    "entity",
		ToID:      "ui.layout",
		EdgeType:  "mentions",
		Weight:    1.0,
		Metadata:  map[string]any{"auto": true},
		CreatedAt: now,
	}

	data, err := json.Marshal(edge)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Edge
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ID != edge.ID {
		t.Errorf("ID: got %q, want %q", got.ID, edge.ID)
	}
	if got.EdgeType != edge.EdgeType {
		t.Errorf("EdgeType: got %q, want %q", got.EdgeType, edge.EdgeType)
	}
	if got.Weight != edge.Weight {
		t.Errorf("Weight: got %v, want %v", got.Weight, edge.Weight)
	}
}

func TestDraftIsKnowledgeObject(t *testing.T) {
	var d Draft
	d.ID = "draft-1"
	d.Type = "text"

	// Draft should be usable wherever KnowledgeObject is expected.
	var obj *KnowledgeObject = &d
	if obj.ID != "draft-1" {
		t.Errorf("Draft alias broken: got %q", obj.ID)
	}
}

func TestObjectFilterZeroValue(t *testing.T) {
	var f ObjectFilter
	if f.Type != "" {
		t.Errorf("Type zero value: got %q", f.Type)
	}
	if f.Limit != 0 {
		t.Errorf("Limit zero value: got %d", f.Limit)
	}
	if f.After != nil {
		t.Errorf("After zero value: got %v", f.After)
	}
}

func TestEntityFilterZeroValue(t *testing.T) {
	var f EntityFilter
	if f.Namespace != "" {
		t.Errorf("Namespace zero value: got %q", f.Namespace)
	}
	if f.Limit != 0 {
		t.Errorf("Limit zero value: got %d", f.Limit)
	}
}

func TestTagJSONRoundTrip(t *testing.T) {
	tag := Tag{Label: "design", Weight: 2.5, Source: "auto"}

	data, err := json.Marshal(tag)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Tag
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Label != tag.Label {
		t.Errorf("Label: got %q, want %q", got.Label, tag.Label)
	}
	if got.Weight != tag.Weight {
		t.Errorf("Weight: got %v, want %v", got.Weight, tag.Weight)
	}
}

func TestSectionJSONRoundTrip(t *testing.T) {
	section := Section{Title: "Intro", Content: "Hello", Order: 0}

	data, err := json.Marshal(section)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Section
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Title != section.Title {
		t.Errorf("Title: got %q, want %q", got.Title, section.Title)
	}
	if got.Order != section.Order {
		t.Errorf("Order: got %d, want %d", got.Order, section.Order)
	}
}

func TestDecisionJSONRoundTrip(t *testing.T) {
	decision := Decision{Title: "Use chi", Status: "accepted", Impact: "medium"}

	data, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Decision
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Title != decision.Title {
		t.Errorf("Title: got %q, want %q", got.Title, decision.Title)
	}
	if got.Status != decision.Status {
		t.Errorf("Status: got %q, want %q", got.Status, decision.Status)
	}
}

func TestTaskJSONRoundTrip(t *testing.T) {
	task := Task{Title: "Write docs", Status: "open"}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Task
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Title != task.Title {
		t.Errorf("Title: got %q, want %q", got.Title, task.Title)
	}
}

func TestJobJSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	job := Job{
		ID:         "job-1",
		Type:       "ingest:text",
		Status:     JobPending,
		Payload:    "some content",
		Pipeline:   "text.short",
		Source:     "cli",
		MaxRetries: 3,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Job
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ID != job.ID {
		t.Errorf("ID: got %q, want %q", got.ID, job.ID)
	}
	if got.Status != JobPending {
		t.Errorf("Status: got %q, want %q", got.Status, JobPending)
	}
	if got.MaxRetries != 3 {
		t.Errorf("MaxRetries: got %d, want 3", got.MaxRetries)
	}
}
