# Workflow: Sign In to the Web UI

## Goal

Open the dpkms web UI (`/ui/`) of a protected or public instance in your browser, signed in as the principal of your ctxt API key, without ever pasting that key into the browser.

A private instance (the default, bound to `127.0.0.1`) needs no sign-in; `ctxt ui open` just opens its `/ui/`.

## Prerequisites

- ctxt is configured to reach the instance with an API key it accepts: `server.url` (or a `server.urls` entry) and `server.token` (or that entry's own `token`). The key was created on the dpkms host, with dpkms stopped, with `dpkms auth key create --maintenance-override`; a key declared in the server's config by hash cannot sign a browser in.
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

The key comes from that instance's `server.urls` entry, or from `server.token`.

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

## Invite a person

People without a ctxt CLI get in through an invite link. On a machine where ctxt holds the owner's key, mint one:

```bash
ctxt share invite --name "Dana Partner"
```

```text
Single-use invite link for Dana Partner, valid until 2026-10-22 14:03:12:
https://dpkms.example.net/ui/invite#t=k2Yx…
```

The link goes to stdout and the note to stderr. It is built on the instance's `server.public_url`, works once, and expires after 7 days unless `--expires` says otherwise (`48h`, `2d`). The secret sits after `#`, so it never reaches a server log, a proxy or a Referer header. Hand it over on a channel you trust: whoever opens it first becomes Dana.

Opening the link creates a principal of kind `person` named Dana with the `reader` role, binds the link to it as an identity of method `link`, and signs the browser in. The page shows who they are signed in as. `ctxt principal list --kind person` lists them, and `ctxt principal show <id>` shows the identity the invite created.

With `--audience <name|id>` the person joins that audience as they accept, so the audience's grants apply from their first request; see [Share your network](sharing.md). Without it they see nothing until you add them to an audience.

A browser that already holds a live session cannot accept an invite. The page offers to sign out and continue, and the refusal does not spend the link. Opening the same link a second time, or after it expired, is refused as invalid.

## Sign in again with a link

A person without a ctxt key signs in again through a sign-in link the owner mints for them:

```bash
ctxt principal link prn_01J…
```

```text
Single-use sign-in link for prn_01J…, valid until 2026-10-15 14:13:12:
https://dpkms.example.net/ui/signin#t=Q8wn…
```

It prints a single-use link under `/ui/signin`, valid for 10 minutes and refused for a suspended or revoked principal. Opening it binds a fresh `link` identity to the principal and signs the browser in; earlier sessions are untouched.

To end what a link opened, remove the identity it created with `ctxt principal identity remove <principal> <identity>`. Every session that identity signed in ends with it. Revoking the principal ends everything it holds.

## Your own keys, identities and sessions

Every principal manages its own credentials with its key, under `write:self`, which every role holds. `ctxt auth whoami` says who the key acts for and what it holds. `ctxt auth key create|list|revoke` mint and end your keys, `ctxt auth identity list|remove` show and end the keys, links and proxy logins bound to you, and `ctxt auth session list|end` list and end your browser sessions.

```bash
ctxt auth whoami
ctxt auth key create --name "assistant" --scope read:objects --expires 7d
ctxt auth key list
ctxt auth session list --all
ctxt auth session end uis_3f2a…
```

A key never holds more than the key or session that created it. An agent's key narrowed to `read:objects` cannot mint a wider one, and a signed-in browser can create keys for its principal limited to what the session holds. Another principal's credential answers 403 `NOT_SELF`; the owner's admin key reaches those through `ctxt auth key create --principal <id>` and `ctxt principal`.

## What the browser session can do

The session acts as your key's principal, so entitlements and quotas apply as for the key. Its scopes are your key's, narrowed to what the web UI needs. It never holds a scope your key lacks, and never more than the web UI's set, whatever your key's role.

| Allowed, if your key has it | Refused (`403 INSUFFICIENT_SCOPE`) |
|---|---|
| Reading and searching objects, entities, jobs, inbox, feeds, aliases, saved searches, search history, reminders, import runs, and the step registry list (every role) | Pipelines, steps, registry changes, watches, the audit log, federation, the MCP mount |
| Retrying a job (`writer`, `admin`); deleting an object (`admin`); the Sharing page: audiences, share policies, grants, ceilings and invites (`admin`) | Every other write (capture, analyze, inbox triage, feed and alias changes, …) |
| The live event stream (every role); managing your own keys and sessions; signing out (every session) | Minting new sign-in links |

A session signed in with a reader's key only reads. An admin's session also holds `admin:shares`, so the owner manages sharing from the web UI's Sharing page. A reader's or writer's session never holds it. To see what your session holds, open `/api/v1/whoami` in the signed-in browser: it lists the session's `scopes` and its expiry.

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
- the moment the key or link identity that signed you in, or your principal, is revoked. Creating a new key does not revive it; sign in again.

When it ends, the next page load shows how to sign in again.

## Validation checklist

- The sign-in page named the principal you expected.
- The sidebar shows your principal and a green dot (live updates connected).
- `dpkms session list` on the server lists your session as `active`.

## Common failure modes

### This sign-in link is invalid, used or expired

The link was opened twice, or more than 60 seconds after `ctxt ui open`. Run `ctxt ui open` again.

### dpkms … refused to mint a sign-in link (exit 5)

The instance did not accept ctxt's key. Set `server.token` (or the token of the matching `server.urls` entry) to an API key created on that instance's host with `dpkms auth key create --maintenance-override` (dpkms stopped).

### Sign-in does not stick

The page says the browser did not keep the session cookie. The cookie is `Secure`, so browsers drop it over plain HTTP to any host but `localhost`. Reach the instance through a TLS-terminating reverse proxy. dpkms also logs a warning when a sign-in arrives over plain HTTP under a non-loopback name.

### Not available to a web UI session

The page or call needs a scope the session does not hold (`403 INSUFFICIENT_SCOPE`, naming the scope). Either it is outside the web UI's set (see the table above), or your key lacks it. Use the ctxt CLI or an API key with that scope.

### Too many sign-in attempts

Sign-in ceremonies are limited to ten attempts a minute per client address, then 429 `RATE_LIMITED` with a `Retry-After`. Wait that long and open the link again; a refused attempt spends nothing.

### This browser is already signed in

An invite or sign-in link opened in a browser that already holds a live session is refused (409 `SESSION_ACTIVE`). The page offers to sign out and continue; the link stays usable.

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
dpkms shutdown                      # the override needs the database to itself
dpkms session list --maintenance-override                    # active sessions, newest first
dpkms session list --all --maintenance-override              # ended ones too
dpkms session revoke uis_3f2a… --maintenance-override        # one browser
dpkms session revoke --principal ops --maintenance-override  # every browser of a principal
```

Sessions exist only on `protected` and `public` instances, whose store is access-gated: these host-side commands take the audited `--maintenance-override`, run as the database owner with dpkms stopped. See [Direct database access](../operations/direct-access.md).

The status column reads `active`, `expired` or `revoked` with its reason: `logout`, `revoked`, `identity revoked` (the key that signed the browser in was revoked) or `principal revoked`. Sessions are stored in the instance database (`ui_sessions`), with only SHA-256 hashes of the cookie secret and the login code and the id of the key that minted them. Ended sessions are pruned 30 days after they end.

Details of the cookie and CSRF rules: [Config and permissions](../reference/config-and-permissions.md#web-ui-sessions).
