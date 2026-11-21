# Workflow: Keep Sites Out of Browser Capture

## Goal

Decide which pages `ctxt capture tabs` and `ctxt capture history` may send, using deny rules and optional allow-only lists in your config. Rules run on your machine before anything is sent.

## Scope

- `capture.url_filter`: global, per-browser and per-profile rules
- `capture.browser` and `capture.history.initial_lookback`
- Platform and browser support for browser capture
- Not covered: the commands themselves ([tabs](./browser-tab-capture.md), [history](./browser-history-capture.md), [schedule](./browser-capture-schedule.md))

## Rules

```yaml
capture:
  browser: brave                  # default for --browser
  url_filter:
    deny:                         # every browser, every profile
      - "*.bank.example.com"
    browsers:
      brave:
        deny: []                  # every Brave profile
        profiles:
          Work:                   # profile name, or folder name ("Profile 1")
            deny:
              - "crm.example.net"
              - "https://drive.example.com/shared"
            allow_only: []        # optional: capture ONLY these
```

| Rule | Matches |
|---|---|
| `crm.example.net` | that host: any scheme, port, path or query |
| `*.example.net` | `example.net` and every subdomain |
| `*://crm.example.net/*` | the same host, written as a URL pattern |
| `https://drive.example.com/shared` | that scheme, host and path, and anything below the path |
| `*://localhost:8080/*` | that port only; no port in the rule means any port |

A rule without `://` is a host and nothing else. `crm.example.net/deals`, `localhost:8080`, `user@crm.example.net` and a bare `*` are rejected with an error naming the rule; write `https://crm.example.net/deals` or `*://localhost:8080/*`. Quote rules that start with `*` in YAML. Userinfo in a URL-form rule is rejected too.

Host matching ignores letter case, userinfo and a trailing dot, and compares internationalized hosts in punycode. A host that appears only in a path or query never matches.

### Always applied

- Only `http` and `https` pages pass. Every other scheme (`file:`, `about:`, `data:`, `ftp:`, `chrome:`, `mailto:` and so on) is dropped before any rule runs. A dry run shows `builtin: scheme not captured (<scheme>)`.
- `localhost`, `*.localhost`, `127.0.0.1` and `[::1]` are denied.
- A URL that cannot be parsed is dropped.

### Allow-only

`allow_only` at any scope is a list a URL must match to be captured. Deny wins over `allow_only`. Every `allow_only` list that applies to the profile (global, browser, profile) must be satisfied, so scopes only narrow, never widen. A URL that matches none is reported as `denied: no allow_only rule matched (<scope>)`.

```yaml
capture:
  url_filter:
    browsers:
      chrome:
        profiles:
          Research:
            allow_only:
              - "*.wikipedia.org"
              - "arxiv.org"
```

### Scope and keys

Browser keys and profile keys compare without regard to letter case. A profile key matches the profile's display name or its folder name. A key that matches no profile of that browser prints a warning, because its rules protect nothing.

### Layers

Rules add up across config layers: your user config, a project `.contexthelp/ctxt.yaml`, each `-c <file>` and each `-c key=value`. Deny lists at each scope are the union of every layer's list. No layer can remove a rule; an empty list, `null` or a missing key changes nothing. Delete a rule from the file that holds it (`ctxt config paths` lists them). Each layer's `allow_only` list is one more gate. This is the one key that does not follow "later layer wins".

A bare `-c capture.url_filter.deny=crm.example.net` is not a list and fails; pass a YAML list:

```bash
ctxt capture tabs -c 'capture.url_filter.deny=["crm.example.net"]' --dry-run
```

### Preview

`--dry-run` on `capture tabs` and `capture history` lists each URL with its decision and the rule or scope behind it, for example `denied by deny rule "crm.example.net" (profile:brave/Work)`. Scope labels are `builtin`, `global`, `browser:<name>` and `profile:<browser>/<profile>`.

## Other capture keys

| Key | Default | Meaning |
|---|---|---|
| `capture.browser` | empty (auto-select) | Browser used when `--browser` is not given: `chrome`, `brave`, `edge`, `arc`, `chromium`, `vivaldi` |
| `capture.history.initial_lookback` | `24h` | How far back the first plain `ctxt capture history` run for a profile reads; must be positive |

## Platform and browser support

| Browser | macOS | Linux | Windows |
|---|---|---|---|
| Chrome | yes | yes | yes |
| Chromium | yes | yes | yes |
| Brave | yes | yes | yes |
| Edge | yes | yes | yes |
| Vivaldi | yes | yes | yes |
| Arc | yes | no | no |

"Yes" means ctxt knows the browser's default profile location on that OS. Locations:

| OS | Root | Example (Brave) |
|---|---|---|
| macOS | `~/Library/Application Support` | `BraveSoftware/Brave-Browser` |
| Linux | `$XDG_CONFIG_HOME`, else `~/.config` (Chrome and Chromium first honour `$CHROME_CONFIG_HOME`) | `BraveSoftware/Brave-Browser` |
| Windows | `%LOCALAPPDATA%` | `BraveSoftware\Brave-Browser\User Data` |

For a browser installed elsewhere, set `CTXT_<BROWSER>_USER_DATA_DIR`, for example `CTXT_BRAVE_USER_DATA_DIR`. A browser with no known location on your OS fails with "browser not supported on this OS".

Detecting the OS default browser (used when no browser is chosen) works on all three systems: macOS reads the system URL-handler preferences, Linux asks `xdg-settings get default-web-browser`, Windows reads the `https` association from the registry. A default that is not one of the supported browsers is not used.

Scheduled capture (`ctxt capture schedule`) is macOS only.

## Common failure modes

### `warning: capture.url_filter.browsers.<browser>.profiles.<key> matches no <browser> profile`

Fix the key to a profile's display name or folder name; `ctxt capture browsers` lists both.

### A rule fails the command with its text in the error

The rule is not a valid host or URL pattern. Use the forms in the table above.

## Related references

- [`browser-tab-capture.md`](./browser-tab-capture.md)
- [`browser-history-capture.md`](./browser-history-capture.md)
- [`browser-capture-schedule.md`](./browser-capture-schedule.md)
