# Secrets Backends

ctxt abstracts secret storage behind a `secrets.backend` config key. Instead of reading API keys directly from environment variables, you configure a backend once and the system resolves secrets through it transparently.

## Quick Start

Add a `secrets` section to `~/.config/contexthelp/config.yaml`:

```yaml
secrets:
  backend: env  # default — reads from environment variables
```

All backends expose the same interface: they resolve named keys (e.g. `OPENAI_API_KEY`) to their values. The providers factory calls the backend automatically when selecting AI providers.

---

## Backends

### `env` (default)

Reads secrets from environment variables. No configuration required.

```yaml
secrets:
  backend: env
```

Set secrets the usual way before running ctxt:

```bash
export OPENAI_API_KEY=sk-...
export ANTHROPIC_API_KEY=sk-ant-...
ctxt analyze myfile.md
```

`Set()` is not supported — the env backend is read-only.

**`ctxt secret list`:** Key enumeration not supported. Shows backend name only. To see what env vars are set, inspect your shell environment directly.

---

### `keychain`

Reads secrets from the OS keychain. Uses `security find-generic-password` on macOS and `secret-tool lookup` on Linux.

```yaml
secrets:
  backend: keychain
  keychain_service: ctxt  # optional, defaults to "ctxt"
```

**Store a secret (macOS):**

```bash
security add-generic-password -s ctxt -a OPENAI_API_KEY -w "sk-..." -U
```

**Store a secret (Linux, requires `libsecret-tools`):**

```bash
secret-tool store --label "ctxt/OPENAI_API_KEY" service ctxt account OPENAI_API_KEY
# enter value at prompt
```

**Verify:**

```bash
security find-generic-password -s ctxt -a OPENAI_API_KEY -w   # macOS
secret-tool lookup service ctxt account OPENAI_API_KEY         # Linux
```

**`ctxt secret list`:** Key enumeration not supported — the keychain API does not expose a safe list operation. `ctxt secret list` shows the service name and guidance. To see stored keys:

```bash
security dump-keychain | grep -A1 'svce.*ctxt'   # macOS
secret-tool search service ctxt                   # Linux
```

---

### `age-file`

Reads secrets from an [age](https://age-encryption.org)-encrypted YAML file. The decrypted file must be a flat `key: value` map.

```yaml
secrets:
  backend: age-file
  age_file: ~/.config/contexthelp/secrets.age
  age_identity_file: ~/.config/contexthelp/identity.txt
```

**Setup:**

```bash
# Generate an age identity (private key)
age-keygen -o ~/.config/contexthelp/identity.txt
chmod 600 ~/.config/contexthelp/identity.txt

# Get the public key from the generated file
grep "public key" ~/.config/contexthelp/identity.txt

# Create a plaintext secrets file
cat > /tmp/secrets.yaml <<EOF
OPENAI_API_KEY: sk-...
ANTHROPIC_API_KEY: sk-ant-...
EOF

# Encrypt it (replace <pubkey> with your public key)
age -r <pubkey> -o ~/.config/contexthelp/secrets.age /tmp/secrets.yaml
rm /tmp/secrets.yaml
```

`Set()` is not supported — edit the plaintext file and re-encrypt.

**`ctxt secret list`:** ✓ Key enumeration supported. Decrypts the age file and lists all key names:

```bash
ctxt secret list
# Secrets backend: age-file
#   OPENAI_API_KEY
#   ANTHROPIC_API_KEY
```

**Verify decryption:**

```bash
age -d -i ~/.config/contexthelp/identity.txt ~/.config/contexthelp/secrets.age
```

---

### `1password`

Reads secrets from a 1Password vault via the [`op` CLI](https://developer.1password.com/docs/cli/). Keys are mapped to `op://Vault/KeyName/password` URIs.

```yaml
secrets:
  backend: 1password
  onepassword_vault: MyVault  # required — name of the vault
```

**Prerequisites:**

```bash
# Install op CLI, then sign in
op signin
```

**Store a secret in 1Password:**

Create a Login or Password item in the vault named exactly after the key (e.g. `OPENAI_API_KEY`). The `password` field is used.

```bash
op item create --category=password --title=OPENAI_API_KEY \
  --vault=MyVault password=sk-...
```

**Verify:**

```bash
op read "op://MyVault/OPENAI_API_KEY/password"
```

`Set()` is not supported — manage items via the 1Password app or `op` CLI directly.

**`ctxt secret list`:** Key enumeration not supported — the `op read` command requires knowing the key name upfront. `ctxt secret list` shows the vault name and guidance. To list items:

```bash
op item list --vault MyVault
```

---

### `gh-secrets`

Writes secrets to [GitHub Actions repository secrets](https://docs.github.com/en/actions/security-guides/using-secrets-in-github-actions) via the [`gh` CLI](https://cli.github.com). Useful for syncing local API keys into CI.

```yaml
secrets:
  backend: gh-secrets
  gh_repo: owner/repo  # optional — defaults to current repository
```

> **Note:** GitHub secrets are write-only by design. `Get()` falls back to environment variables. This backend is primarily for `ctxt secret set` to push values into GitHub Actions.

**Prerequisites:**

```bash
gh auth login
```

**Push a secret to GitHub:**

```bash
# Via ctxt
ctxt secret set OPENAI_API_KEY sk-...

# Or directly via gh CLI
gh secret set OPENAI_API_KEY --body "sk-..." --repo owner/repo
```

**Verify:**

```bash
gh secret list --repo owner/repo
```

**`ctxt secret list`:** ✓ Key enumeration supported. Calls `gh secret list` and shows the names of all secrets in the repository:

```bash
ctxt secret list
# Secrets backend: gh-secrets
#   OPENAI_API_KEY
#   ANTHROPIC_API_KEY
```

Note: GitHub secrets are write-only — `ctxt secret list` shows names only, not values. `ctxt secret get` falls back to environment variables.

---

## Environment Variable Override

`CTXT_SECRETS_BACKEND` overrides `secrets.backend` from config:

```bash
CTXT_SECRETS_BACKEND=keychain ctxt analyze myfile.md
```

---

## For Plugin Authors

Plugins that need API keys should read them through the `secrets.Resolver` rather than calling `os.Getenv` directly. The resolver is available via the providers factory passed to pipeline steps. Direct `os.Getenv` calls bypass backend configuration and will not work when users have configured non-env backends.

---

## CLI Interface (`ctxt secret`)

All backends are accessible through the `ctxt secret` command group:

```bash
# Retrieve a secret from the active backend
ctxt secret get OPENAI_API_KEY

# Store a secret (backend must support Set())
ctxt secret set OPENAI_API_KEY sk-...

# Show the active backend and its config metadata
ctxt secret list
```

JSON output is supported for `get` and `list`:

```bash
ctxt --output json secret get OPENAI_API_KEY
# → {"key":"OPENAI_API_KEY","value":"sk-..."}

ctxt --output json secret list
# → {"backend":"keychain","keychain_service":"ctxt",...}
```

**Read-only backends** (`env`, `age-file`, `1password`) return a clear error on `set`. **`ctxt secret list`** enumerates keys for backends that support it (`age-file`, `gh-secrets`); for others it shows config metadata and guidance on how to inspect keys using native tools.

---

## Backend Comparison

| Backend      | `get` | `set` | `list` (key enumeration) | Requires            | Best for                       |
|-------------|-------|-------|--------------------------|---------------------|-------------------------------|
| `env`        | ✓     | ✗     | ✗ — inspect shell env    | —                   | Simple setups, CI             |
| `keychain`   | ✓     | ✓     | ✗ — use `security dump-keychain` | OS keychain  | Developer laptops             |
| `age-file`   | ✓     | ✗     | ✓ — decrypts and lists keys | `age`, `age-keygen` | Encrypted file per machine |
| `1password`  | ✓     | ✗     | ✗ — use `op item list`   | `op` CLI, account   | Teams sharing a vault         |
| `gh-secrets` | env fallback | ✓ | ✓ — calls `gh secret list` | `gh` CLI, repo | Syncing secrets into GitHub CI |
