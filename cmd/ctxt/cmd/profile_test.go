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

func TestProfileCreateAndView(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("profile", "create", "myproject")
	if err != nil {
		t.Fatalf("profile create should succeed: %v", err)
	}
	if !strings.Contains(out, "Created profile: myproject") {
		t.Errorf("output should confirm profile creation, got: %q", out)
	}

	out, err = db.exec("profile", "view", "myproject")
	if err != nil {
		t.Fatalf("profile view should succeed: %v", err)
	}
	if !strings.Contains(out, "Profile: myproject") {
		t.Error("output should show profile name")
	}
}

func TestProfileViewNotFound(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("profile", "view", "nonexistent")
	if err == nil {
		t.Error("profile view for nonexistent profile should fail")
	}
}

func TestProfileViewNoNameError(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("profile", "view")
	if err == nil {
		t.Error("profile view without name should fail")
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

func TestProfileRm(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("profile", "create", "old-project")
	if err != nil {
		t.Fatalf("create should succeed: %v", err)
	}

	out, err := db.exec("profile", "rm",
		"--confirm=yes", "old-project")
	if err != nil {
		t.Fatalf("profile rm should succeed: %v", err)
	}
	if !strings.Contains(out, "Removed profile: old-project") {
		t.Errorf("output should confirm profile removal, got: %q", out)
	}

	_, err = db.exec("profile", "view", "old-project")
	if err == nil {
		t.Error("profile view for removed profile should fail")
	}
}

func TestProfileSetAndUnset(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("profile", "create", "founder")
	if err != nil {
		t.Fatalf("create should succeed: %v", err)
	}

	out, err := db.exec("profile", "set", "founder")
	if err != nil {
		t.Fatalf("profile set should succeed: %v", err)
	}
	if !strings.Contains(out, "Set default profile to: founder") {
		t.Errorf("output should confirm default profile change, got: %q", out)
	}

	out, err = db.exec("profile", "view", "founder")
	if err != nil {
		t.Fatalf("view should succeed: %v", err)
	}
	if !strings.Contains(out, "(default profile)") {
		t.Error("output should indicate profile is default")
	}

	out, err = db.exec("profile", "unset", "founder")
	if err != nil {
		t.Fatalf("profile unset should succeed: %v", err)
	}
	if !strings.Contains(out, "Cleared default profile") {
		t.Errorf("output should confirm clearing default profile, got: %q", out)
	}
}

func TestProfileSetUnknownProfile(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("profile", "set", "nonexistent")
	if err == nil {
		t.Error("profile set for nonexistent profile should fail")
	}
}

// TestProfileUnsetRequiresName pins the argument-taking form: unset is
// not a no-arg "clear whatever is pinned" command.
func TestProfileUnsetRequiresName(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("profile", "unset")
	if err == nil {
		t.Error("profile unset without a profile name should fail")
	}
}

// TestProfileUnsetRefusesNonDefault is the safety property: naming a
// profile that is not currently the default is an error, not a silent
// clear of somebody else's default.
func TestProfileUnsetRefusesNonDefault(t *testing.T) {
	db := setupTestDB(t)

	if _, err := db.exec("profile", "create", "founder"); err != nil {
		t.Fatalf("create founder should succeed: %v", err)
	}
	if _, err := db.exec("profile", "create", "research"); err != nil {
		t.Fatalf("create research should succeed: %v", err)
	}
	if _, err := db.exec("profile", "set", "founder"); err != nil {
		t.Fatalf("set founder should succeed: %v", err)
	}

	_, err := db.exec("profile", "unset", "research")
	if err == nil {
		t.Fatal("unset of a non-default profile should fail")
	}
	if !strings.Contains(err.Error(), "is not the default") {
		t.Errorf("error should explain the mismatch, got: %v", err)
	}

	// The default must survive the refused unset.
	out, err := db.exec("profile", "view", "founder")
	if err != nil {
		t.Fatalf("view founder should succeed: %v", err)
	}
	if !strings.Contains(out, "(default profile)") {
		t.Error("refused unset must leave the original default in place")
	}
}

func TestProfileUnsetWhenNoDefault(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.exec("profile", "create", "founder"); err != nil {
		t.Fatalf("create should succeed: %v", err)
	}
	_, err := db.exec("profile", "unset", "founder")
	if err == nil {
		t.Error("unset with no default set should fail")
	}
}

func TestProfileHelp(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("profile", "--help")
	if err != nil {
		t.Fatalf("profile --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"list", "view", "create", "rm", "set", "unset"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("profile help should list subcommand %q", subcmd)
		}
	}
}

// TestProfileDeprecatedAliases proves every pre-rename spelling still
// works. The deprecation banner itself is a cobra concern written to
// stderr; here we only assert the behavior is unchanged.
func TestProfileDeprecatedAliases(t *testing.T) {
	db := setupTestDB(t)

	if _, err := db.exec("profile", "create", "legacy"); err != nil {
		t.Fatalf("create should succeed: %v", err)
	}

	out, err := db.exec("profile", "show", "legacy")
	if err != nil {
		t.Fatalf("deprecated 'profile show' should still succeed: %v", err)
	}
	if !strings.Contains(out, "Profile: legacy") {
		t.Errorf("deprecated show should print profile details, got: %q", out)
	}

	out, err = db.exec("profile", "default", "legacy")
	if err != nil {
		t.Fatalf("deprecated 'profile default <name>' should still succeed: %v", err)
	}
	if !strings.Contains(out, "Set default profile to: legacy") {
		t.Errorf("deprecated default should pin the profile, got: %q", out)
	}

	out, err = db.exec("profile", "default")
	if err != nil {
		t.Fatalf("deprecated 'profile default' (clear) should still succeed: %v", err)
	}
	if !strings.Contains(out, "Cleared default profile") {
		t.Errorf("deprecated bare default should clear, got: %q", out)
	}

	out, err = db.exec("profile", "delete", "--confirm=yes", "legacy")
	if err != nil {
		t.Fatalf("deprecated 'profile delete' should still succeed: %v", err)
	}
	if !strings.Contains(out, "Removed profile: legacy") {
		t.Errorf("deprecated delete should remove the profile, got: %q", out)
	}
}
