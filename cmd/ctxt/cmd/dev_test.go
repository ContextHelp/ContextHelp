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

// reindex-vectors is gone: `ctxt embeddings migrate` backfills vectors per
// model, so there is no alias left to forward. The remaining dev aliases
// and find stay signature-clean.
func TestDevReindexVectorsRemoved(t *testing.T) {
	for _, c := range devCmd.Commands() {
		if c.Name() == "reindex-vectors" {
			t.Fatal("ctxt dev reindex-vectors still registered")
		}
	}
	resetAllFlags(rootCmd)
	for _, v := range root.ValidateSignature().Violations {
		if strings.Contains(v.Path, " dev") || strings.HasSuffix(v.Path, " find") {
			t.Errorf("signature violation: %s [%s/%s] %s", v.Path, v.Check, v.Severity, v.Detail)
		}
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
