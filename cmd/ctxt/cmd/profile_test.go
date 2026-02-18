package cmd

import (
	"strings"
	"testing"
)

func TestProfileList(t *testing.T) {
	out, err := executeCommand("profile", "list")
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

func TestProfileShow(t *testing.T) {
	out, err := executeCommand("profile", "show", "founder")
	if err != nil {
		t.Fatalf("profile show should succeed: %v", err)
	}
	if !strings.Contains(out, "Profile: founder") {
		t.Error("output should show profile name")
	}
}

func TestProfileShowNoNameError(t *testing.T) {
	_, err := executeCommand("profile", "show")
	if err == nil {
		t.Error("profile show without name should fail")
	}
}

func TestProfileCreate(t *testing.T) {
	out, err := executeCommand("profile", "create", "myproject")
	if err != nil {
		t.Fatalf("profile create should succeed: %v", err)
	}
	if !strings.Contains(out, "Creating profile: myproject") {
		t.Error("output should confirm profile creation")
	}
}

func TestProfileCreateNoNameError(t *testing.T) {
	_, err := executeCommand("profile", "create")
	if err == nil {
		t.Error("profile create without name should fail")
	}
}

func TestProfileDelete(t *testing.T) {
	out, err := executeCommand("profile", "delete", "old-project")
	if err != nil {
		t.Fatalf("profile delete should succeed: %v", err)
	}
	if !strings.Contains(out, "Deleting profile: old-project") {
		t.Error("output should confirm profile deletion")
	}
}

func TestProfileSetDefault(t *testing.T) {
	out, err := executeCommand("profile", "set-default", "founder")
	if err != nil {
		t.Fatalf("profile set-default should succeed: %v", err)
	}
	if !strings.Contains(out, "Setting default profile to: founder") {
		t.Error("output should confirm default profile change")
	}
}

func TestProfileHelp(t *testing.T) {
	out, err := executeCommand("profile", "--help")
	if err != nil {
		t.Fatalf("profile --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"list", "show", "create", "delete", "set-default"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("profile help should list subcommand %q", subcmd)
		}
	}
}
