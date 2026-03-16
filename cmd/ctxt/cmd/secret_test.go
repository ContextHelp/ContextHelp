package cmd

import (
	"strings"
	"testing"
)

func TestSecretHelp(t *testing.T) {
	out, err := executeCommand("secret", "--help")
	if err != nil {
		t.Fatalf("secret --help should succeed: %v", err)
	}
	for _, sub := range []string{"get", "set", "list"} {
		if !strings.Contains(out, sub) {
			t.Errorf("secret help should list subcommand %q", sub)
		}
	}
}

func TestSecretSetEnvReadOnly(t *testing.T) {
	_, err := executeCommand("secret", "set", "KEY", "val")
	// env backend is read-only, Set() always returns an error
	if err == nil {
		t.Error("secret set with env backend should fail (read-only)")
	}
}

func TestSecretGetMissing(t *testing.T) {
	_, err := executeCommand("secret", "get", "CTXT_TEST_NONEXISTENT_KEY_XYZ")
	if err == nil {
		t.Error("secret get for missing env var should fail")
	}
}

func TestSecretGetFound(t *testing.T) {
	t.Setenv("CTXT_TEST_KEY", "hello")

	out, err := executeCommand("secret", "get", "CTXT_TEST_KEY")
	if err != nil {
		t.Fatalf("secret get should succeed: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("secret get should print value, got: %q", out)
	}
}

func TestSecretList(t *testing.T) {
	out, err := executeCommand("secret", "list")
	if err != nil {
		t.Fatalf("secret list should succeed: %v", err)
	}
	if !strings.Contains(out, "backend") && !strings.Contains(out, "Backend") {
		t.Errorf("secret list should show backend info, got: %q", out)
	}
}
