# Secret CLI Commands Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `ctxt secret set <key> <value>`, `ctxt secret get <key>`, and `ctxt secret list` subcommands that delegate to the configured `secrets.Resolver`.

**Architecture:** A new `cmd/ctxt/cmd/secret.go` file registers a `secret` group command with three subcommands. Each subcommand calls `secrets.NewResolver(cfg.Secrets)` to get the configured backend, then calls `Get`/`Set` on it. `secret list` shows the active backend and its config (keys cannot be enumerated in most backends — we show metadata instead).

**Tech Stack:** Go, cobra, `internal/secrets` package (already exists), `internal/config.SecretsConfig` (already exists)

---

## Background: Existing Code

- `internal/secrets/resolver.go` — `Resolver` interface: `Get(key) (string, error)`, `Set(key, value) error`
- `internal/secrets/factory.go` — `NewResolver(cfg config.SecretsConfig) (Resolver, error)` — selects backend from `cfg.Backend` (`"env"`, `"keychain"`, `"age-file"`, `"1password"`, `"gh-secrets"`)
- `internal/config/config.go:497` — `SecretsConfig` struct
- `cmd/ctxt/cmd/profile.go` — reference command pattern (group cmd + subcommands, reads `cfg`)
- `cmd/ctxt/cmd/testhelpers_test.go` — `setupTestDB(t)` returns `*testDB`; `db.exec(args...)` prepends `--config`
- `cmd/ctxt/cmd/root_test.go` — `executeCommand(args...)` (used for commands that don't need a DB)

## Key Design Decisions

- `secret set` takes value as arg (not stdin) for simplicity; help text notes shell history risk.
- `secret list` shows backend config metadata — the `Resolver` interface has no `List()` method.
- `env` backend's `Set()` always errors (read-only) — this is expected and the error message is descriptive.
- JSON output is supported for `get` and `list`.
- `root_test.go:TestRootSubcommands` must include `"secret"`.

---

### Task 1: Write the failing tests

**Files:**
- Create: `cmd/ctxt/cmd/secret_test.go`

**Step 1: Write the test file**

```go
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
```

**Step 2: Run tests to confirm they all fail**

```bash
go test ./cmd/ctxt/cmd/ -run TestSecret -v -timeout 30s
```

Expected: FAIL — `unknown command "secret"`

---

### Task 2: Implement `cmd/ctxt/cmd/secret.go`

**Files:**
- Create: `cmd/ctxt/cmd/secret.go`

**Step 1: Write the implementation**

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	"github.com/spf13/cobra"
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage secrets",
	Long: `Read and write secrets from the configured backend.

The active backend is set via secrets.backend in your config file.
Supported backends: env, keychain, age-file, 1password, gh-secrets.

Examples:
  # Get a secret
  ctxt secret get OPENAI_API_KEY

  # Set a secret (backend must support writes)
  ctxt secret set OPENAI_API_KEY sk-...

  # Show active backend config
  ctxt secret list

Note: passing secrets as command-line arguments exposes them in shell
history. For sensitive values prefer setting them via your backend's
native tooling (e.g. security add-generic-password for keychain).`,
}

var secretGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a secret value",
	Args:  cobra.ExactArgs(1),
	RunE:  runSecretGet,
}

var secretSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a secret value",
	Args:  cobra.ExactArgs(2),
	RunE:  runSecretSet,
}

var secretListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show active secrets backend configuration",
	RunE:  runSecretList,
}

func init() {
	rootCmd.AddCommand(secretCmd)
	secretCmd.AddCommand(secretGetCmd)
	secretCmd.AddCommand(secretSetCmd)
	secretCmd.AddCommand(secretListCmd)
}

func runSecretGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	r, err := secrets.NewResolver(cfg.Secrets)
	if err != nil {
		return fmt.Errorf("secret get: %w", err)
	}

	value, err := r.Get(key)
	if err != nil {
		return err
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]string{"key": key, "value": value})
	}
	fmt.Fprintln(cmd.OutOrStdout(), value)
	return nil
}

func runSecretSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]

	r, err := secrets.NewResolver(cfg.Secrets)
	if err != nil {
		return fmt.Errorf("secret set: %w", err)
	}

	if err := r.Set(key, value); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Set %s\n", key)
	return nil
}

func runSecretList(cmd *cobra.Command, args []string) error {
	backend := cfg.Secrets.Backend
	if backend == "" {
		backend = "env"
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"backend":           backend,
			"keychain_service":  cfg.Secrets.KeychainService,
			"age_file":          cfg.Secrets.AgeFile,
			"age_identity_file": cfg.Secrets.AgeIdentityFile,
			"onepassword_vault": cfg.Secrets.OnePasswordVault,
			"gh_repo":           cfg.Secrets.GHRepo,
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Secrets backend: %s\n", backend)
	switch backend {
	case "keychain":
		svc := cfg.Secrets.KeychainService
		if svc == "" {
			svc = "ctxt"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  service: %s\n", svc)
	case "age-file":
		fmt.Fprintf(cmd.OutOrStdout(), "  age_file: %s\n", cfg.Secrets.AgeFile)
		fmt.Fprintf(cmd.OutOrStdout(), "  identity_file: %s\n", cfg.Secrets.AgeIdentityFile)
	case "1password":
		fmt.Fprintf(cmd.OutOrStdout(), "  vault: %s\n", cfg.Secrets.OnePasswordVault)
	case "gh-secrets":
		repo := cfg.Secrets.GHRepo
		if repo == "" {
			repo = "(current repo)"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  repo: %s\n", repo)
	}
	return nil
}
```

**Step 2: Run tests**

```bash
go test ./cmd/ctxt/cmd/ -run TestSecret -v -timeout 30s
```

Expected: all PASS

---

### Task 3: Update `root_test.go` subcommands list

**Files:**
- Modify: `cmd/ctxt/cmd/root_test.go`

**Step 1: Add `"secret"` to the subcommands slice in `TestRootSubcommands`**

Find:
```go
for _, subcmd := range []string{"analyze", "import", "list", "find", "open", "delete", "edit", "make", "config", "profile", "job", "entity", "registry", "completion"} {
```

Replace with:
```go
for _, subcmd := range []string{"analyze", "import", "list", "find", "open", "delete", "edit", "make", "config", "profile", "job", "entity", "registry", "secret", "completion"} {
```

**Step 2: Run root tests**

```bash
go test ./cmd/ctxt/cmd/ -run TestRoot -v -timeout 30s
```

Expected: all PASS

---

### Task 4: Build check and smoke test

**Step 1: Build**

```bash
go build ./cmd/ctxt/...
```

Expected: no errors

**Step 2: Smoke test**

```bash
./ctxt secret --help
CTXT_TEST_SMOKE=hello ./ctxt secret get CTXT_TEST_SMOKE
./ctxt secret list
```

Expected: help shows `get`, `set`, `list`; get prints `hello`; list shows `Secrets backend: env`

**Step 3: Clean up binary**

```bash
rm -f ctxt
```

---

### Task 5: Commit

```bash
git add cmd/ctxt/cmd/secret.go cmd/ctxt/cmd/secret_test.go cmd/ctxt/cmd/root_test.go
git commit -m "feat(cli): add ctxt secret get/set/list commands

Refs: tlc/T-0045"
```
