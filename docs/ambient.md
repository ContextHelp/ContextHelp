# Ambient Capture

Local-machine capture for ctxt: commands that run on your machine, read local state (clipboard, watched directories, browser tabs and history) and send it to the dpkms instance ctxt resolves.

> The continuous ambient daemon (`ctxd`) from [ADR-066](decisions/ADR-066-ambient-capture-substrate.md), with its six sources, client-side buffer, sessions ([ADR-067](decisions/ADR-067-session-workunit.md)) and local MCP server ([ADR-068](decisions/ADR-068-mcp-read-surface.md)), is not implemented. `ctxt capture --ambient` exits with "ambient mode not yet implemented", and there is no `ambient:` config block. The source, buffer and cutter code under `internal/ambient/` is library code no command runs yet. See [Not implemented](#not-implemented).

## What runs today

| Need | Command | Config |
|---|---|---|
| Clipboard and directory watching | `ctxt watch enable clipboard`, `ctxt watch start` / `status` / `stop` | `watch.dirs`, `watch.patterns`, `watch.debounce`, `watch.clipboard.{enabled,poll_interval,min_length,auto_ingest}`; `CTXT_NO_CLIPBOARD=1` turns the clipboard watcher off |
| Open browser tabs | `ctxt capture tabs` | `capture.browser`, `capture.url_filter` |
| Browser history | `ctxt capture history` | `capture.history.initial_lookback`, `capture.url_filter` |
| Tabs and history on a timer (macOS LaunchAgents) | `ctxt capture schedule install` | `--history-every` (default 5m), `--tabs-every` (default 30m) |

Config lives in `ctxt.yaml` (see [`ctxt/configuration.md`](ctxt/configuration.md)). Local state:

- `ctxt watch`: one state file per instance under `$XDG_STATE_HOME/ctxt/watch/`.
- `ctxt capture history`: position file `$XDG_STATE_HOME/ctxt/ambient/browserhistory.state`, overridden by `CTXT_AMBIENT_HISTORY_STATE_FILE`.

Walkthroughs: [`manual/workflows/browser-tab-capture.md`](manual/workflows/browser-tab-capture.md), [`manual/workflows/browser-history-capture.md`](manual/workflows/browser-history-capture.md), [`manual/workflows/browser-capture-schedule.md`](manual/workflows/browser-capture-schedule.md).

## Why client-side

dpkms is often deployed remote (managed instance, household NAS, federated peer). A remote dpkms cannot read your clipboard or your browser profile. These commands run in `ctxt` on your machine and send content over the dpkms HTTP API, the same path `ctxt capture <url>` and `ctxt analyze` use. dpkms remains a pipeline + storage worker.

## Privacy posture

Browser capture filters every URL on your machine, before anything is sent:

- Only `http` and `https` pages are captured; localhost and loopback addresses are always dropped.
- URLs matching your [URL filter](#keep-sites-out-of-browser-capture) are dropped.
- `--dry-run` on `ctxt capture tabs` and `ctxt capture history` shows each decision and sends nothing.

The clipboard watcher is off until you enable it (`watch.clipboard.enabled`, or `ctxt watch enable clipboard`).

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

## Composition

Compose what was captured in a time range:

```bash
ctxt compose summary --since 2026-10-12T13:00:00Z --until 2026-10-12T15:30:00Z
ctxt compose brief --since 2026-10-12
```

`--since` / `--until` take `YYYY-MM-DD` or RFC 3339 and filter on creation time.

## Agent integration (MCP)

dpkms serves one MCP endpoint, `/api/v1/mcp/`, with the tools `search` and `schema`. Register it in your agent client yourself; there is no `ctxt mcp` command. Guide: [`manual/workflows/mcp-agents.md`](manual/workflows/mcp-agents.md).

## Not implemented

Specced in ADR-066 to ADR-069, not built:

- The `ctxd` daemon and its service files (brew service, launchd plist, systemd unit), and `ctxt capture --ambient` with `status`, `sources`, `tail`, `stop`, `buffer flush|purge`.
- The foreground-window, screenshot, file-watch and meeting sources as running capture, and source-side redaction in the capture path.
- The client-side buffer (local-fs / S3 / memory) and the `ambient.buffer`, `ambient.sources` and `ambient.session` config blocks.
- Work sessions (`ctxt session ...`); see [`manual/workflows/sessions.md`](manual/workflows/sessions.md).
- The local MCP server on port 8744 and its tools.
- `policy/ambient.yaml`; see [`ctxt/ambient.md`](ctxt/ambient.md).

## Related

- [`manual/workflows/ambient-capture.md`](manual/workflows/ambient-capture.md) — what to run today
- [`architecture/ambient-capture.md`](architecture/ambient-capture.md) — design reference for the unbuilt daemon
- [`manual/workflows/mcp-agents.md`](manual/workflows/mcp-agents.md) — agent integration
- [`diagrams/ambient/`](diagrams/ambient/) — design diagrams (substrate, sessions, MCP, meeting capture)
