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

`SecretsConfig` in the config file selects one of three backends:
- `env` (default) — reads secrets from environment variables
- `age-file` — reads from an `age`-encrypted YAML file; requires `age_file` and
  `age_identity_file` config keys
- `keychain` — reads from the OS keychain (macOS Keychain / libsecret); controlled by
  `keychain_service`

All secrets config is stored locally in the config file. There is no REST admin endpoint
for secrets management. The server reads secrets at startup and when loading provider
config; changing secrets requires a server restart.

---

## Acceptance Criteria

- [ ] `secrets.backend: env` reads API keys from env vars at startup
- [ ] `secrets.backend: age-file` decrypts age-encrypted file using the identity file
- [ ] `secrets.backend: keychain` reads credentials from OS keychain service
- [ ] `secrets.age_file` path is validated at startup; missing file → startup error
- [ ] `secrets.age_identity_file` path is validated; unreadable identity → startup error
- [ ] `secrets.keychain_service` defaults to `"ctxt"` when not set
- [ ] Provider API keys sourced from secrets backend appear in provider config at runtime
- [ ] Plaintext API keys are never written to logs
- [ ] Config file with `backend: age-file` round-trips through YAML without losing fields

---

## Implementation Notes

### Config File (YAML)

```yaml
# Backend: env (default)
secrets:
  backend: env

# Backend: age-encrypted file
secrets:
  backend: age-file
  age_file: ~/.config/contexthelp/secrets.age
  age_identity_file: ~/.config/contexthelp/identity.age

# Backend: OS keychain
secrets:
  backend: keychain
  keychain_service: my-ctxt-instance
```

### Age-Encrypted Secrets File (plaintext before encryption)

```yaml
CH_OPENAI_API_KEY: sk-...
CH_ANTHROPIC_API_KEY: sk-ant-...
CH_GITHUB_TOKEN: ghp_...
```

### Environment Variables (env backend)

```bash
export CH_OPENAI_API_KEY=sk-...
export CH_ANTHROPIC_API_KEY=sk-ant-...
```

### SecretsConfig Fields

| Field | Type | Default | Description |
|---|---|---|---|
| `backend` | string | `"env"` | `"env"`, `"age-file"`, or `"keychain"` |
| `age_file` | string | `""` | Path to age-encrypted secrets YAML |
| `age_identity_file` | string | `""` | Path to age identity (private key) file |
| `keychain_service` | string | `"ctxt"` | Keychain service name |

---

## E2E Test Checklist

### Config File Fields — YAML Round-Trip
- [ ] YAML: `secrets.backend` field persists after `config.WriteBack` (env, age-file, keychain)
- [ ] YAML: `secrets.age_file` path persists through YAML marshal/unmarshal
- [ ] YAML: `secrets.age_identity_file` path persists through YAML marshal/unmarshal
- [ ] YAML: `secrets.keychain_service` persists; missing key defaults to `"ctxt"` on load

### Env Backend — Secrets Read from Environment
- [ ] Env: `secrets.backend: env` + `CH_OPENAI_API_KEY=sk-test` → provider config contains
  API key value at runtime (verify via provider factory init, not via log output)
- [ ] Env: Unset required env var → provider factory returns error or falls back gracefully
  (no panic)

### Age-File Backend — File Validation
- [ ] AgeFile: Missing `age_file` path → `config.Load` or server startup returns error
  containing "age_file" in message
- [ ] AgeFile: Unreadable `age_identity_file` → startup returns error containing
  "identity" in message
- [ ] AgeFile: Valid age-encrypted file + valid identity → secrets loaded and provider
  API key is non-empty at runtime
- [ ] AgeFile: Plaintext API key value does NOT appear in server stdout or stderr logs

### Keychain Backend — Service Name
- [ ] Keychain: `secrets.keychain_service` value is passed to keychain lookup (verify via
  mock/stub that receives correct service name)
- [ ] Keychain: Omitting `keychain_service` in YAML → value defaults to `"ctxt"` after load

### Provider Integration
- [ ] Integration: Provider configured with `${CH_OPENAI_API_KEY}` interpolation in YAML →
  actual value resolved via active secrets backend before provider init
- [ ] Integration: `dpkms serve` starts successfully with `backend: env` and valid env vars

### Security
- [ ] Security: `ctxt config` output (or any CLI command) does not print raw API key values
- [ ] Security: Config file written by `config.WriteBack` does not contain resolved secret
  values (only env-var references or backend config)

---

## Related Stories

- [US-0027](US-0027-configure-ai-provider.md) — AI provider config consumes secrets
- [US-0034](../operations/US-0034-export-and-backup-all-knowledge.md) — backup should exclude plaintext secrets
