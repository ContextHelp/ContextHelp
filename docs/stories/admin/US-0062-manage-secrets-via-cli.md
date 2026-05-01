---
status: shipped
---

# US-0062: Manage Secrets via CLI

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Operations](../../personas/operations.md)

---

## User Goal

As a user, I want to set, retrieve, and list secrets through the `ctxt secret` command
so I can manage API keys without editing config files or typing raw credentials into a
shell export.

---

## Context

The `internal/secrets` package is a thin factory that translates ctxt's
`SecretsConfig.Backend` to a [kit](https://github.com/hop-top/kit)
`secret.Store` (or `MutableStore` when writes are supported). Five backends are
wired: `env`, `keychain` (kit `keyring`), `age-file` (kit `agefile`),
`1password` (kit `onepassword`), `gh-secrets` (kit `ghsecrets`). Users have no
need to invoke backend-specific tools (`security`, `op`, `gh`) directly —
`dpkms secret get/set/list` wraps the active store.

The active backend is determined by `secrets.backend` in the config file.
`ctxt secret` is deprecated; commands now live under `dpkms secret`.

---

## Acceptance Criteria

- [x] `ctxt secret set KEY VALUE` stores the secret in the active backend
- [ ] `ctxt secret set KEY` (no value) prompts for the value interactively (hidden input) — **deferred**
- [x] `ctxt secret get KEY` prints the value for KEY to stdout
- [x] `ctxt secret list` shows the active backend and its config metadata
- [x] `ctxt secret set` with the `env` backend returns a clear error (env is read-only)
- [x] `ctxt secret set` with the `age-file` backend returns a clear error (age-file is read-only)
- [x] `ctxt secret set` with the `1password` backend returns a clear error (read-only via CLI)
- [x] `ctxt secret get KEY` exits non-zero and prints an error if the key is not found
- [x] `--output json` on `ctxt secret get` outputs `{"key": "KEY", "value": "VALUE"}`
- [ ] `ctxt secret list` with `--output json` outputs a JSON array of key names — **partial**: outputs backend config object, not key names (most backends don't support enumeration)

### Implementation Notes (as built)

- `ctxt secret list` shows backend config metadata (type, service name, vault, etc.) rather than enumerating stored keys. Key enumeration is not possible for `env`, `keychain`, or `1password` backends; `age-file` and `gh-secrets` could enumerate but this is not yet implemented.
- Interactive `set KEY` (no value argument) is deferred. The current implementation requires `KEY VALUE` as two positional args. Interactive hidden-input prompt can be added later using `charmbracelet/huh`.

---

## Implementation Notes

### Command Structure

```
ctxt secret
├── set KEY [VALUE]   # store a secret; prompts if VALUE omitted
├── get KEY           # retrieve and print a secret value
└── list              # list known secret names
```

### File: `cmd/ctxt/cmd/secret.go`

```go
var secretCmd = &cobra.Command{
    Use:   "secret",
    Short: "Manage secrets in the configured backend",
}

var secretSetCmd = &cobra.Command{
    Use:   "set KEY [VALUE]",
    Short: "Store a secret in the active backend",
    RunE:  runSecretSet,
}

var secretGetCmd = &cobra.Command{
    Use:   "get KEY",
    Short: "Retrieve a secret from the active backend",
    RunE:  runSecretGet,
}

var secretListCmd = &cobra.Command{
    Use:   "list",
    Short: "List known secret names",
    RunE:  runSecretList,
}
```

### Interactive Input

When `ctxt secret set KEY` is called without a value, use `huh.NewInput().EchoMode(huh.EchoModePassword)` (charmbracelet/huh, already in go.mod) to prompt for the value securely.

### Store Construction

```go
store, err := secrets.New(cfg.Secrets) // returns kit secret.MutableStore
got, err := store.Get(ctx, key)        // *secret.Secret with Value []byte
err := store.Set(ctx, key, []byte(v)) // ErrNotSupported on read-only backends
```

The factory is a thin shim over `kit/go/storage/secret.Open()`; backend
implementations live in kit (one source of truth across hop.top tools).

### `list` Command

Not all backends support enumeration. Return a static informational message for backends that don't:

| Backend | `list` behaviour |
|---------|-----------------|
| `env` | Prints env vars matching a known-key list (OPENAI_API_KEY, ANTHROPIC_API_KEY, etc.) |
| `keychain` | Not enumerable — prints guidance to use OS tools |
| `age-file` | Decrypts and lists all keys in the YAML map |
| `1password` | Not enumerable via `op read` alone — prints guidance |
| `gh-secrets` | Calls `gh secret list [--repo owner/repo]` |

### Error Messages

- Read-only backend: `"secrets: <Backend> backend does not support Set(); <guidance>"`
- Key not found: `"secrets: key \"KEY\" not found"`
- Backend init failure: surface the error from `secrets.New()`

---

## E2E Test Checklist

### Backend round-trip (in-process; no CLI subprocess)
- [x] `env` Set → ErrNotSupported; Get → reads `os.Getenv`; missing key → ErrNotFound
  ([us0062_secrets_cli_test.go](../../../test/integration/us0062_secrets_cli_test.go))
- [x] `keychain` Set → Get → Delete → Get returns ErrNotFound (skipped when host has no keyring)
  ([us0062_secrets_keychain_e2e_test.go](../../../test/integration/us0062_secrets_keychain_e2e_test.go))
- [x] `keychain` Set on existing key overwrites silently
  ([us0062_secrets_keychain_e2e_test.go](../../../test/integration/us0062_secrets_keychain_e2e_test.go))
- [x] `age-file` decrypts a real age-encrypted YAML; Get returns the bare value;
      missing key → ErrNotFound; Set → ErrNotSupported; List enumerates all keys
  ([us0062_secrets_agefile_e2e_test.go](../../../test/integration/us0062_secrets_agefile_e2e_test.go))
- [x] `gh-secrets` Get falls back to env when `gh` CLI not invoked
  ([us0062_secrets_cli_test.go](../../../test/integration/us0062_secrets_cli_test.go))
- [ ] `1password` Get via real `op` CLI (deferred; needs CI vault)

### CLI behaviour (subprocess `dpkms secret …`)
- [ ] `dpkms secret set MYKEY value` with `env` backend → exits non-zero with ErrNotSupported
- [ ] `dpkms secret set MYKEY value` with `keychain` backend → key readable via `security find-generic-password`
- [ ] `dpkms secret get MISSING` → exits non-zero, error contains key name
- [ ] `dpkms secret get KEY --output json` → `{"key":"KEY","value":"..."}` on stdout
- [ ] `dpkms secret list` with `age-file` → lists all keys from decrypted YAML
- [ ] `dpkms secret list` with `gh-secrets` → output includes names from `gh secret list`
- [ ] `dpkms secret set MYKEY` (no value) → interactive prompt without echo (deferred)

### Security
- [ ] Interactive input prompt does not echo characters to terminal (deferred)
- [x] Secret value is never logged or printed except by explicit `dpkms secret get`
- [x] `dpkms secret get` output is the bare value with a trailing newline
  (suitable for `$(dpkms secret get KEY)`)

---

## Related Stories

- [US-0031](US-0031-configure-encryption-and-secrets.md) — configure the secrets backend
- [US-0027](US-0027-configure-ai-provider.md) — AI provider config consumes secrets

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Engineer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/platform-engineer.md)
- [Operations](../../personas/operations.md)

---

## E2E Tests

- [test/integration/us0062_secrets_cli_test.go](../../../test/integration/us0062_secrets_cli_test.go) — env, gh-secrets fallback, validation
- [test/integration/us0062_secrets_agefile_e2e_test.go](../../../test/integration/us0062_secrets_agefile_e2e_test.go) — age round-trip, list, sentinel errors
- [test/integration/us0062_secrets_keychain_e2e_test.go](../../../test/integration/us0062_secrets_keychain_e2e_test.go) — OS keyring round-trip, overwrite (skip when no keyring)
