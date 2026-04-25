package cmd

import (
	"strings"
	"testing"
)

func TestKeyDeprecated(t *testing.T) {
	_, err := executeCommand("key")
	if err == nil {
		t.Fatal("key should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms key") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestKeyInitDeprecated(t *testing.T) {
	_, err := executeCommand("key", "init")
	if err == nil {
		t.Fatal("key init should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms key init") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestKeyRotateDeprecated(t *testing.T) {
	_, err := executeCommand("key", "rotate")
	if err == nil {
		t.Fatal("key rotate should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms key rotate") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestConfigBackupSubcommandRegistered(t *testing.T) {
	out, err := executeCommand("config", "--help")
	if err != nil {
		t.Fatalf("config --help: %v", err)
	}
	if !strings.Contains(out, "backup") {
		t.Errorf("config help should mention 'backup', got: %s", out)
	}
}

func TestConfigRestoreSubcommandRegistered(t *testing.T) {
	out, err := executeCommand("config", "--help")
	if err != nil {
		t.Fatalf("config --help: %v", err)
	}
	if !strings.Contains(out, "restore") {
		t.Errorf("config help should mention 'restore', got: %s", out)
	}
}
