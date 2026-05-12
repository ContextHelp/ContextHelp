package cmd

import (
	"strings"
	"testing"
)

func TestSchemaShowEmpty(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("profile", "create", "dev")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := db.exec("profile", "schema", "show", "dev")
	if err != nil {
		t.Fatalf("schema show: %v", err)
	}
	if !strings.Contains(out, "version 0") {
		t.Errorf("expected version 0, got: %s", out)
	}
	if !strings.Contains(out, "(none") {
		t.Error("expected empty schema indicators")
	}
}

func TestSchemaShowNotFound(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("profile", "schema", "show", "ghost")
	if err == nil {
		t.Error("schema show for nonexistent profile should fail")
	}
}

func TestSchemaAddType(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("profile", "create", "eng")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := db.exec("profile", "schema", "add-type", "eng", "incident")
	if err != nil {
		t.Fatalf("add-type: %v", err)
	}
	if !strings.Contains(out, "Added entity type") {
		t.Errorf("unexpected output: %s", out)
	}
	if !strings.Contains(out, "v1") {
		t.Error("version should be 1 after first add")
	}

	// Duplicate should fail.
	_, err = db.exec("profile", "schema", "add-type", "eng", "incident")
	if err == nil {
		t.Error("duplicate add-type should fail")
	}
}

func TestSchemaAddTopic(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("profile", "create", "research")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := db.exec("profile", "schema", "add-topic", "research", "ml")
	if err != nil {
		t.Fatalf("add-topic: %v", err)
	}
	if !strings.Contains(out, "Added topic") {
		t.Errorf("unexpected output: %s", out)
	}

	// Duplicate should fail.
	_, err = db.exec("profile", "schema", "add-topic", "research", "ml")
	if err == nil {
		t.Error("duplicate add-topic should fail")
	}
}

func TestSchemaAddRule(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("profile", "create", "ops")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := db.exec("profile", "schema", "add-rule", "ops",
		"(?i)deploy", "task")
	if err != nil {
		t.Fatalf("add-rule: %v", err)
	}
	if !strings.Contains(out, "classification rule") {
		t.Errorf("unexpected output: %s", out)
	}

	// Invalid regex should fail.
	_, err = db.exec("profile", "schema", "add-rule", "ops",
		"[invalid", "task")
	if err == nil {
		t.Error("invalid regex should fail")
	}
}

func TestSchemaRemoveType(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("profile", "create", "p1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = db.exec("profile", "schema", "add-type", "p1", "bug")
	if err != nil {
		t.Fatalf("add-type: %v", err)
	}

	out, err := db.exec("profile", "schema", "remove-type",
		"--confirm=yes", "--confirm-token=04ede4ebfbb0", "p1", "bug")
	if err != nil {
		t.Fatalf("remove-type: %v", err)
	}
	if !strings.Contains(out, "Removed entity type") {
		t.Errorf("unexpected output: %s", out)
	}

	// Removing nonexistent type should fail.
	_, err = db.exec("profile", "schema", "remove-type",
		"--confirm=yes", "--confirm-token=04ede4ebfbb0", "p1", "bug")
	if err == nil {
		t.Error("removing nonexistent type should fail")
	}
}

func TestSchemaRemoveTopic(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("profile", "create", "p2")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = db.exec("profile", "schema", "add-topic", "p2", "auth")
	if err != nil {
		t.Fatalf("add-topic: %v", err)
	}

	out, err := db.exec("profile", "schema", "remove-topic",
		"--confirm=yes", "--confirm-token=a8dd3f26f066", "p2", "auth")
	if err != nil {
		t.Fatalf("remove-topic: %v", err)
	}
	if !strings.Contains(out, "Removed topic") {
		t.Errorf("unexpected output: %s", out)
	}

	// Removing nonexistent topic should fail.
	_, err = db.exec("profile", "schema", "remove-topic",
		"--confirm=yes", "--confirm-token=a8dd3f26f066", "p2", "auth")
	if err == nil {
		t.Error("removing nonexistent topic should fail")
	}
}

func TestSchemaVersionIncrements(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("profile", "create", "v")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, _ = db.exec("profile", "schema", "add-type", "v", "a")
	_, _ = db.exec("profile", "schema", "add-type", "v", "b")
	_, _ = db.exec("profile", "schema", "add-topic", "v", "x")

	out, err := db.exec("profile", "schema", "show", "v")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(out, "version 3") {
		t.Errorf("expected version 3 after 3 mutations, got: %s", out)
	}
}

func TestSchemaHelp(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("profile", "schema", "--help")
	if err != nil {
		t.Fatalf("schema --help: %v", err)
	}
	for _, sub := range []string{
		"show", "add-type", "add-topic", "add-rule",
		"remove-type", "remove-topic", "evolve",
	} {
		if !strings.Contains(out, sub) {
			t.Errorf("help should list subcommand %q", sub)
		}
	}
}
