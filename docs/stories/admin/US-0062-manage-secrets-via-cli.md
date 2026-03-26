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

The `internal/secrets` package provides a `Resolver` interface backed by five configurable
backends (`env`, `keychain`, `age-file`, `1password`, `gh-secrets`). Users currently have no
CLI surface to interact with secrets — they must use backend-specific tools (`security`,
`op`, `gh`) directly.

`ctxt secret` wraps the active backend's `Get()` and `Set()` methods behind a single,
backend-agnostic command. The active backend is determined by `secrets.backend` in the
config file.

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

### Resolver Construction

```go
resolver, err := secrets.NewResolver(cfg.Secrets)
```

Use the resolver directly — `Set()` writes to the backend, `Get()` reads from it.

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
- Backend init failure: surface the error from `secrets.NewResolver()`

---

## E2E Test Checklist

### `ctxt secret set`
- [ ] `ctxt secret set MYKEY myvalue` with `keychain` backend → key readable via `security find-generic-password`
- [ ] `ctxt secret set MYKEY myvalue` with `gh-secrets` backend → `gh secret list` shows MYKEY
- [ ] `ctxt secret set MYKEY` (no value) → interactive prompt appears, value accepted without echo
- [ ] `ctxt secret set MYKEY value` with `env` backend → exits non-zero with error about read-only
- [ ] `ctxt secret set MYKEY value` with `age-file` backend → exits non-zero with error about read-only

### `ctxt secret get`
- [ ] `ctxt secret get KEY` with `env` backend + env var set → prints value to stdout
- [ ] `ctxt secret get KEY` with `keychain` backend + key stored → prints value
- [ ] `ctxt secret get MISSING` → exits non-zero, error contains key name
- [ ] `ctxt secret get KEY --json` → `{"key":"KEY","value":"..."}` on stdout

### `ctxt secret list`
- [ ] `ctxt secret list` with `gh-secrets` backend → output includes names from `gh secret list`
- [ ] `ctxt secret list` with `age-file` backend → lists all key names from decrypted YAML
- [ ] `ctxt secret list --json` → JSON array of key name strings
- [ ] `ctxt secret list` with `keychain` or `1password` backend → human-readable guidance message, exits 0

### Security
- [ ] Interactive input prompt does not echo characters to terminal
- [ ] Secret value is never logged or printed except by explicit `ctxt secret get`
- [ ] `ctxt secret get` output is the bare value with a trailing newline (suitable for `$(ctxt secret get KEY)`)

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

> Not yet implemented.
