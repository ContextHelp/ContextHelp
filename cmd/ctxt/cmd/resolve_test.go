package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func seedResolveObject(t *testing.T, db *testDB) *storage.KnowledgeObject {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:                 "obj_resolve1",
		Type:               "note",
		Pipeline:           "text.short",
		Source:             "import:obsidian:notes/signup.md",
		TextContent:        "Signup flows should use progressive disclosure.",
		Summaries:          []string{"Progressive disclosure in signup"},
		ContentHash:        "abc123def456",
		RegistryInfluences: []string{"uxpatterns"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := db.Driver.Objects().Create(context.Background(), obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	return obj
}

func seedResolveEntity(t *testing.T, db *testDB) *storage.Entity {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	ent := &storage.Entity{
		Slug:        "ui.best-practice",
		Title:       "UI Best Practice",
		Description: "High-quality UI design guidelines.",
		Namespace:   "ui",
		VersionHash: "v-hash-9",
		RegistryURL: "https://registry.uxpatterns.io",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Driver.Entities().Upsert(context.Background(), ent); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	return ent
}

func TestResolveObjectMarkdown(t *testing.T) {
	db := setupTestDB(t)
	seedResolveObject(t, db)

	out, err := db.exec("resolve", "obj_resolve1")
	if err != nil {
		t.Fatalf("resolve should succeed: %v", err)
	}
	if !strings.Contains(out, "Signup flows should use progressive disclosure.") {
		t.Errorf("markdown output should contain the object body, got:\n%s", out)
	}
}

func TestResolveObjectJSONProvenance(t *testing.T) {
	db := setupTestDB(t)
	seedResolveObject(t, db)

	out, err := db.exec("resolve", "obj_resolve1", "--format", "json")
	if err != nil {
		t.Fatalf("resolve --format json should succeed: %v", err)
	}

	var res struct {
		Ref        string `json:"ref"`
		Kind       string `json:"kind"`
		Body       string `json:"body"`
		Provenance struct {
			Source             string   `json:"source"`
			Pipeline           string   `json:"pipeline"`
			ContentHash        string   `json:"content_hash"`
			RegistryInfluences []string `json:"registry_influences"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output should be valid JSON: %v\n%s", err, out)
	}
	if res.Kind != "object" {
		t.Errorf("kind = %q, want object", res.Kind)
	}
	if res.Body == "" {
		t.Error("body should not be empty")
	}
	if res.Provenance.ContentHash != "abc123def456" {
		t.Errorf("content_hash = %q, want abc123def456", res.Provenance.ContentHash)
	}
	if res.Provenance.Source != "import:obsidian:notes/signup.md" {
		t.Errorf("source = %q", res.Provenance.Source)
	}
	if len(res.Provenance.RegistryInfluences) != 1 || res.Provenance.RegistryInfluences[0] != "uxpatterns" {
		t.Errorf("registry_influences = %v", res.Provenance.RegistryInfluences)
	}
}

func TestResolveEntityAtRef(t *testing.T) {
	db := setupTestDB(t)
	seedResolveEntity(t, db)

	out, err := db.exec("resolve", "@ui.best-practice")
	if err != nil {
		t.Fatalf("resolve @slug should succeed: %v", err)
	}
	if !strings.Contains(out, "UI Best Practice") {
		t.Errorf("markdown output should contain the entity title, got:\n%s", out)
	}
	if !strings.Contains(out, "High-quality UI design guidelines.") {
		t.Errorf("markdown output should contain the entity description, got:\n%s", out)
	}
}

func TestResolveEntityJSONProvenance(t *testing.T) {
	db := setupTestDB(t)
	seedResolveEntity(t, db)

	out, err := db.exec("resolve", "ui.best-practice", "--format", "json")
	if err != nil {
		t.Fatalf("resolve entity json should succeed: %v", err)
	}

	var res struct {
		Kind       string `json:"kind"`
		Body       string `json:"body"`
		Provenance struct {
			RegistryURL string `json:"registry_url"`
			VersionHash string `json:"version_hash"`
			Namespace   string `json:"namespace"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output should be valid JSON: %v\n%s", err, out)
	}
	if res.Kind != "entity" {
		t.Errorf("kind = %q, want entity", res.Kind)
	}
	if res.Provenance.RegistryURL != "https://registry.uxpatterns.io" {
		t.Errorf("registry_url = %q", res.Provenance.RegistryURL)
	}
	if res.Provenance.VersionHash != "v-hash-9" {
		t.Errorf("version_hash = %q", res.Provenance.VersionHash)
	}
}

func TestResolveNotFound(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("resolve", "@no.such-entity")
	if err == nil {
		t.Fatal("resolve of a missing ref should fail")
	}
}

// TestResolveObjectTagOnlyGraphFallsBackToTextContent covers the shape the
// text.short pipeline produces: a graph carrying Tag nodes but no Section
// nodes. ProjectDocument returns an empty Body and no Sections for it, so
// resolve must fall back to TextContent — RawContent is empty for
// non-web-captured objects, and emitting "" with exit 0 is silent data loss.
func TestResolveObjectTagOnlyGraphFallsBackToTextContent(t *testing.T) {
	db := setupTestDB(t)
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:          "obj_tagonly",
		Type:        "note",
		Pipeline:    "text.short",
		TextContent: "THE-REAL-BODY-TEXT",
		Graph: &storage.ObjectGraph{
			Nodes: []storage.GraphNode{
				{ID: "obj_tagonly:tag:0", NodeType: pluginapi.NodeTypeTag, Label: "signup"},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(context.Background(), obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("resolve", "obj_tagonly")
	if err != nil {
		t.Fatalf("resolve should succeed: %v", err)
	}
	if !strings.Contains(out, "THE-REAL-BODY-TEXT") {
		t.Errorf("markdown body should fall back to TextContent, got:\n%s", out)
	}

	jsonOut, err := db.exec("resolve", "obj_tagonly", "--format", "json")
	if err != nil {
		t.Fatalf("resolve json should succeed: %v", err)
	}
	var res struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &res); err != nil {
		t.Fatalf("output should be valid JSON: %v\n%s", err, jsonOut)
	}
	if res.Body != "THE-REAL-BODY-TEXT" {
		t.Errorf("json body = %q, want the TextContent fallback", res.Body)
	}
}

// TestResolveThinEntityReportsContentStatus asserts an index-only stub is
// distinguishable from a full entity with an empty body, so consumers do not
// retry an unhydratable ref forever.
func TestResolveThinEntityReportsContentStatus(t *testing.T) {
	db := setupTestDB(t)
	now := time.Now().Truncate(time.Second)
	ent := &storage.Entity{
		Slug:          "thin.one",
		Title:         "Thin One",
		ContentStatus: storage.ContentStatusThin,
		RegistryURL:   "https://registry.example.internal",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := db.Driver.Entities().Upsert(context.Background(), ent); err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	out, err := db.exec("resolve", "@thin.one", "--format", "json")
	if err != nil {
		t.Fatalf("resolve thin entity should succeed: %v", err)
	}
	var res struct {
		Body       string `json:"body"`
		Provenance struct {
			ContentStatus string `json:"content_status"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output should be valid JSON: %v\n%s", err, out)
	}
	if res.Provenance.ContentStatus != string(storage.ContentStatusThin) {
		t.Errorf("content_status = %q, want thin — an empty body must be explainable",
			res.Provenance.ContentStatus)
	}
}

// TestResolveFullEntityReportsContentStatus guards the inverse: a hydrated
// entity must not be reported as a stub.
func TestResolveFullEntityReportsContentStatus(t *testing.T) {
	db := setupTestDB(t)
	now := time.Now().Truncate(time.Second)
	ent := &storage.Entity{
		Slug:          "full.one",
		Title:         "Full One",
		Description:   "Hydrated body.",
		ContentStatus: storage.ContentStatusFull,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := db.Driver.Entities().Upsert(context.Background(), ent); err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	out, err := db.exec("resolve", "@full.one", "--format", "json")
	if err != nil {
		t.Fatalf("resolve full entity should succeed: %v", err)
	}
	var res struct {
		Provenance struct {
			ContentStatus string `json:"content_status"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output should be valid JSON: %v\n%s", err, out)
	}
	if res.Provenance.ContentStatus != string(storage.ContentStatusFull) {
		t.Errorf("content_status = %q, want full", res.Provenance.ContentStatus)
	}
}

// TestResolveUnsupportedFormatErrors asserts an unknown --format fails rather
// than silently emitting markdown under exit 0, matching show.go.
func TestResolveUnsupportedFormatErrors(t *testing.T) {
	db := setupTestDB(t)
	seedResolveObject(t, db)

	out, err := db.exec("resolve", "obj_resolve1", "--format", "yaml")
	if err == nil {
		t.Fatalf("unsupported format should fail, got output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error should name the rejected format, got: %v", err)
	}
}

// TestResolveMarkdownFormatAccepted guards the fix above from over-rejecting:
// the documented markdown spellings must still work.
func TestResolveMarkdownFormatAccepted(t *testing.T) {
	db := setupTestDB(t)
	seedResolveObject(t, db)

	for _, format := range []string{"markdown", "md", "table"} {
		out, err := db.exec("resolve", "obj_resolve1", "--format", format)
		if err != nil {
			t.Fatalf("--format %s should succeed: %v", format, err)
		}
		if !strings.Contains(out, "progressive disclosure") {
			t.Errorf("--format %s should emit the body, got:\n%s", format, out)
		}
	}
}
