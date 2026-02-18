package cmd

import (
	"strings"
	"testing"
)

func TestRegistryList(t *testing.T) {
	out, err := executeCommand("registry", "list")
	if err != nil {
		t.Fatalf("registry list should succeed: %v", err)
	}
	if !strings.Contains(out, "Registries:") {
		t.Error("output should contain registries header")
	}
	if !strings.Contains(out, "uxpatterns") {
		t.Error("output should list sample registries")
	}
}

func TestRegistryAdd(t *testing.T) {
	out, err := executeCommand("registry", "add", "testregistry", "https://test.example.com")
	if err != nil {
		t.Fatalf("registry add should succeed: %v", err)
	}
	if !strings.Contains(out, "Adding registry: testregistry") {
		t.Error("output should confirm registry name")
	}
	if !strings.Contains(out, "https://test.example.com") {
		t.Error("output should show registry URL")
	}
	if !strings.Contains(out, "added successfully") {
		t.Error("output should confirm success")
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
	if !strings.Contains(out, "Removing registry: uxpatterns") {
		t.Error("output should confirm removal")
	}
	if !strings.Contains(out, "removed successfully") {
		t.Error("output should confirm success")
	}
}

func TestRegistryRemoveNoNameError(t *testing.T) {
	_, err := executeCommand("registry", "remove")
	if err == nil {
		t.Error("registry remove without name should fail")
	}
}

func TestRegistryInfo(t *testing.T) {
	out, err := executeCommand("registry", "info", "uxpatterns")
	if err != nil {
		t.Fatalf("registry info should succeed: %v", err)
	}
	if !strings.Contains(out, "Registry: uxpatterns") {
		t.Error("output should show registry name")
	}
	if !strings.Contains(out, "Capabilities:") {
		t.Error("output should contain Capabilities section")
	}
	if !strings.Contains(out, "Statistics:") {
		t.Error("output should contain Statistics section")
	}
}

func TestRegistryInfoNoNameError(t *testing.T) {
	_, err := executeCommand("registry", "info")
	if err == nil {
		t.Error("registry info without name should fail")
	}
}

func TestRegistrySync(t *testing.T) {
	out, err := executeCommand("registry", "sync", "uxpatterns")
	if err != nil {
		t.Fatalf("registry sync should succeed: %v", err)
	}
	if !strings.Contains(out, "Syncing registry: uxpatterns") {
		t.Error("output should confirm sync")
	}
	if !strings.Contains(out, "synced successfully") {
		t.Error("output should confirm success")
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
