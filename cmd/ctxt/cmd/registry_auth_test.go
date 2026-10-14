package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// TestRegistryTokenBoundToLoginOrigin: a config that later points the
// logged-in name at another origin (a project-local layer, say) gets no
// token.
func TestRegistryTokenBoundToLoginOrigin(t *testing.T) {
	genuine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(storage.RegistryManifest{Name: "team-cli", Version: "1.0.0"})
	}))
	defer genuine.Close()
	var mu sync.Mutex
	var leaked []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a := r.Header.Get("Authorization"); a != "" {
			mu.Lock()
			leaked = append(leaked, r.URL.Path)
			mu.Unlock()
		}
		_ = json.NewEncoder(w).Encode(storage.RegistryManifest{Name: "team-cli", Version: "1.0.0"})
	}))
	defer other.Close()

	db := setupTestDB(t)
	base, err := os.ReadFile(db.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	withRegistry(t, db, "team-cli", genuine.URL, "bearer")
	if _, err := db.exec("registry", "login", "team-cli", "--token", fakeRegistryToken); err != nil {
		t.Fatalf("registry login: %v", err)
	}

	// Same name, another origin.
	if err := os.WriteFile(db.ConfigPath, base, 0o600); err != nil {
		t.Fatal(err)
	}
	withRegistry(t, db, "team-cli", other.URL, "bearer")
	if _, err := db.exec("registry", "sync", "--dry-run", "team-cli"); err != nil {
		t.Fatalf("sync against the redefined url: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(leaked) != 0 {
		t.Errorf("token sent to an origin it was not stored for: %v", leaked)
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

// withProjectRegistry puts a registries list naming name at url in a
// project `.contexthelp/ctxt.yaml` and runs the rest of the test from
// that project, as a cloned repository would.
func withProjectRegistry(t *testing.T, db *testDB, name, url string) string {
	t.Helper()
	repo := filepath.Join(filepath.Dir(db.ConfigPath), "repo")
	project := filepath.Join(repo, ".contexthelp", "ctxt.yaml")
	if err := os.MkdirAll(filepath.Dir(project), 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("registries:\n  - name: %s\n    url: %s\n    auth:\n      type: bearer\n", name, url)
	if err := os.WriteFile(project, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	t.Cleanup(func() { _ = registry.NewTokenStore().Delete(name) })
	return project
}

// setLoginTerminal makes `registry login` see an interactive (or not)
// stdin that yields input.
func setLoginTerminal(t *testing.T, interactive bool, input string) {
	t.Helper()
	prevIn, prevTTY := loginInput, loginInteractive
	loginInput = strings.NewReader(input)
	loginInteractive = func() bool { return interactive }
	t.Cleanup(func() { loginInput, loginInteractive = prevIn, prevTTY })
}

func assertNoStoredToken(t *testing.T, name string) {
	t.Helper()
	tok, err := registry.NewTokenStore().Get(name)
	var none registry.ErrNoToken
	if !errors.As(err, &none) {
		t.Fatalf("token stored for %q (origin %q), want none (err = %v)", name, tok.Origin, err)
	}
}

func assertStoredOrigin(t *testing.T, name, origin string) {
	t.Helper()
	tok, err := registry.NewTokenStore().Get(name)
	if err != nil {
		t.Fatalf("stored token for %q: %v", name, err)
	}
	if tok.Origin != origin || tok.Token != fakeRegistryToken {
		t.Fatalf("stored token for %q bound to %q (token matches: %v), want %q",
			name, tok.Origin, tok.Token == fakeRegistryToken, origin)
	}
}

const projectRegistryOrigin = "https://registry.attacker.test"

func TestRegistryLoginProjectRegistryRefusedNonInteractive(t *testing.T) {
	db := setupTestDB(t)
	project := withProjectRegistry(t, db, "team-cli", projectRegistryOrigin+"/index")
	setLoginTerminal(t, false, "")

	_, err := db.exec("registry", "login", "team-cli", "--token", fakeRegistryToken)
	if err == nil {
		t.Fatal("login bound a token to a project-config registry without confirmation")
	}
	for _, want := range []string{"project config", filepath.Base(filepath.Dir(project)), "--origin " + projectRegistryOrigin} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q lacks %q", err, want)
		}
	}
	assertNoStoredToken(t, "team-cli")
}

func TestRegistryLoginProjectRegistryOriginFlag(t *testing.T) {
	db := setupTestDB(t)
	withProjectRegistry(t, db, "team-cli", projectRegistryOrigin+"/index")
	setLoginTerminal(t, false, "")

	_, err := db.exec("registry", "login", "team-cli", "--token", fakeRegistryToken,
		"--origin", "https://registry.example.com")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("login with a mismatching --origin: err = %v, want mismatch", err)
	}
	assertNoStoredToken(t, "team-cli")

	out, err := db.exec("registry", "login", "team-cli", "--token", fakeRegistryToken,
		"--origin", "HTTPS://Registry.Attacker.test:443")
	if err != nil {
		t.Fatalf("login with the matching --origin: %v", err)
	}
	if !strings.Contains(out, "token bound to "+projectRegistryOrigin) {
		t.Errorf("login output lacks the bound origin: %q", out)
	}
	assertStoredOrigin(t, "team-cli", projectRegistryOrigin)
}

func TestRegistryLoginProjectRegistryPrompt(t *testing.T) {
	db := setupTestDB(t)
	withProjectRegistry(t, db, "team-cli", projectRegistryOrigin+"/index")

	setLoginTerminal(t, true, "n\n")
	out, err := db.exec("registry", "login", "team-cli", "--token", fakeRegistryToken)
	if err == nil || !strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("declined prompt: err = %v, want not confirmed", err)
	}
	if !strings.Contains(out, "Bind the token to "+projectRegistryOrigin+"?") {
		t.Errorf("prompt does not name the origin: %q", out)
	}
	assertNoStoredToken(t, "team-cli")

	// Confirmation and the prompted token share stdin.
	setLoginTerminal(t, true, "y\n"+fakeRegistryToken+"\n")
	out, err = db.exec("registry", "login", "team-cli")
	if err != nil {
		t.Fatalf("confirmed prompt: %v", err)
	}
	if !strings.Contains(out, "token bound to "+projectRegistryOrigin) {
		t.Errorf("login output lacks the bound origin: %q", out)
	}
	assertStoredOrigin(t, "team-cli", projectRegistryOrigin)
}

// A registry from the user's own config (here the --config file) needs
// no confirmation, even non-interactively.
func TestRegistryLoginOwnConfigPrintsOrigin(t *testing.T) {
	db := setupTestDB(t)
	withRegistry(t, db, "own-cli", "https://Registry.Example.com:443/cli", "bearer")
	setLoginTerminal(t, false, "")

	out, err := db.exec("registry", "login", "own-cli", "--token", fakeRegistryToken)
	if err != nil {
		t.Fatalf("registry login: %v", err)
	}
	if !strings.Contains(out, "token bound to https://registry.example.com") {
		t.Errorf("login output lacks the bound origin: %q", out)
	}
	assertStoredOrigin(t, "own-cli", "https://registry.example.com")
}
