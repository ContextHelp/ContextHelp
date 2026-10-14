# Configuration Structure for ContextHelp

How configuration is organized across `dpkms` (substrate) and `ctxt`
(brain): which files exist, what goes in each, and the top-level keys.

Source of truth: the `Config` type and `setDefaults` in
`internal/config/config.go`. Precedence and the env vars bound to keys:
[ctxt/cli-flag-binding.md](ctxt/cli-flag-binding.md).

---

## Config Files

Each binary reads its own file name under the shared `contexthelp/`
namespace: `ctxt` reads `ctxt.yaml`, `dpkms` reads `dpkms.yaml`.

| Slot | `ctxt` | `dpkms` |
|------|--------|---------|
| User | `$XDG_CONFIG_HOME/contexthelp/ctxt.yaml` | `$XDG_CONFIG_HOME/contexthelp/dpkms.yaml` |
| Project | nearest `.contexthelp/ctxt.yaml` walking up from the working directory | nearest `.contexthelp/dpkms.yaml` |
| System | `/etc/contexthelp/ctxt.yaml` | `/etc/contexthelp/dpkms.yaml` |

`$XDG_CONFIG_HOME` defaults to `~/.config`. Missing files are skipped;
with none present, the built-in defaults apply.

- `CTXT_CONFIG=<path>` replaces all three slots with one file, for either
  binary. Pointing both at the same file shares one config between them.
- `-c <path>` layers more files on top; `-c key=value` overrides one key.

```bash
ctxt config paths     # files ctxt reads, in order
dpkms config paths    # files dpkms reads, in order
```

---

## One Schema, Two Files

Both files share one schema: every key below is valid in either, and a
binary ignores the settings its commands never use. Unknown keys are
ignored without an error, so a misspelled key silently has no effect.
Check the result with `ctxt config show --format json`.

Put each setting where the binary that acts on it reads it:

- **`dpkms.yaml`**: what `dpkms serve` runs with: listener ports,
  `server.workers`, `server.access`, `server.auth`, `jobs`, `storage`,
  `secrets`, `browser`, `steps`.
- **`ctxt.yaml`**: what the client needs: which instance to talk to
  (`server.url`, `server.urls`, `server.token`), `profile`, `capture`,
  `watch`, `search` (sent with each `ctxt find` query), `resurfacing`,
  `embeddings`, `uri`, `backup`.

Both binaries read `storage` (ctxt commands that open the database
directly) and `registries` (`ctxt registry` commands and dpkms's registry
sync), so on a single machine keep those identical in both files, or share
one file through `CTXT_CONFIG`.

---

## Example `dpkms.yaml`

```yaml
# ~/.config/contexthelp/dpkms.yaml
storage:
  type: sqlite                 # sqlite | postgres
  blob:
    backend: local             # local | s3
    threshold: 65536           # bytes

server:
  port: 8080                   # REST; dpkms serve --port wins. Set, it is exact:
                               # serve exits if busy. Unset, a busy 8080 falls back
  grpc_port: 9090
  cookie_bridge_port: 9377     # always binds 127.0.0.1
  workers: 4                   # job workers; --workers wins
  access: private              # private | protected | public
  reminder_interval: 1m

jobs:
  poll_interval: 500ms
  stale_timeout: 30m
  max_retries: 3
  max_hops: 5
  drain_timeout: 30s

providers:
  llm:
    backend: auto
    endpoint: http://localhost:11434
  embedding:
    backend: ollama
    model: nomic-embed-text

duplicates:
  policy: warn                 # warn | drop | keep
  check_exact: true
  check_similar: false

secrets:
  backend: env                 # env | keyring | agefile | onepassword | ghsecrets
```

A `protected` or `public` instance also needs inbound auth under
`server.auth`, or serve refuses to start: see
[Inbound auth, roles and scopes](manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes).

### Secrets

Config files do not expand `${VAR}`: every value is used exactly as
written, so `token: ${X}` sets the literal string `${X}`. Keep secrets
out of the shared config file:

- **Provider API keys**: in the secrets store (`dpkms secret set
  OPENAI_API_KEY`) or the env var of the same name; the store wins.
- **S3 blob credentials**: `CTXT_BLOB_S3_ACCESS_KEY` /
  `CTXT_BLOB_S3_SECRET_KEY`.
- **Everything else** (`server.auth` tokens, `federation.token`): the
  literal value in a gitignored secrets file loaded as a second
  `--config`. Container pattern:
  [deployment/docker.md#secrets](deployment/docker.md#secrets) and
  `docker/dpkms-auth.example.yaml`.

---

## Example `ctxt.yaml`

```yaml
# ~/.config/contexthelp/ctxt.yaml
server:
  urls:                        # first entry is the default instance
    - http://127.0.0.1:8080
    - name: home               # selectable with --instance home
      url: https://dpkms.example.net

profile:
  default: engineer
  profiles:
    engineer:
      description: Engineering focus
      tags: ["technical", "code"]
      search_strategy:
        mode: vector
        rrf:
          fts_weight: 0.2
          vector_weight: 0.8

registries:
  - name: uxpatterns
    url: https://api.uxpatterns.io
    auth:
      type: bearer             # bearer | api_key | oauth2
    sync_mode: full            # full | thin
    trust_level: untrusted     # trusted | untrusted | sandboxed

search:                        # sent with each ctxt find query
  default_mode: hybrid         # fts | vector | hybrid
  rrf:
    k: 60
    fts_weight: 0.5
    vector_weight: 0.5

watch:                         # ctxt watch start
  dirs: ["~/notes"]
  patterns: ["*.md"]
```

Registry tokens never go in a config file: `ctxt registry login <name>`
stores them in the OS keychain. `registries[].auth` holds only the
mechanism (`type`) and, for `api_key`, the header name (`header_name`,
default `X-API-Key`).

Every request ctxt makes to a registry with an `auth.type` (manifest,
entity index, entity pull, entitlements, step download, bookmark search)
carries its stored token: `bearer` and `oauth2` as `Authorization: Bearer
<token>`, `api_key` as `<header_name>: <token>`. `login` binds the token
to the origin (scheme, host, port) of the registry's `url` at that moment;
it goes only to that origin, over https or to a loopback host. A config
that later points the name elsewhere (a project-local
`.contexthelp/ctxt.yaml`, say) gets no token and a one-line warning: run
`login` again if the move is genuine. `login` prints the origin it binds
to. When the `registries` list comes from a project `.contexthelp/ctxt.yaml`,
`login` asks before binding; non-interactively it refuses unless
`--origin <scheme://host[:port]>` names the registry url's origin. A
redirect to another host drops the token. With no token stored the request
goes out unauthenticated, and a 401 or 403 names `ctxt registry login <name>`. A registry without `auth` is
public: the keychain is never read for it. `ctxt config doctor` rejects an
unknown `auth.type`.

dpkms has no login keychain: it reads a registry's token from
`secrets.backend` under the key `registry/<name>` (each character of
the name other than a letter, digit or `_` becomes `_`: `team-ops` is
`registry/team_ops`), a JSON value
`{"origin": "https://registry.example.com", "token": "..."}`, and sends it
under the same origin and https rules. Store it with `dpkms secret set
registry/<name>` (value on stdin); see
[secret management](security/secret-management.md#registry-tokens).

---

## Top-Level Keys

| Key | Purpose |
|-----|---------|
| `storage` | Backend (`type`), database location (`path`), blob store (`blob`) |
| `server` | Serve side: listener ports, `workers`, `dev`, `access`, `auth`, `quotas`, `allowed_hosts`, `ui`. Client side: `url`, `urls`, `token` |
| `jobs` | Worker pool timing: `poll_interval`, `stale_timeout`, `max_retries`, `max_hops`, `drain_timeout` |
| `profile` | Focus profiles: `default`, `profiles.<name>` |
| `registries` | List of registries: `name`, `url`, `auth`, `sync_mode`, `trust_level`, `entitlement_url` |
| `registries_global` | `require_signatures` |
| `plugins` | List of plugins: `type`, `plugin`, `config`. Not read: no binary loads plugins |
| `providers` | Backend per provider: `video`, `document`, `ocr`, `transcription`, `vision`, `diarization`, `llm`, `embedding` |
| `pipelines` | `overrides.<pipeline>`: `providers`, `skip_steps`, `extra_steps` |
| `retrieval` | Progressive retrieval: `method`, tier `top_k`s. Not read |
| `search` | Hybrid search: `default_mode`, `rrf`, `candidate_pool`, `min_score`, `fallback_to_fts`, `reranker` |
| `duplicates` | Duplicate detection: `policy`, `similarity_threshold`, `check_exact`, `check_similar` |
| `embeddings` | Embedding-model lifecycle guards: `min_coverage`, `grace_period` |
| `conventions` | Mention-namespace and tag vocabulary rules. Not read |
| `secrets` | Secrets backend and its settings |
| `watch` | Filesystem and clipboard watchers: `dirs`, `patterns`, `debounce`, `clipboard` (`enabled`, `paths` are not read) |
| `inbox` | Default inbox: `path`, `pipeline`, `tags`. Not read |
| `capture` | Browser capture: `browser`, `url_filter`, `history` |
| `browser` | Built-in IBR browser automation daemon |
| `fanout` | Post-ingest fan-out enrichment |
| `resurfacing` | Background resurfacing queue |
| `federations` | Remote dPKMS peers to sync with |
| `federation` | Receive-side federation `token` |
| `audit` | Audit-log forwarding: `syslog`, `webhook`. Not read |
| `security` | Security alert thresholds and delivery (`alerts`) |
| `backup` | `ctxt config backup`: `dir`, `encrypt_by_default` |
| `privacy` | `telemetry` (`CH_DISABLE_TELEMETRY` forces it off) |
| `uri` | `ctxt://` link `handler`: `cli` or `tui` |
| `i18n` | `enabled`, `preferred_languages`, `auto_translate`, `translate_tags`. Shown by `ctxt config show`; nothing else reads it |
| `steps` | External pipeline steps directory (`path`) |

Defaults for each key are in `setDefaults` in `internal/config/config.go`;
`ctxt config show --format json` prints the merged values. A template
with every key the binaries act on, commented out with its default:
[`config/config.example.yaml`](../config/config.example.yaml).

---

## Validate and Inspect

```bash
ctxt config path                    # highest-precedence existing file
ctxt config show                    # merged config (files + env + -c)
ctxt config show --format json      # full tree, credentials redacted
ctxt config validate                # every validation error, unknown-key warnings, plaintext-secret scan
ctxt config validate --check-secrets=false  # validation errors + unknown-key warnings only
ctxt config doctor                  # schema, secrets, permissions, deprecated keys
ctxt config doctor --fix            # also tightens the file's permissions
```

`ctxt config` commands read `ctxt.yaml`. To check `dpkms.yaml` with them,
point `CTXT_CONFIG` at it:

```bash
CTXT_CONFIG=~/.config/contexthelp/dpkms.yaml ctxt config doctor
```

`dpkms config` offers only `path` and `paths`.

---

## Commands That Write Config

Config files carry no schema version and are never migrated or rewritten
on load. Only commands that change configuration on request (`ctxt setup`,
`ctxt profile create|rm|set|unset`, `ctxt profile schema ...`,
`ctxt registry delete`, `ctxt watch enable|disable`) write a file, and each
changes only the keys it is about:

- **Which file**: the last `-c <path>` when one is given, else the user
  file (`$CTXT_CONFIG`, or `$XDG_CONFIG_HOME/contexthelp/ctxt.yaml`).
  `ctxt setup` always writes the user file. The project
  `.contexthelp/ctxt.yaml` is never written.
- **What lands in it**: the edit alone. Values from other files,
  `-c key=value`, environment variables and built-in defaults stay out.
  Comments, key order, quoting and blank lines are kept.
- **Entries from another file**: a profile or registry that another config
  file defines can't be removed or edited through this one. The command
  fails, names the file it writes, and changes nothing; edit the file that
  defines the entry.
- **Missing file**: created holding only the keys the command sets.

---

## Upgrading

1. Back up config files and keys with the database:
   ```bash
   dpkms backup --include-configs
   ```
2. After upgrading, check the result:
   ```bash
   ctxt config doctor
   CTXT_CONFIG=~/.config/contexthelp/dpkms.yaml ctxt config doctor
   ```

---

## Related

- [ctxt/cli-flag-binding.md](ctxt/cli-flag-binding.md) — precedence, bound env vars, flags
- [environment-variables/README.md](environment-variables/README.md) — every env var
- [manual/reference/config-and-permissions.md](manual/reference/config-and-permissions.md) — serve settings, auth, permissions
