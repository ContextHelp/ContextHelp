# Ambient Capture

Continuous local-machine capture for ctxt — the `ctxd` daemon watches your clipboard, drop folder, browser history, foreground window, screenshots, and meetings while you work, feeding events into your dpkms knowledge graph automatically.

> This page is the high-level summary. For task-oriented walkthroughs, see [`manual/workflows/ambient-capture.md`](manual/workflows/ambient-capture.md). For the engineer-facing architecture reference, see [`architecture/ambient-capture.md`](architecture/ambient-capture.md). Decisions live in [ADR-066](decisions/ADR-066-ambient-capture-substrate.md), [067](decisions/ADR-067-session-workunit.md), [068](decisions/ADR-068-mcp-read-surface.md), [069](decisions/ADR-069-meeting-capture-source.md).

## What it is

Six pluggable ambient sources, all running locally on your machine:

| Source | What it captures | Pipeline |
|---|---|---|
| Clipboard | URL / text / code blocks you copy | url.generic / text.short |
| File-watch | Files dropped into `~/Inbox/ctxt` | text.long / image.ocr / audio.transcribe / video.full |
| Browser-history | Pages you visit (Chrome/FF/Safari/Edge) | url.generic / url.repo |
| Foreground-window | macOS bundle id + app name + window title | (feeds session cutter; no pipeline) |
| Screenshot | Hotkey-triggered screen capture | image.ocr |
| Meeting | Video calls (Zoom/Meet/Teams/FaceTime/Discord) — explicit-trigger only | audio.transcribe / video.full |

Sessions ([ADR-067](decisions/ADR-067-session-workunit.md)) group these signals into bounded work units (idle 5m / soft-cut 3m / timeout 2h) so "what did I work on yesterday afternoon?" returns a coherent answer.

## Why client-side

dpkms is often deployed remote (managed instance, household NAS, federated peer). A remote dpkms cannot read your clipboard, see what app has foreground focus, or capture system audio. Ambient capture runs in `ctxd` on your machine and POSTs to whichever dpkms is configured via the existing `ctxt analyze` HTTP path. dpkms remains a pure pipeline + storage worker.

## Quick start

```bash
# Auto-config: launches ctxd in the background.
ctxt capture --ambient

# OR via service-manager (preferred for desktop installs).
brew services start ctxd                                   # macOS Homebrew
launchctl load ~/Library/LaunchAgents/io.ctxt.ctxd.plist   # macOS launchd
systemctl --user enable --now ctxd.service                # Linux systemd

# Status / sources / live tail
ctxt capture --ambient status
ctxt capture --ambient sources
ctxt capture --ambient tail [--source clipboard]

# Stop
ctxt capture --ambient stop
```

Full walkthrough: [`manual/workflows/ambient-capture.md`](manual/workflows/ambient-capture.md).

## Privacy posture

Privacy enforcement happens **before the event leaves your machine** at three layers:

1. **Source-side redaction** ([`internal/ambient/redact/`](../internal/ambient/redact)) strips known-sensitive shapes (passwords, OAuth tokens, AWS keys, GitHub PATs, SSNs, credit cards) from RawEvent payloads before emission.
2. **kit/runtime/policy CEL guards** subscribe to `ctxt.ambient.event.captured` and can veto sensitive contexts ("drop clipboard while bundle-id matches `com.1password.*`"). Veto fires before the event is buffered or enqueued.
3. **kit/runtime/bus** taps make every transformation (capture, redact, filter, dedup, compress, buffer, enqueue, succeeded/failed) independently observable for audit dashboards.

For continuous-capture sources (foreground, browser-history), the privacy posture is structural:

- Foreground source captures **bundle id + window title only** — strictly NOT the AX tree, focused-element values, visible text, or keystrokes.
- Browser-history runs on a configurable poll interval (default 5 min) and drops URLs matching your [URL filter](#keep-sites-out-of-browser-capture).
- Meeting capture is **explicit-trigger only**; auto-detect prompt is opt-in and never starts recording without user confirmation.

### Keep sites out of browser capture

Every browser capture path (open tabs, history) checks each URL against `capture.url_filter` before sending it anywhere. Rules can apply everywhere, to one browser, or to one browser profile:

```yaml
capture:
  url_filter:
    deny:                              # every browser, every profile
      - "*.bank.example.com"
    browsers:
      brave:
        deny: []                       # every Brave profile
        profiles:
          Work:                        # the name you pass to --browser-profile
            deny:
              - "crm.example.net"
              - "https://drive.example.com/shared"
            allow_only: []             # optional: capture ONLY these
```

Rule syntax:

| Rule | Matches |
|---|---|
| `crm.example.net` | that host: any scheme, port, path or query |
| `*.example.net` | `example.net` and every subdomain |
| `*://crm.example.net/*` | the same host, written as a URL pattern |
| `https://drive.example.com/shared` | that scheme, host and path, and anything below the path |
| `*://localhost:8080/*` | that port only (no port in the rule = any port) |

A rule without `://` is a host and nothing else. A rule such as `crm.example.net/deals`, `localhost:8080`, `user@crm.example.net` or a bare `*` fails the command with an error naming it; write `https://crm.example.net/deals` or `*://localhost:8080/*` instead. Quote rules that start with `*` in YAML.

Host matching ignores letter case, userinfo and a trailing dot, and compares internationalized hosts in punycode, so `bücher.example` and `xn--bcher-kva.example` are the same rule. A host that appears only in the path or query string never matches.

Only `http` and `https` pages are captured. Every other scheme is dropped before any rule runs: `file:`, `about:`, `data:`, `ftp:`, `ws:`, browser pages such as `chrome:` and `brave:`, and app links such as `mailto:`. A dry run shows these as `builtin: scheme not captured (<scheme>)`. localhost and loopback addresses are always dropped too.

Scoped rules only narrow: deny lists add up across global, browser and profile, and a URL must satisfy every `allow_only` list that applies. Deny wins over `allow_only`. A URL that cannot be parsed is dropped.

Rules add up across config layers too. Your user file, a project `.contexthelp/ctxt.yaml`, each `-c <file>` and each `-c key=value` all contribute:

- **Deny lists accumulate.** At every scope (global, `browsers.<b>`, `browsers.<b>.profiles.<p>`) the effective deny list is the union of every layer's list, with duplicates removed. A project file that sets `browsers.brave.profiles.Work.deny` adds to your user file's Work rules, and `-c 'capture.url_filter.deny=["crm.example.net"]'` adds one rule for a single run.
- **No layer can remove a deny rule.** An empty list (`deny: []`), `null`, or leaving the key out changes nothing. To stop denying a site, delete the rule from the file that holds it; `ctxt config paths` lists the files in play.
- **Each `allow_only` list is one more gate.** When two layers set different `allow_only` lists at the same scope, a URL must match both, so a later layer can narrow capture but never widen it.
- A bare value such as `-c capture.url_filter.deny=crm.example.net` is not a list and fails the command; quote a YAML list as shown above.

Every other config key keeps normal layer precedence: the later layer wins.

## Buffer + retention

Events flow through a pluggable client-side buffer ([`internal/ambient/buffer/`](../internal/ambient/buffer)) that holds them while waiting for dpkms to be reachable:

- **Local-FS** (default): XDG-compliant `$XDG_STATE_HOME/ctxt/ambient/`. 7-day TTL, 2 GB cap, LRU eviction. Uses [`hop.top/kit/go/storage/blob/local`](https://hop.top/kit) underneath.
- **S3-compatible** (opt-in): AWS S3 / Cloudflare R2 / Backblaze B2 / MinIO. Provider lifecycle policies handle retention. Use case: cross-machine deployment, compliance retention.
- **In-memory** (tests only): bounded ring.

Configure in `~/.config/contexthelp/config.yaml`:

```yaml
ambient:
  buffer:
    backend: local-fs        # local-fs | s3 | memory
    local_fs:
      path: ~/.local/state/ctxt/ambient   # XDG_STATE_HOME-aware
      max_gb: 2
    s3:
      bucket: ctxt-ambient-buffer
      endpoint: https://r2.example.com    # optional
      # credentials via standard env vars or kit/runtime/secrets
```

## Configuration reference

```yaml
ambient:
  sources:
    clipboard:
      enabled: true
      poll_interval: 2s
      min_length: 80
    filewatch:
      enabled: true
      directories: [~/Inbox/ctxt]
      max_file_size_mb: 500
    browserhistory:
      enabled: true
      poll_interval: 5m
      browsers: [chrome, firefox, safari]
    foreground:
      enabled: true              # macOS only in v1
      debounce_seconds: 1
      capture_window_title: true
      exclude_bundles:
        - com.1password.macos
    screenshot:
      enabled: true
      hotkey: "shift+cmd+s"
      exclude_bundles: [com.1password.macos]
    meeting:
      enabled: false             # explicit opt-in
      auto_detect:
        enabled: false           # opt-in within opt-in
        prompt_timeout: 10s
      media_retention_hours: 48
      local_max_gb: 20
      s3_archive: false
  session:
    gap_minutes: 5
    soft_cut_minutes: 3
    max_session_hours: 2
  buffer:
    backend: local-fs
```

## What gets captured per source

| Source | RawEvent.Payload | Metadata |
|---|---|---|
| Clipboard | The copied text/URL (string bytes) | `foreground_bundle_id` |
| File-watch | Absolute file path | `file_path`, `file_size`, `file_mtime` |
| Browser-history | The URL | `browser`, `title`, `visited_at`, `subtype=search-query` (if applicable) |
| Foreground-window | `bundle_id` (string bytes) | `bundle_id`, `app_name`, `window_title` (toggleable) |
| Screenshot | Image file path | `file_path`, `label`, `width`, `height`, `foreground_bundle_id` |
| Meeting | Recording file path | `file_path`, `label`, `mode` (full/audio_only), `duration_sec`, `bytes`, `end_reason` |

The dpkms-side enrichment pipelines (text.short, url.generic, image.ocr, audio.transcribe, video.full) extract entities, mentions, decisions, tasks, and embeddings from these inputs.

## Composition

Once events are flowing, compose by session or time range:

```bash
ctxt session list --since "yesterday"
ctxt session show sess_a1b2c3d4e5f6
ctxt compose brief --session sess_a1b2c3d4e5f6
ctxt compose summary --since "2 hours ago" --until now
```

## Agent integration (MCP)

Two MCP read-surfaces (per [ADR-068](decisions/ADR-068-mcp-read-surface.md)):

- **dpkms-side** (`/api/v1/mcp/`) — authoritative; 10 tools: `search`, `list`, `get`, `entity`, `recent`, `sessions`, `session`, `compose`, `mentions`, `schema`.
- **ctxd-side** (`:8744/mcp`) — local-only; 5 tools: `current_session`, `recent_local`, `pending_enqueue`, `sources`, `health`.

```bash
ctxt mcp install claude-code        # OR claude-desktop / cursor / codex / opencode / mcp-json
ctxt mcp status
```

Restart your agent client; both endpoints get registered. Full guide: [`manual/workflows/mcp-agents.md`](manual/workflows/mcp-agents.md).

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `ctxd: not running` after `ctxt capture --ambient` | Lockfile stale | `rm $XDG_STATE_HOME/ctxt/ambient/lock` and retry |
| Source `foreground: failed (permission denied)` on macOS | AX permission missing | System Settings → Privacy & Security → Accessibility → grant `ctxd` |
| `Endpoint: dpkms unreachable for >30m` | Network or auth | Check `dpkms.url` in config; `curl <dpkms.url>/health` |
| Buffer at cap, evicting | Slow enqueue / dpkms down | Check `pending_enqueue()` MCP tool; investigate dpkms reachability |
| Source meeting: not started | Auto-detect dismissed | Use explicit `ctxt capture meeting start` |
| Session never starts | Foreground source disabled / no AX permission | Enable foreground source; grant macOS AX |

For deeper troubleshooting (kit/policy rules, bus event taps, manual buffer flush, daemon restart procedures), see the internal dpkms-ambient ops runbook.

## Related

- [`architecture/ambient-capture.md`](architecture/ambient-capture.md) — engineer-facing architecture reference (substrate, source interface, runner, buffer, cutter, MCP)
- [`manual/workflows/ambient-capture.md`](manual/workflows/ambient-capture.md) — task-oriented walkthrough
- [`manual/workflows/meeting-capture.md`](manual/workflows/meeting-capture.md) — meeting-specific workflow
- [`manual/workflows/sessions.md`](manual/workflows/sessions.md) — session/work-unit walkthrough
- [`manual/workflows/mcp-agents.md`](manual/workflows/mcp-agents.md) — agent integration
- [`meeting-capture.md`](meeting-capture.md) — meeting capture summary (companion page)
- [`diagrams/ambient/`](diagrams/ambient/) — 12 mermaid diagrams covering substrate, sessions, MCP, meeting capture
