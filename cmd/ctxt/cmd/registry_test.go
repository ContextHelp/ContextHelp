package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// --- login / logout tests ---

func TestRegistryLoginNoArgsError(t *testing.T) {
	_, err := executeCommand("registry", "login")
	if err == nil {
		t.Error("registry login without name should fail")
	}
}

func TestRegistryLoginUnknownRegistry(t *testing.T) {
	_, err := executeCommand("registry", "login", "nonexistent-registry")
	if err == nil {
		t.Error("registry login for unknown registry should fail")
	}
	if !strings.Contains(err.Error(), "not found in config") {
		t.Errorf("expected 'not found in config' error, got: %v", err)
	}
}

func TestRegistryLoginHelp(t *testing.T) {
	out, err := executeCommand("registry", "login", "--help")
	if err != nil {
		t.Fatalf("registry login --help should succeed: %v", err)
	}
	if !strings.Contains(out, "--token") {
		t.Error("registry login help should mention --token flag")
	}
}

func TestRegistryLogoutNoArgsError(t *testing.T) {
	_, err := executeCommand("registry", "logout")
	if err == nil {
		t.Error("registry logout without name should fail")
	}
}

func TestRegistryLogoutNotStored(t *testing.T) {
	_, err := executeCommand("registry", "logout", "nonexistent-registry-logout")
	if err == nil {
		t.Error("registry logout for unknown token should fail")
	}
}

func TestRegistryHelpShowsLoginLogout(t *testing.T) {
	out, err := executeCommand("registry", "--help")
	if err != nil {
		t.Fatalf("registry --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"login", "logout"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("registry help should list subcommand %q", subcmd)
		}
	}
}

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
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("ETag", `"v1"`)
		_ = json.NewEncoder(w).Encode(storage.RegistryManifest{Name: "testregistry", Version: "1.2.0"})
	}))
	defer srv.Close()
	db := setupTestDB(t)

	out, err := db.exec("registry", "add", "testregistry", srv.URL)
	if err != nil {
		t.Fatalf("registry add: %v", err)
	}
	if !strings.Contains(out, "Adding registry testregistry") {
		t.Errorf("output should confirm registry name, got: %q", out)
	}
	if hits.Load() == 0 {
		t.Fatal("registry add made no request to the registry")
	}
	cache, err := db.Driver.Registries().GetCachedManifest(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("cached manifest: %v", err)
	}
	if cache.Manifest == nil || cache.Manifest.Version != "1.2.0" || cache.ETag != `"v1"` {
		t.Errorf("cached manifest = %+v, want version 1.2.0 with ETag \"v1\"", cache)
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
	db := setupTestDB(t)
	_, err := db.exec("registry", "delete",
		"--confirm=yes", "--confirm-token=8b18e794cfb0", "nonexistent")
	if err == nil {
		t.Error("registry delete for unknown registry should fail")
	}
}

func TestRegistryRemoveNoNameError(t *testing.T) {
	_, err := executeCommand("registry", "delete")
	if err == nil {
		t.Error("registry delete without name should fail")
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
	for _, subcmd := range []string{"list", "add", "delete", "info", "sync", "submit"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("registry help should list subcommand %q", subcmd)
		}
	}
}

func TestRegistrySubmitMissingBundle(t *testing.T) {
	_, err := executeCommand("registry", "submit", "/nonexistent/bundle/path")
	if err == nil {
		t.Fatal("submit with missing bundle should fail")
	}
	if !strings.Contains(err.Error(), "CTXT-4004") {
		t.Errorf("expected CTXT-4004 code, got: %v", err)
	}
}

func TestRegistrySubmitNoArgs(t *testing.T) {
	_, err := executeCommand("registry", "submit")
	if err == nil {
		t.Error("registry submit with no args should fail")
	}
}

func TestRegistrySubmitValidDir(t *testing.T) {
	dir := t.TempDir()
	// Write a manifest.json so validation passes.
	if werr := os.WriteFile(dir+"/manifest.json", []byte(`{"name":"test"}`), 0o600); werr != nil {
		t.Fatalf("write manifest: %v", werr)
	}
	out, err := executeCommand("registry", "submit", dir)
	if err != nil {
		t.Fatalf("submit with valid dir should succeed: %v", err)
	}
	if !strings.Contains(out, "github.com/ideacrafterslabs/registry") {
		t.Errorf("output should include registry URL, got: %s", out)
	}
}

// --- dry-run tests ---

func TestRegistrySyncDryRunFlagExists(t *testing.T) {
	out, err := executeCommand("registry", "sync", "--help")
	if err != nil {
		t.Fatalf("registry sync --help should succeed: %v", err)
	}
	if !strings.Contains(out, "dry-run") {
		t.Error("registry sync --help should document --dry-run flag")
	}
}

func TestRegistrySyncDryRunUnknownRegistry(t *testing.T) {
	// dry-run on unknown registry → "not found in config" before any network call
	_, err := executeCommand("registry", "sync", "--dry-run", "nonexistent")
	if err == nil {
		t.Error("registry sync --dry-run for nonexistent registry should fail")
	}
	if !strings.Contains(err.Error(), "not found in config") {
		t.Errorf("expected 'not found in config' error, got: %v", err)
	}
}

func TestRegistrySyncDryRunNoArgsError(t *testing.T) {
	_, err := executeCommand("registry", "sync", "--dry-run")
	if err == nil {
		t.Error("registry sync --dry-run without name should fail")
	}
}

// syncTestRegistry serves a manifest advertising capabilities.entity_sync
// as entitySync, an entity index at /entities/index and 404 elsewhere,
// and counts /entities/index requests.
func syncTestRegistry(t *testing.T, entitySync bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var indexHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/", "":
			_ = json.NewEncoder(w).Encode(storage.RegistryManifest{
				Name: "sync-cli", Version: "1.0.0",
				Capabilities: storage.RegistryCapabilities{EntitySync: entitySync},
			})
		case "/entities/index":
			indexHits.Add(1)
			_, _ = w.Write([]byte(`[{"slug":"ai.bert","title":"BERT","namespace":"ai","version_hash":"v1"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &indexHits
}

// TestRegistrySyncPullsAdvertisedEntities: `registry sync` follows the
// manifest to the entity index when capabilities.entity_sync is set.
func TestRegistrySyncPullsAdvertisedEntities(t *testing.T) {
	srv, indexHits := syncTestRegistry(t, true)
	db := setupTestDB(t)
	withRegistry(t, db, "sync-cli", srv.URL, "")
	// An untrusted registry's entities queue for approval instead of
	// landing; trust this one so the count is observable.
	appendConfig(t, db, "    trust_level: trusted\n")

	out, err := db.exec("registry", "sync", "sync-cli")
	if err != nil {
		t.Fatalf("registry sync: %v\n%s", err, out)
	}
	if indexHits.Load() == 0 {
		t.Errorf("sync never requested /entities/index; output:\n%s", out)
	}
	if !strings.Contains(out, "Entities synced: 1") {
		t.Errorf("output lacks the entity count:\n%s", out)
	}
}

// TestRegistrySyncNotTrustedSaysEntitiesHeld: a registry that is not
// trusted gets its entities refused, and sync says how many and why
// instead of a bare "Entities synced: 0"; it still exits 0.
func TestRegistrySyncNotTrustedSaysEntitiesHeld(t *testing.T) {
	for _, tc := range []struct {
		name, trustYAML string
		want            []string
	}{
		{"unset", "", []string{
			"Entities not written: 1", "trust_level unset, so untrusted",
			"approval", "trust_level: trusted",
		}},
		{"untrusted", "    trust_level: untrusted\n", []string{
			"Entities not written: 1", "trust_level untrusted",
			"approval", "trust_level: trusted",
		}},
		{"sandboxed", "    trust_level: sandboxed\n", []string{
			"Entities not written: 1", "trust_level sandboxed",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, indexHits := syncTestRegistry(t, true)
			db := setupTestDB(t)
			withRegistry(t, db, "sync-cli", srv.URL, "")
			if tc.trustYAML != "" {
				appendConfig(t, db, tc.trustYAML)
			}

			out, err := db.exec("registry", "sync", "sync-cli")
			if err != nil {
				t.Fatalf("registry sync: %v\n%s", err, out)
			}
			if indexHits.Load() == 0 {
				t.Fatalf("sync never requested /entities/index; output:\n%s", out)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
		})
	}
}

// TestRegistryListHelpSaysCachedManifests: list reads the manifest
// cache (what add and sync fetched), not the config's registries list,
// and its help says so.
func TestRegistryListHelpSaysCachedManifests(t *testing.T) {
	help, err := executeCommand("registry", "list", "--help")
	if err != nil {
		t.Fatalf("registry list --help: %v", err)
	}
	if strings.Contains(help, "every configured registry") {
		t.Errorf("help claims list shows configured registries:\n%s", help)
	}
	if !strings.Contains(help, "cached") || !strings.Contains(help, "registries list") {
		t.Errorf("help does not say list shows cached manifests, not the config:\n%s", help)
	}
}

// TestRegistrySyncStepsOnlySkipsEntities: no capabilities.entity_sync, no
// /entities/index request, and the output says so.
func TestRegistrySyncStepsOnlySkipsEntities(t *testing.T) {
	srv, indexHits := syncTestRegistry(t, false)
	db := setupTestDB(t)
	withRegistry(t, db, "sync-cli", srv.URL, "")

	out, err := db.exec("registry", "sync", "sync-cli")
	if err != nil {
		t.Fatalf("registry sync: %v\n%s", err, out)
	}
	if n := indexHits.Load(); n != 0 {
		t.Errorf("steps-only registry asked for /entities/index %d times", n)
	}
	if !strings.Contains(out, "capabilities.entity_sync") {
		t.Errorf("output does not explain the skipped entities:\n%s", out)
	}
}

// TestRegistryAddLeavesConfigAndSaysSo: add caches the manifest but does
// not write the config file; its help and output say how to subscribe.
func TestRegistryAddLeavesConfigAndSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(storage.RegistryManifest{Name: "unlisted", Version: "1.0.0"})
	}))
	defer srv.Close()
	db := setupTestDB(t)
	before, err := os.ReadFile(db.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}

	out, err := db.exec("registry", "add", "unlisted", srv.URL)
	if err != nil {
		t.Fatalf("registry add: %v", err)
	}
	after, err := os.ReadFile(db.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("registry add changed the config file:\n%s", after)
	}
	if !strings.Contains(out, "Not in config") || !strings.Contains(out, "ctxt config edit") {
		t.Errorf("add output does not say the registry is unconfigured:\n%s", out)
	}

	help, err := executeCommand("registry", "add", "--help")
	if err != nil {
		t.Fatalf("registry add --help: %v", err)
	}
	if strings.Contains(help, "record the named subscription in the config file") {
		t.Errorf("help still claims add writes the config file:\n%s", help)
	}
	if !strings.Contains(help, "does not write the config file") || !strings.Contains(help, "registries:") {
		t.Errorf("help does not say how to subscribe:\n%s", help)
	}
}

// TestRegistryAddConfiguredHasNoNote: a registry already in the config
// gets no subscribe note.
func TestRegistryAddConfiguredHasNoNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(storage.RegistryManifest{Name: "listed", Version: "1.0.0"})
	}))
	defer srv.Close()
	db := setupTestDB(t)
	withRegistry(t, db, "listed", srv.URL, "")

	out, err := db.exec("registry", "add", "listed", srv.URL)
	if err != nil {
		t.Fatalf("registry add: %v", err)
	}
	if strings.Contains(out, "Not in config") {
		t.Errorf("configured registry got the subscribe note:\n%s", out)
	}
}
