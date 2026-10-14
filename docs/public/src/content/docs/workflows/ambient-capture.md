---
title: "Workflow: Ambient Capture"
description: The ambient capture daemon described in [ADR-066](../../decisions/ADR-066-ambient-capture-substrate.md) (`ctxd`, with `ctxt capture --ambient status|sources|tai...
---

The ambient capture daemon described in [ADR-066](../../decisions/ADR-066-ambient-capture-substrate.md) (`ctxd`, with `ctxt capture --ambient status|sources|tail|stop|buffer`) is not implemented. `ctxt capture --ambient` exits with "ambient mode not yet implemented"; there is no `ctxd` binary, launchd plist, brew service or systemd unit, and no `ambient:` config block.

What runs on your machine today:

| Need | Command | Config |
|---|---|---|
| Clipboard and directory watching | `ctxt watch enable clipboard`, `ctxt watch start`, `ctxt watch status`, `ctxt watch stop` | `watch.dirs`, `watch.patterns`, `watch.debounce`, `watch.clipboard.{enabled,poll_interval,min_length,auto_ingest}` in `ctxt.yaml`; `CTXT_NO_CLIPBOARD=1` turns the clipboard watcher off. See `ctxt watch --help`. |
| Open browser tabs | `ctxt capture tabs` | `capture.browser`, `capture.url_filter` — [browser-tab-capture.md](browser-tab-capture.md) |
| Browser history | `ctxt capture history` | `capture.history.initial_lookback`, `capture.url_filter`; position file `$XDG_STATE_HOME/ctxt/ambient/browserhistory.state` (`CTXT_AMBIENT_HISTORY_STATE_FILE` overrides) — [browser-history-capture.md](browser-history-capture.md) |
| Tabs and history on a timer (macOS) | `ctxt capture schedule install` | [browser-capture-schedule.md](browser-capture-schedule.md) |

The ambient sources, buffer, cutter and local MCP server under `internal/ambient/` are library code that no command runs yet.

## Related references

- [`../../ambient.md`](../../ambient.md) — summary, including the `capture.url_filter` rules
- [`mcp-agents.md`](mcp-agents.md) — the dpkms MCP endpoint
- [`../../architecture/ambient-capture.md`](../../architecture/ambient-capture.md) — design reference for the unbuilt daemon
- [`../../decisions/ADR-066-ambient-capture-substrate.md`](../../decisions/ADR-066-ambient-capture-substrate.md)
