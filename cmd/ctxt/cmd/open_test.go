package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"hop.top/uri"
)

func TestOpen(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:        "obj_12345678",
		Type:      "url",
		Subtype:   "article",
		Pipeline:  "url.article",
		Source:    "https://example.com/ux-signup",
		Tags:      []storage.Tag{{Label: "ux"}, {Label: "onboarding"}},
		MentionURIs: []uri.URI{
			{Scheme: "ctxt", Space: "entity", ID: "ui/best-practice"},
			{Scheme: "ctxt", Space: "entity", ID: "ux/onboarding"},
		},
		Summaries: []string{"Best UX practices for signup flows"},
		Decisions: []storage.Decision{{Title: "Use progressive disclosure", Status: "accepted", Impact: "high"}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("open", "obj_12345678")
	if err != nil {
		t.Fatalf("open should succeed: %v", err)
	}
	if !strings.Contains(out, "Knowledge Object: obj_12345678") {
		t.Error("output should show object ID")
	}
	if !strings.Contains(out, "Type:") {
		t.Error("output should contain Type field")
	}
	if !strings.Contains(out, "Tags:") {
		t.Error("output should contain Tags field")
	}
	if !strings.Contains(out, "Mentions:") {
		t.Error("output should contain Mentions field")
	}
	if !strings.Contains(out, "Summary:") {
		t.Error("output should contain Summary section")
	}
	if !strings.Contains(out, "Decisions:") {
		t.Error("output should contain Decisions section")
	}
}

func TestOpenNoIDError(t *testing.T) {
	_, err := executeCommand("open")
	if err == nil {
		t.Error("open without ID should fail")
	}
}

func TestOpenHelp(t *testing.T) {
	out, err := executeCommand("open", "--help")
	if err != nil {
		t.Fatalf("open --help should succeed: %v", err)
	}
	if !strings.Contains(out, "--raw") {
		t.Error("open help should list --raw flag")
	}
}
