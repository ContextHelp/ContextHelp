# Ambient capture configuration (`policy/ambient.yaml`)

`policy/ambient.yaml` declares which sensors are enabled, what platform / permission requirements they have, and per-sensor configuration (feed lists, mount points, bucket names, intervals). It is the source of truth for `ctxt capture --ambient` — the command sweeps every enabled sensor in this file.

> Specced 2026-05-06 by [ADR-065 amendment](../decisions/ADR-065-pluggable-adapters.md#amendment-2026-05-06--service-vs-sensor-distinction-ambient-capture-os-platform-slots). Implementation lands in Phase 2.

## File location

| Resolution order | Source |
|---|---|
| 1 | `$CTXT_AMBIENT_FILE` env var (used by tests + CI) |
| 2 | `$XDG_CONFIG_HOME/contexthelp/policy/ambient.yaml` |

The directory `$XDG_CONFIG_HOME/contexthelp/policy/` is the new home for both this file and `ctxt.yaml` (the kit/runtime/policy CEL rules relocated from PR #23's flat `policies.yaml`). One directory groups all gating + config-of-gates.

If neither path resolves, **the file does not auto-seed**. Unlike `policy/ctxt.yaml` (which has a sensible default — require notes on destructive ops), an ambient sensor list is a deliberate user choice. `ctxt capture --ambient` errors loud when no sensors are enabled (exit 5).

## Schema overview

```yaml
sensors:
  - name: <slot-or-backend-identifier>
    enabled: <bool>
    platform: <darwin|linux|windows> | <list>
    permissions: <list of os-permission strings>
    interval: <duration>     # default cadence when --interval unset
    config:
      <sensor-specific keys>
```

Each entry:

- **`name`** — unique identifier matching either a protocol slot (when the slot has one canonical backend, e.g. `clipboard`, `screen`, `mic`) or a `slot.backend` pair (e.g. `email.mxhook+gmail`, `files.s3`, `files.sshfs`). Naming follows the substrate's slot/backend convention from ADR-065.
- **`enabled`** — when `false`, the sensor is skipped entirely from ambient sweeps (regardless of `--input` / `--skip`). Set `false` to keep config rows around for reference without activating them.
- **`platform`** — string or list. The sensor only runs when the dPKMS instance's OS matches. `["darwin"]` is the Phase 2 default for OS-platform sensors; `["darwin","linux","windows"]` is the eventual cross-platform target.
- **`permissions`** — list of OS-permission strings the sensor needs (see § Permission model below). The substrate checks each at adapter Start; if any are missing, the sensor is skipped during ambient sweep with a stderr warning.
- **`interval`** — default cadence for continuous mode. Only used when the user passes `--interval` to `ctxt capture --ambient` *without* a value (Phase 3+ feature; Phase 2 requires explicit `--interval <duration>`).
- **`config`** — sensor-specific. The keys depend on `name`; see § Per-sensor schemas below.

## Example — minimal

```yaml
sensors:
  - name: clipboard
    enabled: true
    platform: [darwin]
    permissions: []   # clipboard is unprivileged on macOS

  - name: rss
    enabled: true
    config:
      feeds:
        - https://hnrss.org/frontpage.rss
        - https://danluu.com/atom.xml
```

`ctxt capture --ambient` against this config reads the clipboard and polls both RSS feeds once.

## Example — full Phase 2 set

```yaml
sensors:
  # OS-platform sensors (darwin-only in Phase 2)
  - name: clipboard
    enabled: true
    platform: [darwin]
    permissions: []
    interval: 30s

  - name: screen
    enabled: false   # opt-in; requires user-facing screen recording prompt
    platform: [darwin]
    permissions: [tcc:ScreenCapture]
    interval: 5m
    config:
      regions: full   # full | window | rect:x,y,w,h
      format: png
      max_size_mb: 5

  - name: mic
    enabled: false   # opt-in; loud privacy implication
    platform: [darwin]
    permissions: [tcc:Microphone]
    interval: 0       # mic is one-shot per --window; no default cadence
    config:
      sample_rate: 16000
      max_window: 5m

  - name: watched-fs
    enabled: true
    platform: [darwin, linux]
    permissions: []
    config:
      paths:
        - ~/Documents/inbox
        - ~/Downloads

  # Local-fs sensors (no OS permission needed; fsnotify-based)
  - name: files.local-fs
    enabled: true
    config:
      roots:
        - path: ~/Documents
          recursive: true
          ignore: [".DS_Store", "*.tmp"]
        - path: ~/Downloads
          recursive: false

  # Network sensors
  - name: feeds.rss
    enabled: true
    interval: 15m
    config:
      feeds:
        - url: https://hnrss.org/frontpage.rss
          hint: ["#hn"]
        - url: https://danluu.com/atom.xml
          hint: ["#blogs"]
        - url: https://lobste.rs/rss
          hint: ["#lobsters"]

  - name: files.s3
    enabled: true
    interval: 1h
    config:
      buckets:
        - name: my-research-bucket
          region: us-east-1
          credentials_ref: op://Personal/aws-research-bucket
          prefix: papers/
        - name: shared-team-bucket
          region: us-west-2
          credentials_ref: op://Team/aws-shared-readonly
          prefix: ""

  - name: files.sshfs
    enabled: true
    interval: 30m
    config:
      mounts:
        - host: dev.example.com
          user: jad
          remote_path: /srv/notes
          identity_file: ~/.ssh/id_ed25519
          ignore: [".git", "node_modules"]
```

## Per-sensor schemas

### `clipboard`

| Key | Type | Default | Notes |
|---|---|---|---|
| `interval` | duration | `30s` | Cadence in continuous mode. Clipboard is read on every tick; dedup by content hash. |

No `config` block needed today.

### `screen`

| Key | Type | Default | Notes |
|---|---|---|---|
| `regions` | string | `full` | `full` (whole screen), `window` (focused window), `rect:x,y,w,h` (fixed rectangle). |
| `format` | string | `png` | `png`, `jpeg`, `heic`. |
| `max_size_mb` | int | `5` | Skip captures exceeding this size; useful on multi-monitor setups. |

### `mic`

| Key | Type | Default | Notes |
|---|---|---|---|
| `sample_rate` | int | `16000` | Hz. 16000 is sufficient for transcription. |
| `max_window` | duration | `5m` | Hard ceiling on `--window`; protects against accidentally requesting hours of audio. |

### `watched-fs`

| Key | Type | Default | Notes |
|---|---|---|---|
| `paths` | list of strings | required | Directories to watch via fsnotify. New/modified files emit objects on the next ambient sweep. |

### `files.local-fs`

| Key | Type | Default | Notes |
|---|---|---|---|
| `roots[].path` | string | required | Directory root. |
| `roots[].recursive` | bool | `true` | Walk subdirectories. |
| `roots[].ignore` | list of glob patterns | `[]` | Filename / dirname patterns to skip. |

Differs from `watched-fs`: local-fs is a **scan-on-tick** sensor (full tree state at each capture), watched-fs is an **fsnotify event stream** (deltas since last tick).

### `feeds.rss`

| Key | Type | Default | Notes |
|---|---|---|---|
| `feeds[].url` | string | required | RSS / Atom feed URL. |
| `feeds[].hint` | list of strings | `[]` | Default hints attached to objects from this feed. |

### `files.s3`

| Key | Type | Default | Notes |
|---|---|---|---|
| `buckets[].name` | string | required | S3 bucket name. |
| `buckets[].region` | string | required | AWS region or S3-compatible endpoint region. |
| `buckets[].endpoint` | string | (AWS) | Override for S3-compatible endpoints (R2, B2, MinIO, Spaces, Wasabi, Garage). |
| `buckets[].credentials_ref` | string | required | `op://`, `keychain://`, or env-ref URI for AWS credentials. Resolved at adapter Start via `kit/storage/secret`. |
| `buckets[].prefix` | string | `""` | Object key prefix. |

### `files.sshfs`

| Key | Type | Default | Notes |
|---|---|---|---|
| `mounts[].host` | string | required | Hostname or `user@host`. |
| `mounts[].user` | string | required | SSH user. |
| `mounts[].remote_path` | string | required | Path on the remote. |
| `mounts[].identity_file` | string | `~/.ssh/id_ed25519` | SSH key path. |
| `mounts[].ignore` | list of glob patterns | `[]` | Names to skip during walk. |

## Permission model

Sensors that need OS-level permissions declare them via `permissions:` strings. The substrate checks at adapter Start; missing permissions cause the sensor to be **skipped during ambient sweep** (not the whole sweep aborted) with a warning printed to stderr.

### Permission string vocabulary

| Prefix | Platforms | Meaning |
|---|---|---|
| `tcc:<service>` | darwin | macOS TCC (Transparency, Consent, Control) database entry — `tcc:ScreenCapture`, `tcc:Microphone`, `tcc:Camera`, `tcc:Accessibility`, `tcc:InputMonitoring`. |
| `xdg:<portal>` | linux | XDG desktop portal — `xdg:Screenshot`, `xdg:Camera`, `xdg:Microphone`. |
| `win32:<capability>` | windows | Windows Runtime capability — `win32:graphicsCapture`, `win32:microphone`, etc. |

Phase 2 ships only the `tcc:*` strings (darwin sensors). Linux + Windows mappings land in Phase 3.

### Granting permissions

Permission grant flows are out-of-band per platform:

- **macOS**: System Settings → Privacy & Security → (Screen Recording / Microphone / Accessibility) → toggle the dPKMS daemon.
- **Linux**: portal grants happen on first request; portal config in `~/.config/xdg-desktop-portal/`.
- **Windows**: Settings → Privacy & Security → (Screen / Microphone / etc.) → toggle the dPKMS daemon.

The substrate does not attempt to programmatically grant. It surfaces missing permissions via:

```bash
ctxt doctor --check ambient
```

(Phase 2 deliverable.) Reports each enabled sensor's status: granted, denied, or not-yet-prompted.

### Permission lifecycle

The substrate **does not store** permission state — it queries the OS at adapter Start every time. Revoked permissions take effect on the next sweep. No cache, no stale grants.

## Validation + error modes

`ctxt capture --ambient` performs config validation at startup:

| Condition | Behavior |
|---|---|
| File doesn't exist | Error loud: "ambient capture requires `policy/ambient.yaml`; create one — see `ctxt capture --ambient --help`" |
| File exists but `sensors:` is empty or missing | Error loud (same as above) |
| Every sensor disabled (`enabled: false`) | Error loud: "no sensors enabled in `policy/ambient.yaml`" |
| Unknown sensor name | Error loud: "unknown sensor `foo`; valid sensors: clipboard, screen, mic, watched-fs, files.local-fs, files.s3, files.sshfs, feeds.rss" |
| Required `config` keys missing | Error loud, naming the missing key |
| Platform mismatch (sensor is `linux` only, host is `darwin`) | Sensor skipped silently; logged at debug level |
| Permission denied | Sensor skipped with stderr warning; sweep continues |
| Sensor's adapter fails to Start | Sensor skipped with stderr warning; sweep continues |

The principle: **config errors fail loud; runtime errors degrade gracefully.** A typo in the config should be impossible to ignore; a flaky network or a revoked permission should not kill the entire sweep.

## Migration from PR #23 `policies.yaml`

PR #23 placed the kit/runtime/policy YAML at `$XDG_CONFIG_HOME/contexthelp/policies.yaml`. This amendment relocates it to `$XDG_CONFIG_HOME/contexthelp/policy/ctxt.yaml` and adds `policy/ambient.yaml` as a sibling. **The relocation is automatic** on first daemon boot after the Phase 2 upgrade:

```text
$XDG_CONFIG_HOME/contexthelp/policies.yaml      ← old (PR #23)
                            ↓ auto-moved on first boot
$XDG_CONFIG_HOME/contexthelp/policy/ctxt.yaml   ← new
$XDG_CONFIG_HOME/contexthelp/policy/ambient.yaml ← new (created empty if absent? error if absent? — see below)
```

Operators with a custom `CTXT_POLICY_FILE` environment variable must update it; the daemon logs a one-time deprecation warning if the env points at the old name.

`policy/ambient.yaml` is **not** auto-created — its absence is treated the same as an empty `sensors:` list, which makes `ctxt capture --ambient` error loud. This is intentional: ambient sensors require deliberate operator opt-in.

## See also

- [capture.md](capture.md) — `ctxt capture` command spec
- [policies.md](policies.md) — `policy/ctxt.yaml` schema (relocated from `policies.yaml`)
- [ADR-065](../decisions/ADR-065-pluggable-adapters.md) — substrate decision + 2026-05-06 amendment
- [policies.md (PR #23)](policies.md) — original kit/runtime/policy adoption (for historical context)
