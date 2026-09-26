# Capture Browser History

`ctxt capture history` sends the pages you visited in a Chromium-family browser to your ctxt server, one browser profile at a time. Run it without a time flag and it sends only what's new since the last run. Give it a time window and it backfills that window instead. Your URL rules run on your machine first, so sites you exclude never leave it.

Supported browsers: Chrome, Brave, Edge, Arc, Chromium and Vivaldi. Firefox and Safari are not supported yet.

> Companion to [`ambient.md`](../../ambient.md). To capture open tabs instead, see [`browser-tab-capture.md`](browser-tab-capture.md). To run captures on a timer, see [`browser-capture-schedule.md`](browser-capture-schedule.md).

## First run

Preview the last seven days without sending anything:

```bash
ctxt capture history --browser brave --browser-profile Work --since 7d --dry-run
```

```text
brave profile "Work" (Profile 1): visits since --since; saved position not read
  2026-09-19 11:14:13 EDT .. now: 11 visits: 8 allowed, 2 denied, 1 deduped
  would send  2026-09-20 05:13:24  https://example.com/recent/150h  "Recent 150h"  allowed
  ...
  denied      2026-09-26 07:13:24  https://crm.example.net/deal/7   "Deal 7"       denied by deny rule "crm.example.net" (profile:brave/Work)
  denied      2026-09-26 07:43:24  http://localhost:3000/admin      "Local dev"    denied by deny rule "*://localhost/*" (builtin)
dry run: 8 would be sent, 2 denied, 1 deduped (11 visits); nothing sent
```

The preview counts the visits in the window and lists a sample: up to 10 URLs that would be sent and up to 10 that your rules drop, each with the reason. Nothing is sent.

If the preview looks right, run it again without `--dry-run`:

```bash
ctxt capture history --browser brave --browser-profile Work --since 7d
```

```text
brave profile "Work" (Profile 1): visits since --since; the saved position moves forward to the last one handed off
  2026-09-19 11:14:19 EDT .. now: 11 visits: 8 allowed, 2 denied, 1 deduped
  sent  https://example.com/recent/150h  job job_1
  ...
sent 8, denied 2, deduped 1, failed 0 (11 visits)
position "brave:Profile 1": saved at 2026-09-26 10:13:24 EDT
```

This also saves your position, so from now on a plain run (see [Keep it running](#keep-it-running)) picks up after the last visit sent.

Good to know:

- `--browser-profile` takes the name shown in the browser's profile menu (`Work`) or the profile's folder name (`Profile 3`). Don't confuse it with `--profile`, which selects your ctxt focus profile.
- The browser can stay open. ctxt reads a copy of the browser's history and never changes the browser's own files.
- Visits go to the ctxt server your client is set up for (`server.url` or `server.urls`, with its token), the same way `ctxt analyze` routes. If no configured server answers, each URL is queued locally and runs when `dpkms serve` starts.
- The same URL visited several times is sent once per run.

## Keep it running

Leave out the time flags:

```bash
ctxt capture history --browser brave --browser-profile Work
```

```text
brave profile "Work" (Profile 1): incremental, new visits since the saved position
  2026-09-26 10:13:24 EDT .. now: 0 visits: 0 allowed, 0 denied, 0 deduped
sent 0, denied 0, deduped 0, failed 0 (0 visits)
position "brave:Profile 1": unchanged at 2026-09-26 10:13:24 EDT
```

Each run sends every visit newer than the saved position for that browser profile, then moves the position to the last visit it handed off:

- Restarting your machine or ctxt doesn't send anything twice.
- Visits you made while offline are sent once, on the next run.
- If a send fails, the position stops just before that visit. The next run retries it, so nothing is skipped.

The very first plain run for a browser profile has no saved position yet. It reads back 24 hours. To change that, set `capture.history.initial_lookback` in your ctxt config:

```yaml
capture:
  history:
    initial_lookback: 72h    # any positive Go duration: 90m, 12h, 168h
```

A zero or negative value is rejected with exit status 2. To start further back just once, use `--since` instead (see [First run](#first-run)).

To run this automatically, for example every 5 minutes, see [`browser-capture-schedule.md`](browser-capture-schedule.md).

## Backfill a time window

Add `--until` or `--range` and the command backfills exactly that window. Backfills never read or change the saved position, so your regular runs aren't affected.

```bash
# One day
ctxt capture history --browser brave --browser-profile Work --range 2026-09-10

# One week
ctxt capture history --browser brave --browser-profile Work --since 2026-09-01 --until 2026-09-07

# Several windows in one run
ctxt capture history --browser brave --browser-profile Work \
  --range 2026-09-01..2026-09-03 --range 2026-09-08..2026-09-10

# Everything from a date on, or everything up to a date
ctxt capture history --browser brave --browser-profile Work --range 2026-09-01..
ctxt capture history --browser brave --browser-profile Work --range ..2026-09-10
```

A date on its own means the whole day, so `--until 2026-09-07` includes all of 7 September. Dates are read in your local time zone; add `--tz Europe/Paris` to use a different one. Add `--dry-run` to any of these to preview the result first; with several windows the preview counts each one:

```text
brave profile "Work" (Profile 1): backfill; saved position untouched
  2026-09-01 00:00:00 EDT .. 2026-09-04 00:00:00 EDT: 9 visits: 6 allowed, 3 denied, 0 deduped
  2026-09-08 00:00:00 EDT .. 2026-09-11 00:00:00 EDT: 9 visits: 6 allowed, 3 denied, 0 deduped
```

`--since` on its own is not a backfill: it sends everything from that time to now and moves the saved position forward. To backfill from a date to now without touching the position, use `--range <date>..`.

All accepted time formats are listed under [Time formats](#time-formats).

## Keep client systems out

Say you use a client's CRM and file share in your `Work` browser profile. To keep them out of every capture from that profile, add a deny list for the profile to your user config (`ctxt config paths` lists where it lives):

```yaml
capture:
  url_filter:
    browsers:
      brave:
        profiles:
          Work:                    # the same name you pass to --browser-profile
            deny:
              - "crm.example.net"          # that host, any path
              - "*.files.example.net"      # the domain and every subdomain
```

Then check that the rules work:

```bash
ctxt capture history --browser brave --browser-profile Work --since 7d --dry-run
```

Denied URLs appear in the sample along with the rule that caught them. A real run counts denied URLs but never prints them.

What to expect from the rules:

- They're applied on your machine, before anything is sent.
- Deny rules add up across every config file (user, project and `-c`). A project or `-c` file can add denies, but it can't remove yours.
- A rule without `://` names a host, never part of a URL: `crm.example.net` matches every page on that host and nothing else. A rule with a path or port, such as `crm.example.net/deals`, stops the command with an error; write it as a URL, `https://crm.example.net/deals`.
- Only `http` and `https` pages are captured. localhost and every other scheme (`file:`, `about:`, `chrome:`, ...) are always dropped; the preview shows `builtin: scheme not captured (<scheme>)`.
- If a profile key under `capture.url_filter.browsers.<browser>.profiles` names no profile of that browser, the command warns that its rules apply to nothing.

Rule syntax, plus rules for every browser or every profile: [URL filter](../../ambient.md#keep-sites-out-of-browser-capture).

## Start over

To forget the saved position of one browser profile:

```bash
ctxt capture history --browser brave --browser-profile Work --reset-position
```

Other browser profiles keep their positions. The next plain run starts again from `capture.history.initial_lookback`. `--reset-position` does nothing else and can't be combined with `--since`, `--until` or `--range`. Add `--dry-run` to see which position it would reset.

## Reference

### Modes

| You pass | What is sent | Saved position |
|---|---|---|
| no time flag | Visits after the saved position; on the first run, the last `capture.history.initial_lookback` (24h) | Moved to the last visit handed off |
| `--since X` alone | Visits from X to now | Moved forward to the last visit handed off, never back |
| `--until`, or any `--range` | Visits in that window | Not read, not changed |
| `--dry-run` with any of the above | Nothing | Not read, not changed. A plain dry run previews the first-run window, since it doesn't read the position |

### Flags

| Flag | Meaning |
|---|---|
| `--browser <name>` | Which browser: `chrome`, `brave`, `edge`, `arc`, `chromium` or `vivaldi`. Optional: omitted, it comes from `capture.browser`, the profile name, or the OS default browser; see [Choosing the browser and profile](./browser-tab-capture.md#choosing-the-browser-and-profile). |
| `--browser-profile <name>` | Which browser profile: the name shown in the browser, or the profile's folder name. Optional: omitted, the browser's last-used profile is read. |
| `--since <time>` | Send visits from this time. Alone, it moves the saved position forward; with `--until`, it's a backfill. |
| `--until <time>` | Backfill up to this time. |
| `--range <from>..<to>` | Backfill this window. Repeatable. Can't be combined with `--since` or `--until`. |
| `--tz <zone>` | IANA time zone for reading and showing dates, e.g. `Europe/Paris`. Default: your local time zone. |
| `--reset-position` | Forget this browser profile's saved position, then exit. |
| `--dry-run` | Show the count per window and a sample of allowed and denied URLs, with reasons. Sends nothing; doesn't read or change the saved position. |
| `--profile <name>` | Global flag: your ctxt focus profile, forwarded with every URL. Not the browser profile. |
| `--format json` | Global flag: print one JSON document (see [Scripting](#scripting)). |

### Time formats

`--since`, `--until` and either end of `--range` accept:

| Form | Example | Meaning |
|---|---|---|
| RFC 3339 | `2026-09-10T14:30:00+02:00` | That exact moment. |
| Date and time | `2026-09-10T14:30:00` | That moment in your time zone (or `--tz`). |
| Date | `2026-09-10` | As a start (`--since`, left of `..`): the start of that day. As an end (`--until`, right of `..`): the end of that day, so the whole day is included. |
| Relative | `30m`, `12h`, `7d`, `2w` | That many minutes, hours, days or weeks before now. |

Range rules:

- `--range 2026-09-10` means that whole day, the same as `--range 2026-09-10..2026-09-10`. Only a date works without `..`.
- Either end can be left open (`2026-09-01..` or `..2026-09-10`), but not both.
- Overlapping or back-to-back ranges are merged into one.
- An empty, inverted or unreadable range is a usage error: nothing is sent, and the command exits with status 2.

### What gets sent

One entry for each page you actually opened, sent as its URL the same way `ctxt capture <url>` sends one, so the server picks the pipeline. Pages loaded inside frames are skipped, and a chain of redirects counts as a single visit to the page where it ended.

### Saved position

Positions are kept per browser and browser profile (for example `brave:Profile 1`) in one file:

- `$XDG_STATE_HOME/ctxt/ambient/browserhistory.state` when `XDG_STATE_HOME` is set;
- otherwise `~/Library/Application Support/ctxt/ambient/browserhistory.state` on macOS and `~/.local/state/ctxt/ambient/browserhistory.state` on Linux;
- or the path in `CTXT_AMBIENT_HISTORY_STATE_FILE`.

Run with `-V` to print the path. If the file can't be read as a position file, the command stops before sending anything, names the file and exits with status 3. It never rewrites or resets a damaged file: fix it by hand, or delete it, which resets every browser profile. A bounded backfill still works while the file is damaged, because it doesn't read it.

### Scripting

`--format json` prints one document: the mode (`incremental`, `since` or `backfill`), each window with its counts, the saved position before and after the run, the listed visits with their status (`would_send`, `sent`, `failed`, `denied`) and reason, and a summary:

```bash
ctxt capture history --browser brave --browser-profile Work --since 7d --dry-run --format json | jq '.summary'
```

A real run lists every URL it sent or failed to send; a dry run lists the sample (`"sampled": true`).

### Exit status

| Status | Meaning |
|---|---|
| `0` | Every allowed visit was accepted. |
| `1` | At least one send failed. The others were still sent; the position stops before the first failure. |
| `2` | Bad invocation: unknown browser, unknown or ambiguous profile, bad time value or `--tz`, conflicting flags, or a bad `capture.history.initial_lookback`. Nothing was sent. |
| `3` | The browser, the profile folder or its History database is not on disk, or the saved-position file is damaged. Nothing was sent. |

## Troubleshooting

**`profile not found`**: the error lists the profiles that browser has. Pass one of those names, or the folder name, to `--browser-profile`.

**`ambiguous profile name`**: two or more profiles share that name. The error lists their folder names; pass one of them instead, e.g. `--browser-profile "Profile 3"`.

**`NOT_FOUND: brave: no "Local State" in ...`**: the browser isn't installed in its default location. Point ctxt at the browser's user data directory, for example `CTXT_BRAVE_USER_DATA_DIR` (`CTXT_CHROME_USER_DATA_DIR` and so on for other browsers).

**`NOT_FOUND: ... has no History database`**: the profile has never been used. Open it in the browser once, then re-run.

**`NOT_FOUND: position state file is corrupt: <path>`**: see [Saved position](#saved-position).

**Do I need to close the browser?** No. Whether the browser is open or closed, ctxt reads a copy of its history.

**Nothing was sent.** Look at the counts:

- `0 visits` on a plain run: nothing new since the last run. That's expected.
- `0 visits` on a backfill: that window has no visits for this profile. Check that you picked the right browser profile and time zone (`--tz`). Chromium browsers usually keep about 90 days of history, so older visits can't be backfilled.
- Visits counted, but all denied: your URL rules drop them. Run the same command with `--dry-run`; the reason next to each sample entry tells you which rule applied.

**`GENERIC: capture history: N of M sends failed`**: the listing shows each failed URL with the server's reply. Check the server, then run the same command again. A plain run resends from the first failure on.

**Firefox or Safari**: not supported yet. Only Chromium-family browsers work for now.
