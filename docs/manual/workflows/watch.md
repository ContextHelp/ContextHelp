# Workflow: Watch Folders and the Clipboard

## Goal

Send new and changed files from chosen folders, and optionally what you copy, to your ctxt server while you work, without running `ctxt capture` by hand.

## Scope

- Directory watcher: notes folders, Obsidian and Logseq vaults
- Clipboard watcher (opt-in)
- Start, stop and inspect the watcher for one instance
- Not covered: browser tabs and history ([tabs](./browser-tab-capture.md), [history](./browser-history-capture.md)), server-side watches (`/api/v1/watches`), which are separate and never see the records `ctxt watch` keeps

## Prerequisites

1. A dpkms instance your client resolves (`--server`, `--instance`, `CTXT_INSTANCE`, the current instance, else `server.urls`), and a token that may write. Deleting the object of a deleted file needs `delete:objects`, which is an admin token on a protected instance.
2. At least one directory in `watch.dirs`, or the clipboard watcher enabled.

## Procedure

### Step 1: Configure

```yaml
watch:
  dirs:
    - ~/notes
  patterns:
    - "**/*.md"
  debounce: 500ms
  clipboard:
    enabled: false
    poll_interval: 2s
    min_length: 80
    auto_ingest: true
```

| Key | Default | Meaning |
|---|---|---|
| `watch.dirs` | none | Directories to watch. `~` expands to your home. A path that is not an existing directory stops the command with a usage error |
| `watch.patterns` | none | Glob patterns files must match, relative to the watched directory. With none set, every file matches |
| `watch.debounce` | `500ms` | Wait after a change before sending the file |
| `watch.clipboard.enabled` | `false` | Turn the clipboard watcher on |
| `watch.clipboard.poll_interval` | `2s` | How often the clipboard is read |
| `watch.clipboard.min_length` | `80` | Minimum characters for plain text to qualify |
| `watch.clipboard.auto_ingest` | `true` | Send qualifying clips without asking; when `false`, nothing is sent |

`ctxt config path` shows which file to edit.

### Step 2: Enable the clipboard (optional)

```bash
ctxt watch enable clipboard
```

This writes `watch.clipboard.enabled: true` to your config. `ctxt watch disable clipboard` turns it off. `clipboard` is the only name either command accepts. Setting `CTXT_NO_CLIPBOARD=1` turns the clipboard watcher off whatever the config says.

A clip is sent when it changed since the last one and is a URL, contains a fenced code block, or has at least `min_length` characters.

### Step 3: Start

```bash
ctxt watch start
```

```text
Watching for instance home (http://127.0.0.1:8080)
State file: ~/.local/state/ctxt/watch/home.state
Clipboard watcher: disabled
Directory watcher: /Users/you/notes (mode=generic)
Watchers running. Press Ctrl-C or run 'ctxt watch stop' to stop.
```

The command blocks. On start every watched directory is scanned once: files that changed since the last run against this instance are sent, unchanged ones are not. After that, each change is sent after the debounce. A deleted file deletes the object it produced.

The mode names how a folder is read. A folder holding `.obsidian` is `obsidian`, one holding `logseq` is `logseq`, and any other is `generic`. The mode is part of each file's source label (`watch:<mode>:<relative path>`). In generic mode `.md` and `.markdown` files are sent as text, `.pdf` as `pdf`, `.html` and `.htm` as `url`, and every other file as text. Files are routed to pipelines as described in [Capture](./capture-content.md#how-the-server-picks-the-pipeline).

Set `CTXT_WATCH_POLLING=1` to poll for changes instead of using file-system events, for folders where events are unreliable.

What was already sent is kept per instance in one state file under `$XDG_STATE_HOME/ctxt/watch/`. Pointing the watcher at a different instance sends each file to it once.

### Step 4: Check and stop

```bash
ctxt watch status
```

```text
Instance          : home (http://127.0.0.1:8080)
Watcher           : running (pid 4242, since 2026-11-18 09:12:03)
Clipboard watcher : disabled
State file        : ~/.local/state/ctxt/watch/home.state

Path                  Configured  Watching  Files  Last Enqueue         Last Error
/Users/you/notes      yes         yes       128    2026-11-18 09:40:11  -
```

`status` reads this machine's files only and never calls the server. `--format json` prints the instance, the process, the clipboard setting, the state file and one entry per directory.

```bash
ctxt watch stop
```

`stop` signals the running watcher for the resolved instance and waits up to 15 seconds for it to exit. It succeeds when none is running, and removes a stale run file if it finds one.

## Outputs to validate

- `ctxt watch status` shows `running` and each directory as `Configured yes`, `Watching yes`
- The `Files` count rises after you save a file in a watched directory
- `Last Error` stays `-`; a failed send is recorded there and retried at the file's next change or the next start

## Common failure modes

### `nothing to watch: watch.dirs is empty and the clipboard watcher is disabled`

Exit status `2`. Add directories to `watch.dirs` or run `ctxt watch enable clipboard`.

### `watch.dirs entry "..." is not a directory`

Create the directory or remove it from `watch.dirs`.

### Another watcher for this instance is running

Exit status `4`. Run `ctxt watch stop`, or watch another instance with `--instance`.

### Nothing answers, or the token is rejected

`start` checks the server before watching anything: exit status `70` when nothing answers, `5` when the token is rejected.

### A deleted file's object stays

The token lacks `delete:objects`. Use an admin token; the failure shows under `Last Error`.

## Related references

- [`capture-content.md`](./capture-content.md)
- [`ingestion-capture.md`](./ingestion-capture.md)
- [`../reference/config-and-permissions.md`](../reference/config-and-permissions.md)
