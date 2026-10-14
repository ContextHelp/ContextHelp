# Local Capture Environment Variables

Clipboard access and the browser-history position file. Both are read by
`ctxt` on the machine doing the capture, never by `dpkms`.

---

## Clipboard

### CTXT_NO_CLIPBOARD
**Type:** string\
**Default:** _(unset)_\
**Config key:** none for the input fallback; `watch.clipboard.enabled` for
the watcher

Keeps `ctxt` away from the system clipboard. Two readers, two rules:

| Reader | Turned off by | Effect |
|---|---|---|
| Input fallback of bare `ctxt`, `ctxt analyze`, `ctxt capture`, `ctxt find`, `ctxt show`, `ctxt export` | any non-empty value | With no argument (and, except for `capture`, no piped stdin) the command fails with `no input provided and clipboard is unsupported` instead of reading the clipboard. |
| `ctxt watch` clipboard watcher | exactly `1` | The watcher never polls, whatever `watch.clipboard.enabled` says; `ctxt watch status` reports `disabled (CTXT_NO_CLIPBOARD=1)`. |

Set it to `1` to cover both. Scripts, CI and tests use it so a run never
picks up whatever the operator last copied.

```bash
CTXT_NO_CLIPBOARD=1 ctxt watch start
```

Source: `internal/cli/input.go`, `cmd/ctxt/cmd/capture.go`,
`internal/watcher/clipboard.go`, `cmd/ctxt/cmd/watch.go`.

---

## Browser history

### CTXT_AMBIENT_HISTORY_STATE_FILE
**Type:** string (path)\
**Default:** `<state dir>/ctxt/ambient/browserhistory.state`

Path of the position file `ctxt capture history` keeps: the last visit
handed off, per dpkms instance and browser profile. The state dir is
`$XDG_STATE_HOME`, else `~/.local/state` on Linux and
`~/Library/Application Support` on macOS.

The file is created on the first successful run (mode `0600`, directory
`0700`) and locked through a `<file>.lock` sidecar while it is updated. A
file that cannot be parsed, or one from another format version, stops the
run with exit 3 and is never rewritten — fix or delete it; the next
incremental run then starts from `capture.history.initial_lookback`.
`--reset-position` clears one browser profile's entry. Backfills
(`--until`, `--range`) neither read nor move the file.

```bash
CTXT_AMBIENT_HISTORY_STATE_FILE=/tmp/history-test.state ctxt capture history --browser brave
```

Source: `internal/ambient/position/position.go`. See
[browser-history-capture.md](../manual/workflows/browser-history-capture.md).

---

## Related

- [../ctxt/capture.md](../ctxt/capture.md) — `ctxt capture`
- [../manual/workflows/ambient-capture.md](../manual/workflows/ambient-capture.md) — what runs locally today
