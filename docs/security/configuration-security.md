# Configuration Security

Keeping `ctxt.yaml` and `dpkms.yaml` free of secrets, readable only by
their owner, and checked before they ship.

## Overview

Config files hold structure; secrets come from elsewhere. Every value in a
config file is taken literally: `${VAR}` is **not** expanded, so a config
file cannot reference an environment variable. The file names, cascade and
keys are described in [../configuration-structure.md](../configuration-structure.md).

## Configuration File Security

### File Permissions

```bash
# User files
chmod 600 ~/.config/contexthelp/ctxt.yaml ~/.config/contexthelp/dpkms.yaml

# System files
sudo chmod 600 /etc/contexthelp/ctxt.yaml /etc/contexthelp/dpkms.yaml

# Verify: should show -rw------- (600)
ls -l ~/.config/contexthelp/
```

`ctxt config doctor` reports a world- or group-readable config file;
`ctxt config doctor --fix` tightens it. Both check the file `ctxt` reads
(`CTXT_CONFIG`, else `$XDG_CONFIG_HOME/contexthelp/ctxt.yaml`); point
`CTXT_CONFIG` at `dpkms.yaml` to check that one:

```bash
ctxt config doctor --fix
CTXT_CONFIG=~/.config/contexthelp/dpkms.yaml ctxt config doctor --fix
```

### File Ownership

```bash
# Personal use
chown "$USER" ~/.config/contexthelp/*.yaml

# System daemon running as its own user
sudo chown contexthelp:contexthelp /etc/contexthelp/dpkms.yaml
```

## Where Secrets Go

| Secret | Where it comes from | Never |
|--------|---------------------|-------|
| Provider API keys (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `OPENROUTER_API_KEY`) | Environment variable of that name | Config file |
| Embedding provider key | Variable named by `providers.embedding.api_key_env` | The key itself in config |
| Registry tokens | OS keychain, via `ctxt registry login <name>` | Config file, environment |
| PostgreSQL password | libpq environment (`PGPASSWORD`) or `~/.pgpass`; DSN in `storage.path` without it | Password inside the DSN |
| S3 blob credentials | `CTXT_BLOB_S3_ACCESS_KEY`, `CTXT_BLOB_S3_SECRET_KEY` | `storage.blob.s3.*` in a committed file |
| Config-bundle passphrase | `CTXT_BACKUP_PASSPHRASE`, OS keychain or prompt | Config file |
| Other secrets `dpkms secret` manages | Backend chosen by `secrets.backend` (`env`, `keyring`, `agefile`, `onepassword`, `ghsecrets`) | — |

Registry entries carry only the mechanism:

```yaml
registries:
  - name: private-registry
    url: https://registry.example.com
    auth:
      type: bearer          # bearer | api_key | oauth2
```

```bash
ctxt registry login private-registry   # prompts; stores in the OS keychain
```

### Secrets that live in config

A few settings have no other source: inbound auth tokens
(`server.auth.static.tokens[].token`), client tokens (`server.token`,
`server.urls[].token`) and federation tokens (`federation.token`,
`federations[].token`). Write them literally (no `${VAR}`) in their own
file, `0600`, owned by the user that runs the binary and kept out of
version control, and layer it on the main config with a second
`--config` (later files win):

```bash
dpkms serve --config ~/.config/contexthelp/dpkms.yaml \
  --config ~/.config/contexthelp/dpkms-secrets.yaml
```

No config key reads a secret from a file: there is no `token_file`,
`connection_file` or `api_key_file`, and such keys are ignored like any
other unknown key.

### Anti-Pattern: Hardcoded Secrets

```yaml
# BAD - the value is used literally, and it is now in your repo
server:
  token: s3cret-literal
storage:
  path: "postgresql://ctxt:password@db/contexthelp"

```

Also wrong: `api_key_env: ${OPENAI_API_KEY}` is not expanded, so it names
a variable literally called `${OPENAI_API_KEY}`. Write the variable's
name: `api_key_env: OPENAI_API_KEY`.

## Configuration Validation

```bash
# Loads the config and scans it for plaintext secrets (exit 1 on findings)
ctxt config validate

# Load errors only
ctxt config validate --check-secrets=false

# Schema, secrets, permissions and deprecated keys
ctxt config doctor

# Merged result after files, env and -c
ctxt config show --format json
```

The secret scan flags values starting with `sk-`, `sk-ant-`, `eyJ` or
`ghp_`, and values longer than 8 characters (other than paths and backend
names) in fields whose name contains `token`, `password`, `secret` or
`key`. It skips values written as `${VAR}` or `$VAR`; those are not
expanded either, so such a value is a literal string that silently does
not work.

### Configuration Review Checklist

- [ ] No literal secret in any config file
- [ ] No `${VAR}` value anywhere (it is not expanded)
- [ ] Registry tokens stored with `ctxt registry login`
- [ ] Files holding auth or federation tokens are `0600`
- [ ] `ctxt config validate` and `ctxt config doctor` pass

## Configuration as Code

### What to Commit

```yaml
# .contexthelp/ctxt.yaml — project layer, safe to commit
profile:
  default: engineer
registries:
  - name: team-taxonomy
    url: https://registry.example.com
    auth:
      type: bearer
search:
  default_mode: hybrid
```

Unknown keys are ignored without an error, so review committed files with
`ctxt config show --format json` rather than trusting that a key took
effect.

### What NOT to Commit

- Any file with `server.auth`, `server.token`, `server.urls[].token`,
  `federation.token` or `federations[].token` values
- `.env` files
- `storage.path` values that embed a database password

The repository's pre-commit hooks scan staged files for secrets; see
[../git-hooks.md](../git-hooks.md).

## Configuration for Different Environments

Pick the file per environment instead of templating one:

```bash
# One file for both binaries, replacing the system/user/project cascade
CTXT_CONFIG=/etc/contexthelp/prod/dpkms.yaml dpkms serve

# Layer an environment file on top of the cascade
dpkms serve -c /etc/contexthelp/staging.yaml

# Override one key for one run
dpkms serve -c server.workers=8
```

Bound environment variables (`CH_SERVER_PORT`, `DPKMS_WORKERS`,
`CTXT_BLOB_*`, ...) sit above every file and below `-c key=value`; the full
list and precedence are in [../ctxt/cli-flag-binding.md](../ctxt/cli-flag-binding.md).

## Container Configuration

- Mount the config file read-only and point `CTXT_CONFIG` at it.
- Inject secrets as environment variables from the orchestrator's secret
  store: provider API keys, `PGPASSWORD`, `CTXT_BLOB_S3_*`.
- Keep auth tokens in a mounted secret file loaded as a second
  `--config` (or `-c <path>` layer), not in an image layer. The repo's
  image loads `/run/secrets/dpkms-auth.yaml` this way (template:
  `docker/dpkms-auth.example.yaml`); see
  [Docker: Secrets](../deployment/docker.md#secrets).

```yaml
# docker-compose.yml (excerpt)
services:
  dpkms:
    environment:
      CTXT_CONFIG: /etc/contexthelp/dpkms.yaml
      OPENAI_API_KEY: ${OPENAI_API_KEY}   # expanded by compose, not by dpkms
      PGPASSWORD: ${PGPASSWORD}
    volumes:
      - ./dpkms.yaml:/etc/contexthelp/dpkms.yaml:ro
```

See [../deployment/docker.md](../deployment/docker.md) for the image itself.

## Related Documentation

- [Configuration structure](../configuration-structure.md)
- [CLI flags, env vars and precedence](../ctxt/cli-flag-binding.md)
- [Environment variables](../environment-variables/README.md)
- [Secret Management Best Practices](./secret-management.md)
- [Secrets backends](./secrets-backends.md)
- [Security Model](./security-model.md)
