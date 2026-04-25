package cmd

import (
	"strings"
	"testing"
)

func TestDevDeprecated(t *testing.T) {
	_, err := executeCommand("dev")
	if err == nil {
		t.Fatal("dev should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms dev") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestDevReindexVectorsDeprecated(t *testing.T) {
	_, err := executeCommand("dev", "reindex-vectors")
	if err == nil {
		t.Fatal("dev reindex-vectors should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms dev reindex-vectors") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestDevValidateRegistryDeprecated(t *testing.T) {
	_, err := executeCommand("dev", "validate-registry", "foo.yaml")
	if err == nil {
		t.Fatal("dev validate-registry should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms dev validate-registry") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestDevInitPluginDeprecated(t *testing.T) {
	_, err := executeCommand("dev", "init-plugin", "my-plugin")
	if err == nil {
		t.Fatal("dev init-plugin should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms dev init-plugin") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestDevGenDocsDeprecated(t *testing.T) {
	_, err := executeCommand("dev", "gen-docs")
	if err == nil {
		t.Fatal("dev gen-docs should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms dev gen-docs") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}
