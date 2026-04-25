package cmd

import (
	"strings"
	"testing"
)

func TestSecretDeprecated(t *testing.T) {
	_, err := executeCommand("secret")
	if err == nil {
		t.Fatal("secret should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms secret") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestSecretGetDeprecated(t *testing.T) {
	_, err := executeCommand("secret", "get", "KEY")
	if err == nil {
		t.Fatal("secret get should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms secret get") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestSecretSetDeprecated(t *testing.T) {
	_, err := executeCommand("secret", "set", "KEY", "val")
	if err == nil {
		t.Fatal("secret set should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms secret set") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestSecretListDeprecated(t *testing.T) {
	_, err := executeCommand("secret", "list")
	if err == nil {
		t.Fatal("secret list should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms secret list") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}
