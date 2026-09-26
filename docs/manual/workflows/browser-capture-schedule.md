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
  records that binary's path. A Homebrew install records the
  `/opt/homebrew/bin/ctxt` (or `/usr/local/bin/ctxt`) link rather than
  the versioned Cellar path, so `brew upgrade` does not break it.

## Install

```bash
ctxt capture schedule install --browser chrome --browser-profile Work
```

```text
installed com.contexthelp.ctxt.capture-history.chrome.profile-1-99aa3634
  runs:  ctxt capture history every 5m0s
  plist: ~/Library/LaunchAgents/com.contexthelp.ctxt.capture-history.chrome.profile-1-99aa3634.plist
  logs:  ~/Library/Logs/ctxt/capture-history.chrome.profile-1-99aa3634.out.log
         ~/Library/Logs/ctxt/capture-history.chrome.profile-1-99aa3634.err.log
installed com.contexthelp.ctxt.capture-tabs.chrome.profile-1-99aa3634
  runs:  ctxt capture tabs every 30m0s
  ...
```

(Here Chrome keeps the "Work" profile in the folder `Profile 1`.)

`--browser` is one of `chrome`, `brave`, `edge`, `arc`, `chromium`,
`vivaldi`. `--browser-profile` is the profile's display name or its
folder name. Quote names that contain spaces:

```bash
ctxt capture schedule install --browser brave --browser-profile "Profile 1"
```

Both flags are optional and are resolved the way `ctxt capture tabs`
resolves them: `capture.browser`, then the one installed browser that
has the named profile, then the OS default browser; an omitted profile
is the browser's last-used one. See
[Choosing the browser and profile](./browser-tab-capture.md#choosing-the-browser-and-profile),
and run `ctxt capture browsers` to see what would be picked.

Install resolves once and writes the result into the agents: the
browser and the profile **folder**. The scheduled runs never
auto-select, so changing your default browser or switching profiles
later does not move them. A name that matches no profile, or several,
is refused with exit status `2` and a list of candidates, and nothing
is installed.

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
LABEL                                                           KIND     BROWSER  PROFILE    EVERY  LOADED
com.contexthelp.ctxt.capture-history.chrome.profile-1-99aa3634  history  chrome   Profile 1  5m0s   true
com.contexthelp.ctxt.capture-tabs.chrome.profile-1-99aa3634     tabs     chrome   Profile 1  30m0s  true
```

`PROFILE` is the profile folder written into the agent.

`LOADED` is whether launchd currently has the job. Add `--format json`
for the full record, including plist and log paths.

## Logs

Each agent writes to its own pair of files in `~/Library/Logs/ctxt/`:

```bash
tail -f ~/Library/Logs/ctxt/capture-history.chrome.profile-1-*.log
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

Uninstall resolves `--browser` and `--browser-profile` the same way
install does, so the flags you installed with remove the same agents.
If the profile has since been deleted from the browser, name it by the
folder shown in `ctxt capture schedule list`:

```bash
ctxt capture schedule uninstall --browser chrome --browser-profile "Profile 1"
```

## Details

- **Why two agents.** A launchd job has one command and one interval.
  Keeping history and tabs apart gives each its own timer, logs and
  failure state, with no wrapper script in between.
- **Labels.** The label is the kind, the browser, a readable slug of the
  profile folder and a short hash of the exact folder name, so labels
  never collide. The folder is stable: renaming the profile in the
  browser does not change it. It is passed to the command as a single
  argument, with no shell involved, so spaces and other characters are
  safe.
- **Moving or upgrading ctxt.** The agent runs the binary path recorded
  at install time. Homebrew upgrades are covered (the `bin/ctxt` link is
  recorded, not the versioned Cellar directory). If you move the binary
  anywhere else, run `install` again.
- **Failed load.** If `launchctl` refuses the job, install removes the
  plist it just wrote, so nothing starts unexpectedly at next login.
