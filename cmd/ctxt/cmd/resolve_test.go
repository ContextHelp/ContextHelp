package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
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
