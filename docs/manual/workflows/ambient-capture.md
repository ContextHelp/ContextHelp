# Workflow: Ambient Capture

## Goal

Run the local ambient capture daemon (`ctxd`) so signals from your working environment — clipboard, files, browser, foreground apps, screenshots, meetings — flow into ctxt continuously, without explicit per-event commands.

## Scope

- Starting and managing `ctxd` (CLI auto-config or service-manager)
- Configuring which sources are active
- Privacy posture and per-source permissions
- Choosing a buffer backend (local-FS / S3 / memory)
- Tailing the live capture stream and managing the buffer
- Interaction with sessions (ADR-067) and meeting capture (ADR-069)

## Primary stories

- `US-0211` (passive clipboard watcher)
- `US-0212` (screen monitor)
- `US-0213` to `US-0222` (file-watch, browser-history, foreground-window, sessions, meeting capture, MCP, redact, auto-detect, mobile)

## Prerequisites

1. `ctxt` and `ctxd` installed (Phase 2 substrate landed; see `tlc track show ambient-capture`).
2. `dpkms` reachable from your machine. dpkms can be local or remote — `ctxd` POSTs ambient events to whichever instance is configured (`~/.config/contexthelp/config.yaml`, `dpkms.url`).
3. OS permissions granted for sources you want active (per-OS dialogs the first time the source needs them).

## Procedure

### Step 1: Start the daemon

The simplest path — auto-config, runs in the background:

```bash
ctxt capture --ambient
```

Foreground (useful for debugging or running under tmux):

```bash
ctxt capture --ambient --foreground
```

Service-manager paths (recommended for desktop installs that should survive logout):

```bash
# macOS
brew services start ctxd
# OR
launchctl load ~/Library/LaunchAgents/io.ctxt.ctxd.plist

# Linux
systemctl --user enable --now ctxd.service
```

All three paths launch the same `ctxd` binary with the same configuration. Pick whichever fits your supervision style; only one instance runs at a time (`ctxd` uses a lockfile).

### Step 2: Check status and active sources

```bash
ctxt capture --ambient status
```

Sample output:

```
ctxd: running (PID 4218, uptime 2h 14m)
buffer: 12 events queued (~3.4 MB), backend=local-fs
endpoint: https://dpkms.example.com (last enqueue 8s ago, last failure: never)
sources:
  clipboard       active   23 events captured / 19 enqueued / 4 deduped
  filewatch       active   2 watched dirs (~/Inbox, ~/Drop)
  browser         active   chrome, firefox
  foreground      active   macOS AX
  screenshot      idle     (on-demand)
  meeting         idle     (no active recording)
session:
  sess_a1b2c3d4e5f6 — Q3 planning (started 14:32, 12 events, app_mix: zoom 67%, chrome 23%, slack 10%)
```

List registered sources without runtime info:

```bash
ctxt capture --ambient sources
```

### Step 3: Tail the live capture stream

```bash
ctxt capture --ambient tail
ctxt capture --ambient tail --source clipboard
ctxt capture --ambient tail --source meeting --since 1h
```

Each line shows: timestamp · source · kind · fingerprint (first 8 chars) · pipeline-routed-to · result (succeeded / deduped / filtered / waiting).

### Step 4: Choose a buffer backend

The buffer holds RawEvents while waiting for dpkms to be reachable (or while kit/policy holds them). Configure via `~/.config/contexthelp/config.yaml`:

```yaml
ambient:
  buffer:
    backend: local-fs        # default; XDG-compliant
    # backend: s3            # for cross-machine or compliance retention
    # backend: memory        # tests only
    local_fs:
      path: ~/.local/state/ctxt/ambient    # XDG_STATE_HOME-aware; override here if needed
      max_gb: 2
    s3:
      endpoint: https://r2.example.com
      bucket: ctxt-ambient-buffer
      # credentials via standard env vars or kit/runtime/secrets
```

Force buffer flush (replay everything held):

```bash
ctxt capture --ambient buffer flush
```

Drop the buffer entirely (confirms first):

```bash
ctxt capture --ambient buffer purge
```

### Step 5: Configure privacy guards (kit/policy CEL)

Drop clipboard events while a sensitive bundle has focus:

```yaml
# ~/.config/contexthelp/policy.d/no-clipboard-from-1password.cel
match: ctxt.ambient.event.captured
where: |
  event.source == "clipboard"
  && event.metadata.foreground_bundle_id.matches("com.1password.*")
action: deny
reason: "1Password content never leaves the machine"
```

CEL guards run **before** the event leaves the user's machine. See [`../../decisions/ADR-066-ambient-capture-substrate.md`](../../decisions/ADR-066-ambient-capture-substrate.md) §Decision for the full event taxonomy you can subscribe to.

### Step 6: Stop the daemon

```bash
ctxt capture --ambient stop

# OR via service-manager
brew services stop ctxd
launchctl unload ~/Library/LaunchAgents/io.ctxt.ctxd.plist
systemctl --user stop ctxd.service
```

On clean shutdown, `ctxd` drains in-flight enqueues, closes the active session (`force_end`, reason `shutdown`), and persists buffer state.

## Common patterns

### "I want to see what was captured today"

```bash
ctxt session list --since "today"
ctxt session show <id>
```

Or use the agent-facing MCP — see [mcp-agents.md](mcp-agents.md).

### "I want to disable a source temporarily"

```yaml
ambient:
  sources:
    browser:
      enabled: false
```

Then `ctxt capture --ambient stop && ctxt capture --ambient` to reload.

### "dpkms is down for an hour. What happens?"

`ctxd` buffers events locally; `ctxt capture --ambient status` shows `last_failure` advancing. When dpkms returns, `ctxd` replays in order. Sessions get the right `session_id` even if they arrive at dpkms before the session row (soft-FK; per ADR-067).

### "I want to know what's buffered but not yet enqueued"

```bash
ctxt capture --ambient status        # shows aggregate
# OR via MCP:
# pending_enqueue() tool from ctxd-side MCP server
```

## Outputs to validate

- `ctxd` running with PID and uptime
- Sources in expected state (active / idle)
- Buffer size reasonable (<50% of `max_gb` under normal load)
- `last_enqueue` recent (< 1m for active machines)
- Bus events flowing (`ctxt.ambient.*` topics)

## Common failure modes

### "Source clipboard: failed (permission denied)"

- macOS: System Settings → Privacy & Security → Accessibility → grant `ctxd`
- Linux: ensure `xclip` or `wl-clipboard` available; restart daemon
- Windows: clipboard access requires no special permission; check antivirus

### "Endpoint: dpkms unreachable for >30m"

- Check `dpkms.url` in config
- Network connectivity to remote dpkms (curl `<dpkms.url>/health`)
- Auth token if dpkms is `--public` bound

### "Buffer cap reached, evicting"

- Increase `ambient.buffer.local_fs.max_gb`
- Or switch to S3 backend with provider lifecycle policies
- Or investigate why dpkms isn't accepting events

### "Source meeting: not started (auto-detect dismissed)"

- Auto-detect is opt-in; explicit `ctxt capture meeting start` always works
- Check kit/policy rules aren't vetoing the request

## Related references

- [`meeting-capture.md`](meeting-capture.md) — meeting-specific workflow
- [`sessions.md`](sessions.md) — sessions / WorkUnits
- [`mcp-agents.md`](mcp-agents.md) — agent integration
- [`../../architecture/ambient-capture.md`](../../architecture/ambient-capture.md) — architecture detail
- [`../../decisions/ADR-066-ambient-capture-substrate.md`](../../decisions/ADR-066-ambient-capture-substrate.md)
- [`../../diagrams/ambient/`](../../diagrams/ambient/) — diagrams
