# Workflow: Sign In to the Web UI

## Goal

Open the dpkms web UI (`/ui/`) of a protected or public instance in your browser, signed in as the principal of your ctxt API token, without ever pasting that token into the browser.

A private instance (the default, bound to `127.0.0.1`) needs no sign-in; `ctxt ui open` just opens its `/ui/`.

## Prerequisites

- ctxt is configured to reach the instance with a token it accepts: `server.url` (or a `server.urls` entry) and `server.token` (or that entry's own `token`).
- You reach the instance over HTTPS, or over HTTP on `localhost` / `127.0.0.1`. Browsers keep the session cookie only there; see [Sign-in does not stick](#sign-in-does-not-stick).

## Quick start

```bash
ctxt ui open
```

ctxt asks the instance for a single-use sign-in link and opens it in your default browser. The page shows who you are signed in as:

```text
Signed in as ops on dpkms.example.net
```

Check the name, then click **Continue**. If you did not start this sign-in, click **Not you? Sign out**.

`ctxt ui open` signs in to the instance every other ctxt command talks to. To sign in to another one, name it, or give its URL:

```bash
ctxt ui open --instance home
ctxt ui open --server https://dpkms.example.net
```

The token comes from that instance's `server.urls` entry, or from `server.token`.

## Open the link somewhere else

On a machine without a browser, or to open the link in a specific browser, print it instead:

```bash
ctxt ui open --no-browser
```

```text
Single-use sign-in link for http://127.0.0.1:8081, valid until 14:03:12:
http://127.0.0.1:8081/ui/auth#code=yXwMtd…
```

The link goes to stdout, the note to stderr. ctxt also prints the link, and opens nothing, whenever stdout or stderr is not a terminal (a pipe, a script, CI).

The link works once, for 60 seconds. Run `ctxt ui open` again for a new one.

## What the browser session can do

The session acts as your token's principal, so entitlements and quotas apply as for the token, but with a reduced scope:

| Allowed | Refused (`403 SESSION_SCOPE`) |
|---|---|
| Reading and searching objects, entities, jobs, inbox, feeds, aliases, suggestions, saved searches, search history, reminders, import runs, and the step registry list | Pipelines, steps, registry changes, watches, the audit log, federation, the MCP mount |
| The web UI's own actions: delete an object, retry a job, sign out | Every other write (capture, analyze, inbox triage, feed and alias changes, …) |
| The live event stream | Minting new sign-in links |

The Registry page lists registries in a browser session; to fetch or update one, use `ctxt registry` or `dpkms step registry`.

## See a search as a graph

On the Search page, type a query and click **View as graph** next to **Search**. The search graph viewer opens in the same tab for the query in the box (`/ui/searchgraph/?q=<query>`), showing every candidate the search scored and why; browser Back returns to the Search page. The action is greyed out while the box is empty.

The graph always runs the free-text hybrid search, so a query-language expression such as `type==note` is searched as plain words there. How to read it: [Search graph](search-graph.md#read-the-graph).

## How long a session lasts

A session ends:

- after 12 hours without a request (`server.ui.session.idle_ttl`),
- 7 days after sign-in, however active (`server.ui.session.max_ttl`),
- when you click sign out (the arrow next to your name in the sidebar),
- when an operator revokes it (`dpkms session revoke`),
- as soon as the token that signed you in is removed from the server's `server.auth.static.tokens` and dpkms restarts. Putting the token back does not revive the session.

When it ends, the next page load shows how to sign in again.

## Validation checklist

- The sign-in page named the principal you expected.
- The sidebar shows your principal and a green dot (live updates connected).
- `dpkms session list` on the server lists your session as `active`.

## Common failure modes

### This sign-in link is invalid, used or expired

The link was opened twice, or more than 60 seconds after `ctxt ui open`. Run `ctxt ui open` again.

### dpkms … refused to mint a sign-in link (exit 5)

The instance did not accept ctxt's token. Set `server.token` (or the token of the matching `server.urls` entry) to one of the instance's `server.auth.static.tokens`.

### Sign-in does not stick

The page says the browser did not keep the session cookie. The cookie is `Secure`, so browsers drop it over plain HTTP to any host but `localhost`. Reach the instance through a TLS-terminating reverse proxy. dpkms also logs a warning when a sign-in arrives over plain HTTP under a non-loopback name.

### Not available to a web UI session

The page or call needs more than the browser scope allows (see the table above). Use the ctxt CLI or an API token.

## For operators

Session bounds live in the server config:

```yaml
server:
  ui:
    session:
      idle_ttl: 12h
      max_ttl: 168h
```

List and revoke sessions on the server host; both read the database directly, and a running dpkms refuses a revoked cookie on its next request:

```bash
dpkms session list                  # active sessions, newest first
dpkms session list --all            # ended ones too
dpkms session revoke uis_3f2a…      # one browser
dpkms session revoke --principal ops  # every browser of a principal
```

The status column reads `active`, `expired`, `revoked (<reason>)` or `token removed`. Sessions are stored in the instance database (`ui_sessions`), with only SHA-256 hashes of the cookie secret, the login code and the minting token; ended sessions are pruned 30 days after they end.

Details of the cookie and CSRF rules: [Config and permissions](../reference/config-and-permissions.md#web-ui-sessions).
