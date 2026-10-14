package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// fakeRegistryToken is a fixed, non-secret value stored in the in-memory
// keychain testguard installs; failures may print it.
const fakeRegistryToken = "fake-registry-token"

// withRegistry appends a registries entry to db's config file.
func withRegistry(t *testing.T, db *testDB, name, url, authType string) {
	t.Helper()
	base, err := os.ReadFile(db.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	body := string(base) + fmt.Sprintf("registries:\n  - name: %s\n    url: %s\n", name, url)
	if authType != "" {
		body += "    auth:\n      type: " + authType + "\n"
	}
	if err := os.WriteFile(db.ConfigPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Cleanup(func() { _ = registry.NewTokenStore().Delete(name) })
}

// TestRegistryLoginTokenReachesRegistry drives the real wiring: the token
// `registry login` stores is the one `registry sync` sends.
func TestRegistryLoginTokenReachesRegistry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fakeRegistryToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(storage.RegistryManifest{Name: "paid-cli", Version: "1.0.0"})
	}))
	defer srv.Close()

	db := setupTestDB(t)
	withRegistry(t, db, "paid-cli", srv.URL, "bearer")

	_, err := db.exec("registry", "sync", "--dry-run", "paid-cli")
	if err == nil || !strings.Contains(err.Error(), "ctxt registry login paid-cli") {
		t.Fatalf("sync before login: err = %v, want login hint", err)
	}

	if _, err := db.exec("registry", "login", "paid-cli", "--token", fakeRegistryToken); err != nil {
		t.Fatalf("registry login: %v", err)
	}
	if _, err := db.exec("registry", "sync", "--dry-run", "paid-cli"); err != nil {
		t.Fatalf("sync after login: %v", err)
	}
}

func TestRegistryLoginWithoutAuthTypeNotes(t *testing.T) {
	db := setupTestDB(t)
	withRegistry(t, db, "public-cli", "http://127.0.0.1:1", "")

	out, err := db.exec("registry", "login", "public-cli", "--token", fakeRegistryToken)
	if err != nil {
		t.Fatalf("registry login: %v", err)
	}
	if !strings.Contains(out, "has no auth.type") {
		t.Errorf("login output lacks the auth.type note: %q", out)
	}
}
