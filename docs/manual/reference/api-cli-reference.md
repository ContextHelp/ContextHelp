# API and CLI Reference Map

This page maps practical operations to current CLI commands and API endpoints.

## Command map (current runtime)

### Ingestion and jobs

```bash
ctxt analyze <content> [--type text|url|image|audio|video|feed|auto] [--file <path>] [--wait]
ctxt import chrome --file <bookmarks.html> [--dry-run] [--max-items N] [--pipeline <name>] [--server <url>]
ctxt import onedrive --drive-id <id>|--drive-folder <id>:/path|--item-ref <id>/<item-id>|--shared-with-me [--token <token>] [--since <RFC3339>] [--include-ext .pdf] [--max-items N] [--dry-run]
ctxt import pinboard [--token <token>] [--file <pinboard.json>] [--since <RFC3339|YYYY-MM-DD>] [--tag <tag>] [--max-items N] [--dry-run]
ctxt import raindrop --collection-id <id>|--all [--token <token>] [--since <RFC3339|YYYY-MM-DD>] [--tag <tag>] [--query <text>] [--max-items N] [--dry-run]
# Story-target importer surface (US-0300 to US-0317):
# ctxt import <source> [source-specific flags]
ctxt job list [--state pending|running|completed|failed|cancelled] [--limit N] [--offset N]
ctxt job status <job_id>
ctxt job retry <job_id>
ctxt job cancel <job_id> --confirm-token=<token>
# On the dpkms host, against its local store:
dpkms job list|status|log|retry|cancel
```

### Retrieval and inspection

```bash
ctxt find "<natural-language query>" [--limit N]
ctxt find "<query>" --explain                     # per-signal score breakdown
ctxt find "<query>" --graph [--no-browser]        # interactive search graph (see workflows/search-graph.md)
ctxt find "<query>" --graph --format json|yaml    # search graph as JGF
ctxt find "<query>" --graph -o <file>.html|.json|.yaml|.graphml|.gexf
ctxt list [--type <type>] [--tagged <tag>] [--after <date>] [--q "<query>"]
ctxt show <object_id> [--raw] [--format json|yaml]
ctxt export <object_id> [--format yaml]
```

### Composition

```bash
ctxt compose brief|plan|summary|draft [--tagged <tag>] [--mention @entity.slug] [--since <date>] [--until <date>] [--output-file <file>]
```

### Profiles and config

```bash
ctxt profile list
ctxt profile view <name>
ctxt profile create <name> --config <file>
ctxt profile set <name>
ctxt profile unset <name>
ctxt profile rm <name> --confirm-token=<sha>
ctxt config show|path|validate|edit
```

### Registry operations

```bash
ctxt registry list
ctxt registry info <name>
ctxt registry add <name> <url>
ctxt registry sync <name>
ctxt registry pending [--registry <name>]
ctxt registry approve <id>... | --all --registry <name>   # --confirm-token=<sha>
ctxt registry reject <id>...
ctxt registry delete <name> --confirm-token=<sha>
```

### Pipeline and infrastructure (`dpkms`)

```bash
dpkms serve [--port 8080] [--grpc-port 9090] [--cookie-bridge-port 9377] [--workers N] [--public]
dpkms pipeline list|show|create|archive|unarchive|remove
dpkms pipeline enqueue [--pipeline <name>] [--type <type>] [--wait] <content>
dpkms pipeline step list|install|uninstall
dpkms pipeline step registry list|add|update|autoupdate
dpkms housekeeping vacuum|reindex|compact|prune
dpkms session list [--all] [--principal <p>] | revoke <id> | revoke --principal <p>
```

### Web UI

```bash
ctxt ui open [--server <url> | --instance <name>] [--no-browser]
```

### Identity, principals and sharing

```bash
ctxt auth whoami
ctxt auth key create [--name <label>] [--expires 30d|2026-12-31] [--scope <scope>]... [--principal <id>]
ctxt auth key list [--principal <id>]
ctxt auth key revoke <key-id> [--principal <id>]
ctxt auth identity list | remove <identity-id>
ctxt auth session list [--all] | end <session-id>
# admin:principals (the owner's key):
ctxt principal list [--kind person|device|peer] [--status active|suspended|revoked]
ctxt principal show|suspend|link <id>
ctxt principal create --kind person|device|peer --name <name> [--role admin|writer|reader]...
ctxt principal revoke <id> --confirm-token=<sha>
ctxt principal identity add <id> --method proxy|oidc --subject <subject> [--issuer <issuer>] [--name <label>]
ctxt principal identity list <id> | remove <id> <identity-id>
# admin:shares (the owner's key):
ctxt share invite --name <display name> [--expires 7d] [--audience <audience>]
ctxt share audience create <name> | add <audience> <principal-id> [--expires 90d] | remove <audience> <principal-id> | list
ctxt share policy create [file] | edit <policy> [file] | show <policy> | list     # YAML from a file or stdin
ctxt share template list | install colleagues-network|partners-network
ctxt share grant <audience> <policy> [--expires 30d]
ctxt share revoke <grant-id>
ctxt share ceiling <principal-id> --deny-classes personal,financial
```

## REST endpoint map (from current spec)

Core routes:

- `GET /health`
- `POST /analyze`
- `POST /api/v1/pipelines/enqueue`
- `GET /jobs`
- `GET /jobs/{id}`
- `POST /jobs/{id}/retry`
- `POST /jobs/{id}/cancel`
- `GET /objects`
- `GET /objects/{id}`
- `PATCH /objects/{id}`
- `DELETE /objects/{id}`
- `GET /profiles`
- `GET /profiles/{name}`
- `POST /compose`
- `GET /registries`
- `GET /registries/{id}`
- `GET /entities`
- `GET /entities/{slug}`
- `GET /entities/{slug}/related`

Web UI sign-in routes (see [Sign in to the web UI](../workflows/web-ui-sign-in.md)):

- `POST /api/v1/ui/login-codes` (`read:ui`, API token only): single-use login code, 60 s
- `POST /ui/auth/session`: login code for the session cookie (same-origin, `X-Ctxt-CSRF: 1`)
- `GET /api/v1/whoami`: who the caller is (`via`: `token`, `session` or `none`), its scopes and, for a session, the session
- `DELETE /api/v1/ui/session` (`signout:ui`, browser sessions only): sign the browser out

Invite and sign-in link routes (same guide):

- `POST /api/v1/share/invites` (`admin:shares`): `{display_name, expires_in}` → `{url, expires_at, expires_in}`, the link under `/ui/invite`, single use, 7 days by default
- `POST /api/v1/principals/{id}/links` (`admin:principals`): a 10-minute sign-in link under `/ui/signin` for an active principal
- `POST /ui/invite/session`, `POST /ui/signin/session`: ticket for the session cookie (same-origin, `X-Ctxt-CSRF: 1`; rate limited per client address; refused with a live session)

Principal registry routes (`admin:principals`): `GET`, `POST /api/v1/principals`; `GET /api/v1/principals/{id}`; `POST .../suspend`, `.../revoke`; `GET`, `POST .../identities`, `DELETE .../identities/{identity}`; `GET`, `POST .../keys`, `DELETE .../keys/{key}`. Own credentials (`write:self`): `GET`, `POST /api/v1/self/keys`, `DELETE /api/v1/self/keys/{key}`, `PUT /api/v1/self/keys/{key}/policy`; `GET /api/v1/self/identities`, `DELETE /api/v1/self/identities/{identity}`; `GET /api/v1/self/sessions`, `DELETE /api/v1/self/sessions/{session}`.

Sharing routes (`admin:shares`; see [Share your network](../workflows/sharing.md)): `GET`, `POST /api/v1/share/audiences`, `GET /api/v1/share/audiences/{audience}`, `PUT`, `DELETE .../members/{principal}`; `GET`, `POST /api/v1/share/policies`, `GET`, `PUT /api/v1/share/policies/{policy}`; `GET /api/v1/share/templates`, `POST /api/v1/share/templates/{name}`; `GET`, `POST /api/v1/share/grants`, `DELETE /api/v1/share/grants/{grant}`; `GET`, `PUT /api/v1/principals/{id}/ceiling`; `PUT /api/v1/share/keys/{key}/policy`. Audiences and policies may be named by id or name. A policy document travels as the `document` field of a JSON body (YAML or JSON text), validated on write. An edit bumps the version and never renames. A template installs once. `GET /share/grants?principal=` lists the grants in force for one principal. A non-owner principal with a grant in force sees `shared: {owner, policies}` in `GET /whoami`.

Importer routes (story-target contract):

- `GET /api/v1/importers`
- `POST /api/v1/importers/{source}/run`
- `GET /api/v1/importers/runs/{run_id}`

Reference: [`docs/api/api-rest.md`](https://github.com/ContextHelp/ContextHelp/blob/next/docs/api/api-rest.md)

## gRPC service map (from current spec)

- `AnalyzeService`
- `JobService`
- `ObjectService`
- `ProfileService`
- `CompositionService`
- `RegistryService`
- `EntityService`
- `SuggestionService` (optional)

Reference: [`docs/api/api-grpc.md`](https://github.com/ContextHelp/ContextHelp/blob/next/docs/api/api-grpc.md)

## Runtime-vs-story note

Some stories reference planned agent-native contracts (for example, `/query-schema` in `US-0037`). Use this page for currently documented command/API surfaces and treat story-target contracts as forward-looking unless implemented in runtime.

## Related stories

- Ingestion/jobs: `US-0001` to `US-0008`, `US-0300` to `US-0317`, `US-0039`, `US-0106`, `US-0113`
- Retrieval: `US-0016` to `US-0021`, `US-0051` to `US-0055`, `US-0061`
- Composition: `US-0022` to `US-0026`, `US-0056` to `US-0060`, `US-0040`
