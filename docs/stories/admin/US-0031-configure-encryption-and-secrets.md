# US-0031: Configure Encryption And Secrets

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Operations](../../personas/operations.md),
[Maintainers](../../personas/maintainers.md)

---

## User Goal

As an operator or maintainer, I want to store API keys and sensitive credentials using a
secrets backend so they are never written in plaintext to the config file.

---

## Context

`SecretsConfig` in the config file selects one of five backends via `secrets.backend`:

| Backend | Description |
|---------|-------------|
| `env` (default) | Reads from environment variables |
| `keychain` | OS keychain — macOS `security(1)` or Linux `secret-tool` |
| `age-file` | `age`-encrypted YAML file on disk |
| `1password` | 1Password vault via `op` CLI |
| `gh-secrets` | GitHub Actions secrets via `gh` CLI (write-only; `Get()` falls back to env) |

All backends implement the same `secrets.Resolver` interface (`Get(key) string`, `Set(key, value)`).
The providers factory uses the active resolver transparently — callers never read `os.Getenv` directly.

All secrets config is stored locally in the config file. There is no REST admin endpoint
for secrets management. The server reads secrets at startup; changing backends requires a restart.

See [docs/security/secrets-backends.md](../../security/secrets-backends.md) for setup instructions.

---

## Acceptance Criteria

- [x] `secrets.backend: env` reads API keys from env vars at startup
- [x] `secrets.backend: keychain` reads credentials from OS keychain service
- [x] `secrets.backend: age-file` decrypts age-encrypted file using the identity file
- [x] `secrets.backend: 1password` reads secrets via `op read op://Vault/Key/password`
- [x] `secrets.backend: gh-secrets` falls back to env for `Get()`; uses `gh secret set` for `Set()`
- [x] `secrets.age_file` required when backend is `age-file`; missing → error containing "age_file"
- [x] `secrets.age_identity_file` required when backend is `age-file`; unreadable → error containing "age_identity_file"
- [x] `secrets.keychain_service` defaults to `"ctxt"` when not set
- [x] `secrets.onepassword_vault` required when backend is `1password`; missing → error containing "vault"
- [x] `secrets.gh_repo` is optional for `gh-secrets`; empty means current repo
- [x] Provider API keys sourced from secrets backend appear in provider config at runtime
- [x] Plaintext API keys are never written to logs
- [x] Config file with any backend round-trips through YAML without losing fields
- [x] `secrets.backend` with unknown value → validation error listing valid options

---

## Implementation Notes

### Config File (YAML)

```yaml
# Backend: env (default)
secrets:
  backend: env

# Backend: OS keychain
secrets:
  backend: keychain
  keychain_service: ctxt       # optional, defaults to "ctxt"

# Backend: age-encrypted file
secrets:
  backend: age-file
  age_file: ~/.config/contexthelp/secrets.age
  age_identity_file: ~/.config/contexthelp/identity.txt

# Backend: 1Password
secrets:
  backend: 1password
  onepassword_vault: MyVault   # required

# Backend: GitHub Actions secrets (write-only; Get falls back to env)
secrets:
  backend: gh-secrets
  gh_repo: owner/repo          # optional
```

### SecretsConfig Fields

| Field | Type | Default | Description |
|---|---|---|---|
| `backend` | string | `"env"` | One of `env`, `keychain`, `age-file`, `1password`, `gh-secrets` |
| `age_file` | string | `""` | Path to age-encrypted secrets YAML |
| `age_identity_file` | string | `""` | Path to age identity (private key) file |
| `keychain_service` | string | `"ctxt"` | Keychain service name |
| `onepassword_vault` | string | `""` | 1Password vault name |
| `gh_repo` | string | `""` | GitHub repo (`owner/repo`); empty = current repo |

### Implementation

- `internal/secrets/resolver.go` — `Resolver` interface, `EnvResolver`, `ErrNotFound`
- `internal/secrets/keychain.go` — `KeychainResolver` (build tag `darwin || linux`)
- `internal/secrets/agefile.go` — `AgeFileResolver` (uses `filippo.io/age`)
- `internal/secrets/onepassword.go` — `OnePasswordResolver` (shells to `op`)
- `internal/secrets/ghsecrets.go` — `GHSecretsResolver` (shells to `gh`)
- `internal/secrets/factory.go` — `NewResolver(cfg SecretsConfig) (Resolver, error)`
- `internal/providers/factory.go` — `NewFactory(cfg, resolver)` wires resolver; `apiKey()` replaces `os.Getenv`

---

## E2E Test Checklist

### Config File Fields — YAML Round-Trip
- [ ] YAML: `secrets.backend` field persists after `config.WriteBack` for all five backends
- [ ] YAML: `secrets.age_file` and `secrets.age_identity_file` paths persist through marshal/unmarshal
- [ ] YAML: `secrets.keychain_service` persists; missing key defaults to `"ctxt"` on load
- [ ] YAML: `secrets.onepassword_vault` persists through marshal/unmarshal
- [ ] YAML: `secrets.gh_repo` persists; empty string round-trips correctly

### Env Backend
- [ ] Env: `OPENAI_API_KEY=sk-test` → provider factory receives the value
- [ ] Env: Unset env var → factory falls back gracefully (no panic)

### Keychain Backend
- [ ] Keychain: `keychain_service` value is passed to the lookup command
- [ ] Keychain: Omitting `keychain_service` → defaults to `"ctxt"`

### Age-File Backend
- [ ] AgeFile: Missing `age_file` → error containing `"age_file"`
- [ ] AgeFile: Missing `age_identity_file` → error containing `"age_identity_file"`
- [ ] AgeFile: Valid encrypted file + valid identity → provider API key non-empty at runtime
- [ ] AgeFile: Plaintext key value does NOT appear in server stdout/stderr

### 1Password Backend
- [ ] 1Password: Missing `onepassword_vault` → error containing `"vault"`
- [ ] 1Password: `op read op://Vault/KEY/password` called with correct vault and key
- [ ] 1Password: `Set()` returns error (read-only)

### gh-secrets Backend
- [ ] GH: `Get()` returns env var value when set
- [ ] GH: `Get()` returns error when env var unset
- [ ] GH: `Set()` invokes `gh secret set KEY --body VALUE [--repo owner/repo]`
- [ ] GH: Empty `gh_repo` → no `--repo` flag passed to `gh`

### Provider Integration
- [ ] Integration: `dpkms serve` starts successfully with `backend: env` and valid env vars
- [ ] Integration: Provider factory `LLM()` auto-detection uses resolver, not bare `os.Getenv`

### Security
- [ ] Security: `ctxt config show` does not print raw API key values
- [ ] Security: `config.WriteBack` does not write resolved secret values to disk

---

## Related Stories

- [US-0027](US-0027-configure-ai-provider.md) — AI provider config consumes secrets
- [US-0062](US-0062-manage-secrets-via-cli.md) — `ctxt secret set/get/list` CLI commands
- [US-0034](../operations/US-0034-export-and-backup-all-knowledge.md) — backup should exclude plaintext secrets
