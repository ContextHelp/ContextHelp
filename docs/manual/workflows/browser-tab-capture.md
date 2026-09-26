# Workflow: Capture Open Browser Tabs

## Goal

Send the tabs open in one browser profile to ctxt with one command, without a browser extension, while keeping chosen sites on your machine.

## Scope

- Chromium-family browsers: `chrome`, `brave`, `edge`, `arc`, `chromium`, `vivaldi`
- One browser profile per run
- Filtering with `capture.url_filter`, previewed with `--dry-run`
- Not covered: page HTML, text selections, page elements (the browser extension in `US-0207`, not built yet)

## Primary stories

- `US-0207` (web tab capture): `ctxt capture tabs` is the extension-free path for open tabs
- `US-0214` (browser history source): uses the same `capture.url_filter` rules

## Prerequisites

1. A ctxt server your client is configured to use (`server.url` or `server.urls`). Tabs go to that server, with its token, exactly as `ctxt analyze` routes. If no configured server answers, each URL is queued locally and runs when `dpkms serve` starts.
2. You have used the browser profile at least once, so it has a session on disk. The browser does not need to be running.

## Procedure

### Step 1: Keep private sites out

Add deny rules to your ctxt config for anything that must never leave the machine. Rules can apply to every browser, to one browser, or to one browser profile:

```yaml
capture:
  url_filter:
    deny:                        # every browser, every profile
      - "*://*.bank.example.com/*"
    browsers:
      brave:
        profiles:
          Work:                  # profile name, or folder name ("Profile 1")
            deny:
              - "*://crm.example.net/*"
              - "*://drive.example.com/*"
```

`*://crm.example.net/*` matches that host on any scheme, port and path; `*://*.example.net/*` adds every subdomain. localhost, `file:` and browser-internal pages are always dropped. Full rule syntax and `allow_only` lists: [Keep sites out of browser capture](../../ambient.md#keep-sites-out-of-browser-capture).

Deny rules add up across config files: your user config, a project `.contexthelp/ctxt.yaml`, each `-c <file>` and each `-c key=value`. No project or `-c` file can drop a rule from your user config; to stop denying a site, delete the rule from the file that holds it (`ctxt config paths` lists them). Each layer's `allow_only` list is one more list a URL must match, so a later layer can narrow capture but never widen it.

### Step 2: Preview with a dry run

```bash
ctxt capture tabs --browser brave --browser-profile Work --dry-run
```

```text
brave profile "Work" (Profile 1): session as of 2026-09-26 10:34:23 (6m ago)
  would send  https://example.com/a            "Alpha page"   allowed
  denied      https://crm.example.net/deal/42  "Deal 42"      denied by deny rule "*://crm.example.net/*" (profile:brave/Work)
  duplicate   https://example.com/a            "Alpha again"  duplicate of an earlier tab
  denied      http://localhost:3000/           "Local dev"    denied by deny rule "*://localhost/*" (builtin)
  would send  https://github.com/example/repo  "Repo"         allowed
dry run: 2 would be sent, 2 denied, 1 deduped (5 tabs); nothing sent
```

Every tab is listed with the filter's decision and the rule behind it. Nothing is sent. The same URL open in several tabs is sent once.

### Step 3: Send the tabs

```bash
ctxt capture tabs --browser brave --browser-profile Work
```

```text
brave profile "Work" (Profile 1): session as of 2026-09-26 10:34:23 (6m ago)
  sent  https://example.com/a            job job_1
  sent  https://github.com/example/repo  job job_2
sent 2, denied 2, deduped 1, failed 0 (5 tabs)
```

Each URL is enqueued the same way `ctxt capture <url>` enqueues one, so the server picks the pipeline. Denied URLs are counted, never printed. To file the captures under a focus profile, add `--profile <focus-profile>`.

## What "session as of" means

The tabs come from the session file the browser keeps in the profile folder. "Session as of" is when the browser last wrote it:

- Browser running: seconds to minutes ago. The browser saves after every tab change.
- Browser closed: the time it quit. You get the tabs that were open then.

When the session is older than 15 minutes, the command warns that the browser may be closed. The capture still runs. For the current tabs, open the browser and run the command again.

## Choosing the profile

`--browser-profile` takes the name shown in the browser's profile picker (`Work`, any letter case) or the profile folder name (`"Profile 1"`). If two profiles share a name, the command stops and lists the folders; pass the folder name instead:

```text
USAGE: ambiguous profile name: brave has 2 profiles named "Personal" (use a folder name: "Personal" (Profile 2), "Personal" (Profile 3))
```

The browser's own `chrome://version` page (`brave://version` in Brave) shows the folder under "Profile Path".

## Scripting

`--format json` prints one document with the session time, every tab's status (`would_send`, `sent`, `failed`, `denied`, `duplicate`), its reason, and a summary:

```bash
ctxt capture tabs --browser brave --browser-profile Work --dry-run --format json | jq '.summary'
```

Exit status: `0` all allowed tabs sent, `1` at least one send failed (the others were still sent), `2` bad invocation (unknown browser, unknown or ambiguous profile), `3` the browser, profile folder or session file is not on disk.

## Outputs to validate

- The dry run lists every tab you expected, with the decision you expected
- Tabs from private sites show `denied` with the rule that caught them
- The real run reports `failed 0`

## Common failure modes

### `warning: capture.url_filter.browsers.brave.profiles.<key> matches no brave profile`

The profile key in your config names no profile of that browser, so its rules protect nothing. Fix the key to the profile's name or folder name.

### `NOT_FOUND: brave: no "Local State" in ...`

The browser is not installed in its default location. Point ctxt at the browser's user data directory, for example `CTXT_BRAVE_USER_DATA_DIR` (`CTXT_CHROME_USER_DATA_DIR` and so on for other browsers).

### `NOT_FOUND: ... has no session file`

The profile has never been opened. Open it in the browser once, then re-run.

### `GENERIC: capture tabs: N of M sends failed`

The listing shows each failed URL with the server's reply. Check the server, then re-run; every allowed tab is sent again.

## Related references

- [`ingestion-capture.md`](./ingestion-capture.md)
- [`browser-history-capture.md`](./browser-history-capture.md) (browser history and backfill)
- [`../../ambient.md`](../../ambient.md) (full filter rules)
- [`../../stories/capture/US-0207-web-tab-capture.md`](../../stories/capture/US-0207-web-tab-capture.md)
