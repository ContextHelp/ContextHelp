package cmd

import (
	"strings"
	"testing"
)

func TestRegistryList(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("registry", "list")
	if err != nil {
		t.Fatalf("registry list should succeed: %v", err)
	}
	if !strings.Contains(out, "Registries") {
		t.Error("output should contain registries header")
	}
}

func TestRegistryAdd(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("registry", "add", "testregistry", "https://test.example.com")
	if err != nil {
		// Fetch may fail in test environment (network), that's expected
		if !strings.Contains(err.Error(), "fetch registry") {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if !strings.Contains(out, "Adding registry testregistry") {
		t.Error("output should confirm registry name")
	}
}

func TestRegistryAddMissingArgsError(t *testing.T) {
	_, err := executeCommand("registry", "add", "only-name")
	if err == nil {
		t.Error("registry add with only name should fail (needs URL)")
	}
}

func TestRegistryAddNoArgsError(t *testing.T) {
	_, err := executeCommand("registry", "add")
	if err == nil {
		t.Error("registry add with no args should fail")
	}
}

func TestRegistryRemove(t *testing.T) {
	out, err := executeCommand("registry", "remove", "uxpatterns")
	if err != nil {
		t.Fatalf("registry remove should succeed: %v", err)
	}
	if !strings.Contains(out, "Registry removal not yet implemented") {
		t.Error("output should indicate not yet implemented")
	}
}

func TestRegistryRemoveNoNameError(t *testing.T) {
	_, err := executeCommand("registry", "remove")
	if err == nil {
		t.Error("registry remove without name should fail")
	}
}

func TestRegistryInfo(t *testing.T) {
	// Info requires the registry name to exist in config; without it, expect an error
	_, err := executeCommand("registry", "info", "nonexistent")
	if err == nil {
		t.Error("registry info for nonexistent registry should fail")
	}
	if !strings.Contains(err.Error(), "not found in config") {
		t.Errorf("expected 'not found in config' error, got: %v", err)
	}
}

func TestRegistryInfoNoNameError(t *testing.T) {
	_, err := executeCommand("registry", "info")
	if err == nil {
		t.Error("registry info without name should fail")
	}
}

func TestRegistrySync(t *testing.T) {
	// Sync requires the registry name to exist in config; without it, expect an error
	_, err := executeCommand("registry", "sync", "nonexistent")
	if err == nil {
		t.Error("registry sync for nonexistent registry should fail")
	}
	if !strings.Contains(err.Error(), "not found in config") {
		t.Errorf("expected 'not found in config' error, got: %v", err)
	}
}

func TestRegistrySyncNoNameError(t *testing.T) {
	_, err := executeCommand("registry", "sync")
	if err == nil {
		t.Error("registry sync without name should fail")
	}
}

func TestRegistryHelp(t *testing.T) {
	out, err := executeCommand("registry", "--help")
	if err != nil {
		t.Fatalf("registry --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"list", "add", "remove", "info", "sync"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("registry help should list subcommand %q", subcmd)
		}
	}
}
