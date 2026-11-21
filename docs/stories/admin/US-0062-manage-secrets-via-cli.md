---
status: partial
---

# US-0062: Manage Secrets via CLI

> **Implementation status:** `dpkms secret get/set/list` built; `list --output json` returns backend config, not key names; most CLI-level behaviors (get exit codes, json output, set errors per backend) have no CLI test, only store-level tests.

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Operations](../../personas/operations.md)

---

## User Goal

As a user, I want to set, retrieve, and list secrets through the `dpkms secret` command
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

- [x] `dpkms secret set <key>` stores the secret in the active backend, reading the value from stdin (default when piped) or `--from-file <path>`
- [x] `dpkms secret set <key> --prompt` reads the value from a hidden interactive prompt (requires a TTY)
- [x] `dpkms secret set <key> <value>` is refused: positional values would leak via shell history, ps output and CI logs
- [x] `dpkms secret get KEY` prints the value for KEY to stdout
- [x] `dpkms secret list` shows the active backend and its config metadata
- [x] `dpkms secret set` with the `env` backend returns a clear error (env is read-only)
- [x] `dpkms secret set` with the `age-file` backend returns a clear error (age-file is read-only)
- [x] `dpkms secret set` with the `1password` backend returns a clear error (read-only via CLI)
- [x] `dpkms secret get KEY` exits non-zero and prints an error if the key is not found
- [x] `--output json` on `dpkms secret get` outputs `{"key": "KEY", "value": "VALUE"}`
- [ ] `dpkms secret list` with `--output json` outputs a JSON array of key names — **partial**: outputs backend config object, not key names (most backends don't support enumeration)

### Implementation Notes (as built)

- `ctxt secret list` shows backend config metadata (type, service name, vault, etc.) rather than enumerating stored keys. Key enumeration is not possible for `env`, `keychain`, or `1password` backends; `age-file` and `gh-secrets` could enumerate but this is not yet implemented.
- `set <key>` takes the value from stdin, `--from-file`, or `--prompt` (hidden input via `term.ReadPassword`). A positional value is rejected.

---

## Implementation Notes

### Command Structure

```
ctxt secret
├── set KEY           # store a secret; value from stdin, --from-file, or --prompt
├── get KEY           # retrieve and print a secret value
└── list              # list known secret names
```

### File: `cmd/dpkms/cmd/secret.go`

```go
var secretCmd = &cobra.Command{
    Use:   "secret",
    Short: "Manage secrets in the configured backend",
}

var secretSetCmd = &cobra.Command{
    Use:   "set <key>",
    Short: "Set a secret value (read from stdin, --from-file, or --prompt)",
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

### Value Input

`set` reads the value from stdin (default when stdin is not a TTY), `--from-file <path>` (`-` for stdin), or `--prompt` (hidden input, TTY required). `--from-file` and `--prompt` are mutually exclusive.

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
- [ ] `dpkms secret set MYKEY` (value on stdin) with `env` backend → exits non-zero with ErrNotSupported
- [ ] `dpkms secret set MYKEY` (value on stdin) with `keychain` backend → key readable via `security find-generic-password`
- [ ] `dpkms secret get MISSING` → exits non-zero, error contains key name
- [ ] `dpkms secret get KEY --output json` → `{"key":"KEY","value":"..."}` on stdout
- [ ] `dpkms secret list` with `age-file` → lists all keys from decrypted YAML
- [ ] `dpkms secret list` with `gh-secrets` → output includes names from `gh secret list`
- [ ] `dpkms secret set MYKEY --prompt` → interactive prompt without echo

### Security
- [ ] `--prompt` input does not echo characters to terminal
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
- Platform Engineer
- [Operations](../../personas/operations.md)

---

## E2E Tests

- [test/integration/us0062_secrets_cli_test.go](../../../test/integration/us0062_secrets_cli_test.go) — env, gh-secrets fallback, validation
- [test/integration/us0062_secrets_agefile_e2e_test.go](../../../test/integration/us0062_secrets_agefile_e2e_test.go) — age round-trip, list, sentinel errors
- [test/integration/us0062_secrets_keychain_e2e_test.go](../../../test/integration/us0062_secrets_keychain_e2e_test.go) — OS keyring round-trip, overwrite (skip when no keyring)
