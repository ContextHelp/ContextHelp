package cmd

import (
	"strings"
	"testing"
)

func TestConfigShow(t *testing.T) {
	out, err := executeCommand("config", "show")
	if err != nil {
		t.Fatalf("config show should succeed: %v", err)
	}
	if !strings.Contains(out, "Configuration:") {
		t.Error("output should contain 'Configuration:'")
	}
	if !strings.Contains(out, "storage:") {
		t.Error("output should contain storage section")
	}
	if !strings.Contains(out, "server:") {
		t.Error("output should contain server section")
	}
	if !strings.Contains(out, "profile:") {
		t.Error("output should contain profile section")
	}
}

func TestConfigPath(t *testing.T) {
	out, err := executeCommand("config", "path")
	if err != nil {
		t.Fatalf("config path should succeed: %v", err)
	}
	if !strings.Contains(out, "config.yaml") {
		t.Error("output should contain config filename")
	}
}

func TestConfigValidate(t *testing.T) {
	out, err := executeCommand("config", "validate")
	if err != nil {
		t.Fatalf("config validate should succeed: %v", err)
	}
	if !strings.Contains(out, "Configuration is valid") {
		t.Error("output should confirm valid configuration")
	}
}

func TestConfigHelp(t *testing.T) {
	out, err := executeCommand("config", "--help")
	if err != nil {
		t.Fatalf("config --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"show", "path", "validate", "edit"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("config help should list subcommand %q", subcmd)
		}
	}
}
