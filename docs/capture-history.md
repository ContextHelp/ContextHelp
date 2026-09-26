# Capture Browser History

`ctxt capture history` sends the pages you visited in a Chromium-family browser to your ctxt server, one browser profile at a time. Run it without a time range and it picks up where the last run stopped. Give it a time range and it backfills that window instead. Your URL rules run on your machine first, so sites you exclude never leave it.

Supported browsers: Chrome, Brave, Edge, Arc, Chromium and Vivaldi. Firefox and Safari are not supported yet.

> Companion to [`ambient.md`](ambient.md). To capture open tabs instead, see [`capture-tabs.md`](capture-tabs.md). To run captures on a timer, see [`capture-schedule.md`](capture-schedule.md).

## First run

Preview the last seven days without sending anything:

```bash
ctxt capture history --browser brave --browser-profile Work --since 7d --dry-run
```

The preview shows how many visits fall in the window, plus a sample of URLs that would be sent and URLs your rules drop, each with the reason. Nothing is sent.

If the preview looks right, run it again without `--dry-run`:

```bash
ctxt capture history --browser brave --browser-profile Work --since 7d
```

Good to know:

- `--browser-profile` takes the name shown in the browser's profile menu (`Work`) or the profile's folder name (`Profile 3`). Don't confuse it with `--profile`, which selects your ctxt focus profile.
- The browser can stay open. ctxt reads a copy of the browser's history and never changes the browser's own files.
- Visits go to the ctxt server you're set up for (`server.url` and its token). To send them to a different instance, add `--instance <name>`; see [multiple instances](cheatsheet-human.md#multiple-instances).

## Keep it running

Leave out the time range:

```bash
ctxt capture history --browser brave --browser-profile Work
```

This sends every visit newer than the last one sent for that browser profile, then saves its position. Each run sends only what's new:

- Restarting your machine or ctxt doesn't send anything twice.
- Visits you made while offline are sent once, on the next run.

To run this automatically, for example every 5 minutes, see [`capture-schedule.md`](capture-schedule.md).

## Backfill a time window

Add any time flag (`--since`, `--until` or `--range`) and the command backfills that window. Backfills leave the saved position alone, so your regular runs aren't affected.

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

A date on its own means the whole day, so `--until 2026-09-07` includes all of 7 September. Dates are read in your local time zone; add `--tz Europe/Paris` to use a different one. Add `--dry-run` to any of these to preview the result first.

All accepted time formats are listed under [Time formats](#time-formats).

## Keep client systems out

Say you use a client's CRM and file share in your `Work` browser profile. To keep them out of every capture from that profile, add a deny list for the profile to your user config (`~/.config/contexthelp/config.yaml`):

```yaml
capture:
  url_filter:
    browsers:
      brave:
        profiles:
          Work:                    # the same name you pass to --browser-profile
            deny:
              - "*://crm.example.net/*"
              - "*://*.example.com/*"
```

Then check that the rules work:

```bash
ctxt capture history --browser brave --browser-profile Work --since 7d --dry-run
```

Denied URLs appear in the sample along with the reason.

What to expect from the rules:

- They're applied on your machine, before anything is sent.
- Deny rules add up across every config file (user, project and `-c`). A project or `-c` file can add denies, but it can't remove yours.
- localhost, `file:`, `about:` and browser-internal pages are always dropped.

Rule syntax, plus rules for every browser or every profile: [URL filter](ambient.md#keep-sites-out-of-browser-capture).

## Reference

### Flags

| Flag | Meaning |
|---|---|
| `--browser <name>` | Which browser: `chrome`, `brave`, `edge`, `arc`, `chromium` or `vivaldi`. |
| `--browser-profile <name>` | Which browser profile: the name shown in the browser, or the profile's folder name. |
| `--since <time>` | Backfill from this time. |
| `--until <time>` | Backfill up to this time. |
| `--range <from>..<to>` | Backfill this window. Repeatable. Can't be combined with `--since` or `--until`. |
| `--tz <zone>` | IANA time zone for reading dates, e.g. `Europe/Paris`. Default: your local time zone. |
| `--dry-run` | Show the count per range and a sample of allowed and denied URLs, with reasons. Sends nothing. |
| `--profile <name>` | Global flag: your ctxt focus profile. Not the browser profile. |
| `--instance <name>` | Global flag: send to this ctxt instance. |

Without `--since`, `--until` or `--range`, the command runs incrementally from the saved position.

### Time formats

`--since`, `--until` and either end of `--range` accept:

| Form | Example | Meaning |
|---|---|---|
| RFC3339 | `2026-09-10T14:30:00+02:00` | That exact moment. |
| Date | `2026-09-10` | As a start (`--since`, left of `..`): the start of that day. As an end (`--until`, right of `..`): the end of that day, so the whole day is included. |
| Relative | `30m`, `12h`, `7d`, `2w` | That many minutes, hours, days or weeks before now. |

Range rules:

- `--range 2026-09-10` means that whole day, the same as `--range 2026-09-10..2026-09-10`. Only a date works without `..`.
- Either end can be left open (`2026-09-01..` or `..2026-09-10`), but not both.
- Overlapping or back-to-back ranges are merged into one.
- An empty, inverted or unreadable range is a usage error: nothing is sent, and the command exits with status 2.

### What gets sent

One entry for each page you actually opened. Pages loaded inside frames are skipped, and a chain of redirects counts as a single visit to the page where it ended.

### Saved position

Incremental runs save their position for each browser and browser profile in:

```text
$XDG_STATE_HOME/ctxt/ambient/browserhistory.state
```

If `XDG_STATE_HOME` isn't set, that's `~/.local/state/ctxt/ambient/browserhistory.state`. Backfills never read or change this file.

To reset the saved position, pause any scheduled captures (see [`capture-schedule.md`](capture-schedule.md)) and then delete the file:

```bash
rm "${XDG_STATE_HOME:-$HOME/.local/state}/ctxt/ambient/browserhistory.state"
```

This resets every browser profile at once.

## Troubleshooting

**"profile not found"**: the error lists the profiles that browser has. Pass one of those names, or the folder name, to `--browser-profile`.

**"ambiguous profile name"**: two or more profiles share that name. The error lists their folder names; pass one of them instead, e.g. `--browser-profile "Profile 3"`.

**Do I need to close the browser?** No. Whether the browser is open or closed, ctxt reads a copy of its history.

**Nothing was sent.** Run the same command with `--dry-run`:

- The count is 0 on an incremental run: nothing new has happened since the last run. That's expected.
- The count is 0 on a backfill: that window has no visits for this profile. Check that you picked the right browser profile and time zone (`--tz`). Chromium browsers usually keep about 90 days of history, so older visits can't be backfilled.
- The count is above 0, but every sample entry is denied: your URL rules drop them. The reason next to each entry tells you which rule applied.

**Exit status 2**: the time range is invalid (empty, inverted or unreadable), or `--range` was combined with `--since` or `--until`. Nothing was sent.

**Firefox or Safari**: not supported yet. Only Chromium-family browsers work for now.
