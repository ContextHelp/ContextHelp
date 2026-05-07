package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestComposeBrief(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_make_1",
		Type:      "text",
		Summaries: []string{"Summary of best practices"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("compose", "brief")
	if err != nil {
		t.Fatalf("make brief should succeed: %v", err)
	}
	if !strings.Contains(out, "# brief") {
		t.Error("output should contain composition heading")
	}
	if !strings.Contains(out, "obj_make_1") {
		t.Error("output should reference the source object")
	}
}

func TestComposePlan(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_plan_1",
		Type:      "text",
		Summaries: []string{"Plan content"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("compose", "plan")
	if err != nil {
		t.Fatalf("make plan should succeed: %v", err)
	}
	if !strings.Contains(out, "# plan") {
		t.Error("output should contain plan heading")
	}
}

func TestComposeNoObjects(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("compose", "summary")
	if err != nil {
		t.Fatalf("make with no objects should succeed: %v", err)
	}
	if !strings.Contains(out, "No matching objects found") {
		t.Error("output should indicate no matching objects")
	}
}

func TestComposeUnknownTypeError(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("compose", "invalid_type")
	if err == nil {
		t.Error("make with unknown type should fail")
	}
}

func TestComposeNoTypeError(t *testing.T) {
	_, err := executeCommand("compose")
	if err == nil {
		t.Error("make without type argument should fail")
	}
}

func TestComposeHelp(t *testing.T) {
	out, err := executeCommand("compose", "--help")
	if err != nil {
		t.Fatalf("make --help should succeed: %v", err)
	}
	for _, flag := range []string{"--mention", "--tagged", "--since", "-o"} {
		if !strings.Contains(out, flag) {
			t.Errorf("make help should list flag %s", flag)
		}
	}
	for _, typ := range []string{"brief", "plan", "summary", "draft"} {
		if !strings.Contains(out, typ) {
			t.Errorf("make help should mention type %q", typ)
		}
	}
}
