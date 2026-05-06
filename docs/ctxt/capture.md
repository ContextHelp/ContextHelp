# `ctxt capture`

`ctxt capture` is the canonical user-facing verb for explicit reads from any source the dPKMS substrate handles — URLs, files, stdin, and the ambient set of configured sensors. It replaces the ad-hoc `ctxt` / `ctxt analyze` / `ctxt ingest` invocations as the recommended entry point for new workflows; existing forms keep working for backward compat.

> Specced 2026-05-06 by [ADR-065 amendment](../decisions/ADR-065-pluggable-adapters.md#amendment-2026-05-06--service-vs-sensor-distinction-ambient-capture-os-platform-slots). Implementation lands in Phase 2 alongside the sensor adapters that `--ambient` invokes.

## Why a new verb

The dPKMS already accepted captures via several distinct paths (clipboard auto-detect, `ctxt analyze --file`, `ctxt ingest --source <name>`). They worked but didn't share a vocabulary. The substrate now distinguishes **service adapters** (long-running daemons like Stalwart, mxhook+gmail) from **sensors** (passive observers like rss, sshfs, clipboard). User-initiated reads — whether one URL or every sensor at once — are all `ctxt capture`. Service adapters are configured in `policy/ambient.yaml` and connected to via their wire protocols (IMAP, CardDAV); they don't appear here.

## Synopsis

```
ctxt capture [<source>] [flags]
ctxt capture --ambient [flags]
ctxt capture --stdin [flags]
```

`<source>` is a URL, file path, or quoted string. With no `<source>` and no `--ambient`/`--stdin`, the command reads the system clipboard.

## Flags

### Source selection

| Flag | Type | Semantics |
|---|---|---|
| `--ambient` | bool | Sweep every sensor enabled in `policy/ambient.yaml`. Mutually exclusive with positional `<source>` and `--stdin`. |
| `--stdin` | bool | Read from stdin. Mutually exclusive with positional `<source>` and `--ambient`. |
| `--input <name>` | repeatable + CSV | Restrict ambient sweep to listed sensors. Repeated flags and CSV both supported (`--input clipboard --input screen` and `--input clipboard,screen` are equivalent). Only valid with `--ambient`. |
| `--skip <name>` | repeatable + CSV | Exclude listed sensors from the ambient sweep. Only valid with `--ambient`. |
| `--source <name>` | string | Override auto-detection on a positional `<source>`. Use when the detector chain would pick wrong (e.g. a vCard file shouldn't classify as `text`). |

### Bounds

| Flag | Type | Semantics |
|---|---|---|
| `--window <duration>` | duration string | Capture state from the last N (e.g. `5m`, `1h`, `24h`). Sensors that support windowing apply it (RSS reads items published within the window, mic records that duration, screen reads the buffer); sensors that don't support windowing ignore it gracefully (clipboard always reads "now"). |
| `--interval <duration>` | duration string | When set, the command runs continuously and re-reads the source(s) on this cadence until interrupted (Ctrl-C). When absent, the command is one-shot. |

### Routing

| Flag | Type | Semantics |
|---|---|---|
| `--inbox` | bool | Route captured objects through the inbox for triage instead of running pipelines immediately. Composable with every form. |
| `--pipeline <name>` | string | Override pipeline selection (default: detector chain picks). |

### Metadata (composable across all forms)

| Flag | Type | Semantics |
|---|---|---|
| `--hint <value>` | repeatable + CSV | Free-form hints attached to the captured object — typically `#tag` or short keywords. Influences pipeline selection + downstream enrichment. |
| `--mention <value>` | repeatable + CSV | Mention targets (`@project.alpha`, `@person.name`). Resolved against the entity registry. |
| `--note <text>` | string | Audit note explaining why this capture happened. Required by some default policy rules; recorded against the resulting object. |
| `--profile <name>` | string | Focus profile for the capture (overrides the default profile). |

## Examples

### URL capture

```bash
# RSS / Atom feed — detector picks the rss pipeline
ctxt capture https://hnrss.org/frontpage.rss
ctxt capture https://danluu.com/atom.xml

# YouTube video — detector picks video.transcript
ctxt capture https://youtube.com/watch?v=dQw4w9WgXcQ
ctxt capture https://youtu.be/dQw4w9WgXcQ

# Generic page — url.standard
ctxt capture https://example.com/post

# Authenticated source — detector falls through; explicit pipeline override
ctxt capture https://app.notion.so/page-id --pipeline url.authenticated
```

### File capture

```bash
ctxt capture ./notes.md
ctxt capture ./meeting.m4a
ctxt capture ~/Downloads/screenshot.png

# Override detection on a non-obvious format
ctxt capture ./contact.vcf --source contacts
ctxt capture ./calendar.ics --source calendar
```

### Stdin capture

```bash
cat README.md | ctxt capture --stdin
echo "thought i had" | ctxt capture --stdin --type text
```

### Continuous capture (single source)

```bash
# Re-poll the feed every 15m. Ctrl-C stops the loop.
ctxt capture https://hnrss.org/frontpage.rss --interval 15m

# Re-poll with a window — each tick reads items from the last 30m.
# Useful when the source has more than the interval covers; dedup
# handles overlap.
ctxt capture https://hnrss.org/frontpage.rss --interval 15m --window 30m
```

### Ambient capture

```bash
# One-shot sweep of every sensor enabled in policy/ambient.yaml
ctxt capture --ambient

# Restrict to specific sensors (repeatable OR CSV — equivalent)
ctxt capture --ambient --input clipboard
ctxt capture --ambient --input clipboard --input screen
ctxt capture --ambient --input clipboard,screen

# Exclude sensors
ctxt capture --ambient --skip mic
ctxt capture --ambient --skip mic,screen

# Time-bounded ambient: capture state from the last N
ctxt capture --ambient --window 5m
ctxt capture --ambient --window 1h --input mic,screen

# Long-running ambient: re-sweep every 30m. Ctrl-C stops.
ctxt capture --ambient --interval 30m

# Long-running, scoped + windowed
ctxt capture --ambient --input rss,s3 --interval 15m --window 30m
```

### Triage routing

```bash
# Park captures in the inbox instead of running pipelines now —
# composable on every form
ctxt capture https://hnrss.org/frontpage.rss --inbox
ctxt capture ./meeting.m4a --inbox
ctxt capture --ambient --inbox
ctxt capture --ambient --window 1h --inbox
```

### Metadata composition

```bash
ctxt capture https://example.com --hint "#research" --mention "@project.alpha"
ctxt capture --ambient --window 30m --profile founder
ctxt capture ./meeting.m4a --note "client kickoff 2026-05-06"

# Multiple hints (repeatable + CSV)
ctxt capture <url> --hint research --hint draft
ctxt capture <url> --hint research,draft
```

## Behavior details

### One-shot vs. continuous

The presence of `--interval` is the **only** signal for continuous mode. Without it, every form runs once and exits. With it, the form re-reads on the cadence until interrupted. There is no separate `--watch` flag.

### Empty ambient set

If `--ambient` is invoked with no sensors enabled in `policy/ambient.yaml`, the command **errors loud** rather than silently exiting:

```text
Error: ambient capture requires at least one enabled sensor;
edit ~/.config/contexthelp/policy/ambient.yaml — see `ctxt capture --ambient --help`
```

This surfaces config bugs immediately rather than letting users wonder why nothing was captured.

### Sensor permissions

Sensors that need OS-level permissions (TCC on macOS for clipboard/screen/mic, similar on other platforms) declare those requirements in `policy/ambient.yaml`. A sensor whose permission isn't granted is **skipped** during the ambient sweep with a warning printed to stderr; the rest of the sweep continues. The user is directed to the `ctxt doctor --check ambient` command (Phase 2 deliverable) to surface and remediate permission gaps.

### Detector chain

`ctxt capture <source>` runs the source through the existing pipeline detector chain (`pipeline.DetectorFunc`). The chain is extended in Phase 2 to include adapter-supplied detectors (each adapter declares URL/path patterns it handles). The first matching detector wins; `--source <name>` short-circuits the chain.

### Backward compatibility

| Today | Tomorrow |
|---|---|
| `ctxt` (bare; reads clipboard) | `ctxt capture --input clipboard` |
| `ctxt "some text"` | `ctxt capture "some text"` |
| `cat x \| ctxt` | `cat x \| ctxt capture --stdin` |
| `ctxt analyze --file x.md` | `ctxt capture ./x.md` |
| `ctxt analyze --inbox` | `ctxt capture --inbox` |
| `ctxt ingest --source rss --url <url>` | `ctxt capture <url>` (or explicit `--source rss`) |
| `ctxt ingest --source <name> --watch --interval 5m` | `ctxt capture <source> --interval 5m` |

The old forms keep working through the existing `cmd/ctxt/cmd/{analyze,ingest}.go` handlers; `capture` is a new sibling subcommand, not a replacement at the binary level.

### Exit codes

| Code | When |
|---|---|
| 0 | Success — at least one object captured (or one-shot completed cleanly with zero items) |
| 1 | Generic error (network failure, file not found, invalid flag combination) |
| 4 | Policy denied — a CEL rule from `policy/ctxt.yaml` vetoed the capture (mirrors the existing exit-4 mapping from PR #23) |
| 5 | Configuration error — empty ambient set, invalid `policy/ambient.yaml`, missing required permission declarations |

## Operator setup: an OS-level shortcut for `ctxt capture --ambient`

For the "always have eyes/ears on" workflow, bind `ctxt capture --ambient` to a global keyboard shortcut on your platform of choice.

### macOS (Raycast)

1. Install Raycast (https://raycast.com).
2. Settings → Extensions → Script Commands → Add Script Directory.
3. Drop a script `~/raycast-scripts/ctxt-ambient.sh`:

   ```bash
   #!/usr/bin/env bash
   # @raycast.schemaVersion 1
   # @raycast.title Capture ambient (ctxt)
   # @raycast.mode silent
   # @raycast.packageName ctxt
   ctxt capture --ambient --window 30s --inbox
   ```

4. Bind the script to a hotkey (Settings → Extensions → Capture ambient → Hotkey).

### macOS (Shortcuts.app)

Create a Shortcut:
- Action: **Run Shell Script** → `ctxt capture --ambient --window 30s --inbox`
- Settings → Run Without Showing Output
- Add to menu bar / keyboard shortcut → bind your preferred chord.

### macOS (Karabiner-Elements)

Add to `~/.config/karabiner/karabiner.json` under your active profile's `complex_modifications`:

```json
{
  "description": "Cmd+Shift+Space → ctxt capture --ambient",
  "manipulators": [{
    "type": "basic",
    "from": { "key_code": "spacebar", "modifiers": { "mandatory": ["command","shift"] } },
    "to": [{ "shell_command": "/usr/local/bin/ctxt capture --ambient --window 30s --inbox" }]
  }]
}
```

### Linux (GNOME custom shortcut)

Settings → Keyboard → View and Customize Shortcuts → Custom Shortcuts → `+`:
- Name: `ctxt ambient capture`
- Command: `/usr/local/bin/ctxt capture --ambient --window 30s --inbox`
- Shortcut: bind your preferred chord.

### Linux (i3 / sway / hyprland)

Add to your config:

```
bindsym $mod+Shift+space exec --no-startup-id ctxt capture --ambient --window 30s --inbox
```

(`bind = SUPER SHIFT, space, exec, ctxt capture --ambient --window 30s --inbox` for hyprland.)

### Windows (PowerToys Run / AutoHotkey)

PowerToys Run plugin or AutoHotkey script:

```ahk
^+Space::Run, "C:\Program Files\ctxt\ctxt.exe" capture --ambient --window 30s --inbox
```

### iOS / Android (companion app — Phase 3)

Mobile capture invokes the dPKMS instance over HTTP (per `~/.fam` workspace design). Out of scope for Phase 2; tracked under the mobile companion track.

## See also

- [ambient.md](ambient.md) — `policy/ambient.yaml` schema, sensor enablement, OS permissions
- [policies.md](policies.md) — `policy/ctxt.yaml` schema (relocated from `policies.yaml` in PR #23)
- [pipelines.md](pipelines.md) — pipeline runtime, detector chain, lifecycle
- [api-cli.md](api-cli.md) — full CLI reference (Phase 2 update will document `capture` alongside `analyze` and `ingest`)
- [ADR-065](../decisions/ADR-065-pluggable-adapters.md) — substrate decision + 2026-05-06 amendment
