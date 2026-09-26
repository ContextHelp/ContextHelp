# Workflow: Scheduled Browser Capture (macOS)

## Goal

Capture a browser profile's history and open tabs on a timer, without
running `ctxt capture` by hand. `ctxt capture schedule` installs a
per-user macOS LaunchAgent that runs the capture commands for you.

## Before you start

- macOS. On other platforms the command refuses with an "unsupported"
  error.
- The one-shot commands already work for your profile (see
  [browser-tab-capture.md](./browser-tab-capture.md) and
  [browser-history-capture.md](./browser-history-capture.md)). Run them
  once by hand first; the scheduled jobs run exactly the same thing:

  ```bash
  ctxt capture history --browser chrome --browser-profile Work
  ctxt capture tabs --browser chrome --browser-profile Work
  ```

- Install from the `ctxt` binary you want the schedule to use. The agent
  records that binary's resolved path.

## Install

```bash
ctxt capture schedule install --browser chrome --browser-profile Work
```

```text
installed com.contexthelp.ctxt.capture-history.chrome.work-d3ce02d1
  runs:  ctxt capture history every 5m0s
  plist: ~/Library/LaunchAgents/com.contexthelp.ctxt.capture-history.chrome.work-d3ce02d1.plist
  logs:  ~/Library/Logs/ctxt/capture-history.chrome.work-d3ce02d1.out.log
         ~/Library/Logs/ctxt/capture-history.chrome.work-d3ce02d1.err.log
installed com.contexthelp.ctxt.capture-tabs.chrome.work-d3ce02d1
  runs:  ctxt capture tabs every 30m0s
  ...
```

`--browser` is one of `chrome`, `brave`, `edge`, `arc`, `chromium`,
`vivaldi`. `--browser-profile` is the profile's display name or its
folder name. Quote names that contain spaces:

```bash
ctxt capture schedule install --browser brave --browser-profile "Profile 1"
```

To preview without writing anything or calling `launchctl`, add
`--dry-run`. It prints each plist and where it would go.

## What runs when

Each browser profile gets two agents, one per capture kind:

| Agent   | Command                  | Default interval | Change with       |
|---------|--------------------------|------------------|-------------------|
| history | `ctxt capture history`   | every 5 minutes  | `--history-every` |
| tabs    | `ctxt capture tabs`      | every 30 minutes | `--tabs-every`    |

Both run once right after install and again at every login, then on
their interval. `ctxt capture history` only sends visits made since its
last run, so frequent runs stay cheap. If the Mac is asleep when a run is
due, launchd runs it once on wake rather than catching up on every missed
interval.

Common variations:

```bash
# history only, every 10 minutes
ctxt capture schedule install --browser chrome --browser-profile Work --no-tabs --history-every 10m

# send captures to a specific ctxt instance
ctxt capture schedule install --browser chrome --browser-profile Work --instance work
```

`--instance` and `--profile` given at install time are written into the
scheduled command line. Environment variables such as `CTXT_INSTANCE`
are not carried over, so pass `--instance` explicitly if you rely on
one. Intervals must be at least `1m`.

Re-running `install` replaces the agents in place, so it is also how you
change an interval or target.

## Check it

```bash
ctxt capture schedule list
```

```text
LABEL                                                      KIND     BROWSER  PROFILE  EVERY  LOADED
com.contexthelp.ctxt.capture-history.chrome.work-d3ce02d1  history  chrome   Work     5m0s   true
com.contexthelp.ctxt.capture-tabs.chrome.work-d3ce02d1     tabs     chrome   Work     30m0s  true
```

`LOADED` is whether launchd currently has the job. Add `--format json`
for the full record, including plist and log paths.

## Logs

Each agent writes to its own pair of files in `~/Library/Logs/ctxt/`:

```bash
tail -f ~/Library/Logs/ctxt/capture-history.chrome.work-*.log
```

`.out.log` holds the command's normal output; `.err.log` holds errors,
such as an unreachable server or a profile that no longer exists.

## Uninstall

```bash
ctxt capture schedule uninstall --browser chrome --browser-profile Work
```

This unloads both agents and deletes their plists. Log files stay. Add
`--no-tabs` or `--no-history` to keep that agent. Uninstalling a profile
that has no agents does nothing and succeeds.

## Details

- **Why two agents.** A launchd job has one command and one interval.
  Keeping history and tabs apart gives each its own timer, logs and
  failure state, with no wrapper script in between.
- **Labels.** The label is the kind, the browser, a readable slug of the
  profile name and a short hash of the exact name, so `Work`, `work` and
  `Work!` never collide. The profile name itself is passed to the command
  as a single argument, with no shell involved, so spaces, quotes and
  other characters are safe.
- **Moving or upgrading ctxt.** The agent runs the binary path recorded
  at install time. If that path changes (for example, a package manager
  installs new versions under versioned directories), run `install`
  again.
- **Failed load.** If `launchctl` refuses the job, install removes the
  plist it just wrote, so nothing starts unexpectedly at next login.
