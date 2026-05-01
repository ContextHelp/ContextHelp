package cmd

import (
	"strings"
	"testing"
)

func TestProfileList(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("profile", "list")
	if err != nil {
		t.Fatalf("profile list should succeed: %v", err)
	}
	if !strings.Contains(out, "Focus Profiles:") {
		t.Error("output should contain 'Focus Profiles:'")
	}
	if !strings.Contains(out, "Default profile:") {
		t.Error("output should show default profile info")
	}
}

func TestProfileCreateAndShow(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("profile", "create", "myproject")
	if err != nil {
		t.Fatalf("profile create should succeed: %v", err)
	}
	if !strings.Contains(out, "Created profile: myproject") {
		t.Errorf("output should confirm profile creation, got: %q", out)
	}

	out, err = db.exec("profile", "show", "myproject")
	if err != nil {
		t.Fatalf("profile show should succeed: %v", err)
	}
	if !strings.Contains(out, "Profile: myproject") {
		t.Error("output should show profile name")
	}
}

func TestProfileShowNotFound(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("profile", "show", "nonexistent")
	if err == nil {
		t.Error("profile show for nonexistent profile should fail")
	}
}

func TestProfileShowNoNameError(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("profile", "show")
	if err == nil {
		t.Error("profile show without name should fail")
	}
}

func TestProfileCreateAlreadyExists(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("profile", "create", "myproject")
	if err != nil {
		t.Fatalf("first create should succeed: %v", err)
	}

	_, err = db.exec("profile", "create", "myproject")
	if err == nil {
		t.Error("creating duplicate profile should fail")
	}
}

func TestProfileDelete(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("profile", "create", "old-project")
	if err != nil {
		t.Fatalf("create should succeed: %v", err)
	}

	out, err := db.exec("profile", "delete", "old-project")
	if err != nil {
		t.Fatalf("profile delete should succeed: %v", err)
	}
	if !strings.Contains(out, "Deleted profile: old-project") {
		t.Errorf("output should confirm profile deletion, got: %q", out)
	}

	_, err = db.exec("profile", "show", "old-project")
	if err == nil {
		t.Error("profile show for deleted profile should fail")
	}
}

func TestProfileSetDefault(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("profile", "create", "founder")
	if err != nil {
		t.Fatalf("create should succeed: %v", err)
	}

	out, err := db.exec("profile", "default", "founder")
	if err != nil {
		t.Fatalf("profile default should succeed: %v", err)
	}
	if !strings.Contains(out, "Set default profile to: founder") {
		t.Errorf("output should confirm default profile change, got: %q", out)
	}

	out, err = db.exec("profile", "show", "founder")
	if err != nil {
		t.Fatalf("show should succeed: %v", err)
	}
	if !strings.Contains(out, "(default profile)") {
		t.Error("output should indicate profile is default")
	}

	out, err = db.exec("profile", "default")
	if err != nil {
		t.Fatalf("profile default (clear) should succeed: %v", err)
	}
	if !strings.Contains(out, "Cleared default profile") {
		t.Errorf("output should confirm clearing default profile, got: %q", out)
	}
}

func TestProfileHelp(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("profile", "--help")
	if err != nil {
		t.Fatalf("profile --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"list", "show", "create", "delete", "default"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("profile help should list subcommand %q", subcmd)
		}
	}
}
