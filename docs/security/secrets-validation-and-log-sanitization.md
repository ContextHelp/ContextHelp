# Secrets Validation and Log Sanitization

What ctxt does to keep secrets out of configuration files and command
output, and where it does nothing.

In short:

- `ctxt config validate` and `ctxt config doctor` scan the effective
  configuration for values that look like plaintext secrets.
- `ctxt config show` redacts every credential, in every `--format`.
- `ctxt config doctor` warns when the config file is group- or
  world-readable and `--fix` sets it to `0600`.
- There is **no log sanitization**. Logs are not scanned or redacted, and
  no config key or env var changes that. There is no
  `security.secrets` or `security.logging` config, no `CH_SECURITY_*`
  env var and no `CH_DEBUG*` env var; the only `security` key is
  `security.alerts`.

---

## Config secret scan

The scan (`internal/config/secrets_scan.go`) walks every string field of
the effective config: all file layers, bound env vars and `-c` overrides
merged.

A value is flagged when:

| Rule | Detail |
|---|---|
| Value prefix | starts with `sk-ant-`, `sk-`, `eyJ` (JWT) or `ghp_` |
| Field name | the dotted path contains `token`, `password`, `secret` or `key`, and the value is longer than 8 characters |

The field-name rule skips values that look like paths (start with `/`,
`~/`, `./` or contain `/`, so URLs too) and known backend names such as
`env`, `keyring`, `agefile`, `onepassword`, `ghsecrets`, `sqlite`,
`postgres`, `s3`. Values written as `${VAR}` or `$VAR` are skipped by the
scan; the validator rejects them separately, because config files never
expand env vars.

Each finding names the field, a hint (the value redacted as `config show`
redacts it, see below) and the reason:

```bash
# Validation plus the secret scan (--check-secrets is on by default).
# Exit code 1 on any validation error or secret warning.
ctxt config validate

# Validation only
ctxt config validate --check-secrets=false

# Lint: validation errors, secret warnings, file permissions, access smells
ctxt config doctor
```

The scan runs on ctxt's own config cascade. To check a dpkms config or a
separate secrets file, layer it in: `ctxt config doctor -c <file>`.

The scan is not configurable: there are no custom patterns, exclusion
lists or inline skip comments. To scan files outside the config, use an
external tool:

```bash
gitleaks dir ~/.config/contexthelp/ --verbose --redact
```

---

## Config output redaction

`ctxt config show` prints a redacted copy of the effective config, in the
default view and every `--format` (`json`, `yaml`, ...). The secret-scan
hints of `config validate` and `config doctor`, and the URL quoted in a
`server.url` / `server.urls` load error, use the same masks:

| Value | Shown as |
|---|---|
| Credential of 16 or more characters | `****` and its last 4 characters (`****9f2c`) |
| Shorter credential | `****` |
| Unset credential | empty, as before |
| Password inside a URL or connection string | masked in place: `postgres://jad:****@db/ctxt`, `password=****` |

Credentials are the tokens (`server.token`, `server.urls[].token`,
`server.auth.static.tokens[].token`, `federation.token`,
`federations[].token`), the S3 keys (`storage.blob.s3.access_key`,
`secret_key`), and the audit and security-alert webhook URLs. Connection strings and URLs (`storage.path`, endpoints, registry
and federation URLs) keep everything but an embedded password or
secret-named parameter.

The `secret` struct tag on the config fields (`internal/config/redact.go`)
is the list: a new field whose name contains `token`, `password`,
`secret` or `key` fails the tests until it is tagged. There is no flag to
print credentials; read them from the config file itself.

---

## Config file permissions

`ctxt config doctor` warns when the config file is world-readable or
group-readable (`internal/config/lint.go`), and `ctxt config doctor --fix`
runs the equivalent of `chmod 600` on it. The file checked is the last
`-c <file>` given, otherwise `CTXT_CONFIG`, otherwise
`$XDG_CONFIG_HOME/contexthelp/ctxt.yaml`. Directories, `dpkms.yaml` and
other layers are not checked unless passed with `-c`; set them by hand:

```bash
chmod 600 ~/.config/contexthelp/ctxt.yaml ~/.config/contexthelp/dpkms.yaml
chmod 700 ~/.config/contexthelp/

# System-wide layer, if you use one
sudo chmod 600 /etc/contexthelp/dpkms.yaml

ls -la ~/.config/contexthelp/
# Should show: -rw------- for each file
```

---

## Keeping secrets out of config

Config files take every value literally: `${VAR}` is not expanded, so a
config file cannot pull a secret from the environment. Keep secrets out of
committed config instead: provider API keys in the env vars the provider
reads (`OPENAI_API_KEY`, ...), S3 blob credentials in
`CTXT_BLOB_S3_ACCESS_KEY` / `CTXT_BLOB_S3_SECRET_KEY`, and tokens that
must live in a config key (`server.auth`, `federation.token`) written
literally in a gitignored file (0600) layered with a second `--config`
(see [../deployment/docker.md#secrets](../deployment/docker.md#secrets)).

Registry tokens never appear in config. The registry entry names the auth
mechanism only; `ctxt registry login` stores the token in the OS keychain:

```yaml
registries:
  - name: private-registry
    url: https://registry.example.com
    auth:
      type: bearer          # bearer | api_key | oauth2
```

```bash
# Prompts for the token; nothing lands in shell history
ctxt registry login private-registry

# Non-interactive: pass it from secure storage
ctxt registry login private-registry --token "$(pass show contexthelp/registry-token)"

# Remove it
ctxt registry logout private-registry
```

`config.RegistryConfig` has no token field, so code that builds a
`config.Config` cannot carry a registry token either. Never assign a
literal secret to a config struct field such as `server.token` or
`federation.token` in source.

---

## What output shows secrets

### Protected

- `ctxt config validate` / `ctxt config doctor` print only the 4-character
  hint of a flagged value.
- `dpkms secret set` refuses a secret value as a positional argument; it
  reads stdin, `--from-file` or `--prompt`.
- `ctxt ui open --format json` drops the single-use login code from the
  printed URL once the link was opened in a browser.
- On a `public` instance, `/healthz` answers anonymous callers with the
  health verdict only; version, queue depths and upgrade state need a
  valid token.

### Not protected

- **Logs.** `slog` output, including `-V` / `--verbose` debug output, is
  written as-is. Do not ship debug logs from an instance that handles
  secrets without checking them first.
- **`ctxt config show` with a structured format** (`--format json`,
  `yaml`, ...) prints the whole effective config unredacted, including
  `server.token`, `server.urls[].token`, `server.auth.static.tokens`,
  `federation.token` and `storage.blob.s3` keys. The default text output
  prints a summary that includes `storage.path`, so a PostgreSQL DSN with
  an embedded password shows up there too.
- **Unwired helper.** `internal/ambient/redact` (a wrapper over kit's
  `core/redact`) exists with tests, but no runtime code path calls it.

To check a log or data directory for leaked secrets, scan it:

```bash
gitleaks dir ~/.local/share/contexthelp/ --verbose --redact
```

---

## Security alerts

dpkms counts failed authentication (per client address) and ACL denials
(per principal) in a 60-second window. Past the threshold, it writes
a `security.auth_failure` / `security.acl_denial` entry to the audit log
and, when configured, posts it to a webhook or sends it by SMTP:

```yaml
security:
  alerts:
    auth_failure_threshold: 3     # default
    acl_denial_threshold: 10      # default
    webhook_url: https://alerts.example.com/dpkms
```

```bash
ctxt audit list --event-type security.auth_failure --since 24h
```

---

## Examples

### Database connection with the password from the environment

```yaml
# ~/.config/contexthelp/dpkms.yaml
storage:
  type: postgres
  # No password here: config files do not expand env vars; libpq reads PGPASSWORD
  path: postgresql://contexthelp@db.internal/contexthelp
```

```bash
export PGPASSWORD=$(aws secretsmanager get-secret-value \
  --secret-id contexthelp/db \
  --query SecretString \
  --output text)

dpkms serve
```

### Multi-environment shell setup

```bash
# .envrc (using direnv)
# Development
export CH_STORAGE_TYPE=sqlite   # debug output: pass --verbose

# Staging
if [ "$ENVIRONMENT" = "staging" ]; then
  export CH_STORAGE_TYPE=postgres
  export PGPASSWORD=$(pass show staging/db)
  export OPENAI_API_KEY=$(pass show staging/openai-key)
fi
```

---

## Related Documentation

- [validation.md](validation.md) — config, HTTP and query validation
- [secret-management.md](secret-management.md) — secret lifecycle
- [secrets-backends.md](secrets-backends.md) — where provider keys are read from
- [Config and Permissions](../manual/reference/config-and-permissions.md) — server access, auth and scopes
- [Environment variables: security](../environment-variables/security.md)
