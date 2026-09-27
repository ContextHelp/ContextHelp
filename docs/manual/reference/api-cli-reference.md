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
ctxt job list [--state pending|running|completed|failed|cancelled] [--limit N]
ctxt job status <job_id>
ctxt job log <job_id>
ctxt job retry <job_id>
ctxt job cancel <job_id>
```

### Retrieval and inspection

```bash
ctxt find "<natural-language query>" [--limit N]
ctxt find "<query>" --explain                     # per-signal score breakdown
ctxt find "<query>" --graph [--no-browser]        # interactive search graph (see workflows/search-graph.md)
ctxt find "<query>" --graph --format json|yaml    # search graph as JGF
ctxt find "<query>" --graph -o <file>.html|.json|.yaml|.graphml|.gexf
ctxt list [--type <type>] [--tag a,b] [--after <ISO>] [--q "<query>"]
ctxt open <object_id> [--raw] [--output json|yaml|text]
```

### Composition

```bash
ctxt make brief|plan|summary|draft [--tag a,b] [--mention @entity.slug] [--since <ISO>] [--output-file <file>]
```

### Profiles and config

```bash
ctxt profile list
ctxt profile view <name>
ctxt profile create <name> --config <file>
ctxt profile set <name>
ctxt profile unset <name>
ctxt profile rm <name> --confirm=yes
ctxt config show|path|validate|edit
```

### Registry operations

```bash
ctxt registry list
ctxt registry info <name>
ctxt registry add <name> <url>
ctxt registry sync <name>
ctxt registry remove <name>
```

### Pipeline and infrastructure (`dpkms`)

```bash
dpkms serve [--port 8080] [--grpc-port 9090] [--workers N] [--public]
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

## REST endpoint map (from current spec)

Core routes:

- `GET /health`
- `POST /analyze`
- `POST /api/v1/pipelines/enqueue`
- `GET /jobs`
- `GET /jobs/{id}`
- `POST /jobs/{id}/retry`
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

- `POST /api/v1/ui/login-codes` (API token only): single-use login code, 60 s
- `POST /ui/auth/session`: login code for the session cookie (same-origin, `X-Ctxt-CSRF: 1`)
- `GET /api/v1/ui/session`: who the caller is (`via`: `token` or `session`)
- `DELETE /api/v1/ui/session`: sign the browser out

Importer routes (story-target contract):

- `GET /api/v1/importers`
- `POST /api/v1/importers/{source}/run`
- `GET /api/v1/importers/runs/{run_id}`

Reference: [`../../api/api-rest.md`](../../api/api-rest.md)

## gRPC service map (from current spec)

- `AnalyzeService`
- `JobService`
- `ObjectService`
- `ProfileService`
- `CompositionService`
- `RegistryService`
- `EntityService`
- `SuggestionService` (optional)

Reference: [`../../api/api-grpc.md`](../../api/api-grpc.md)

## Runtime-vs-story note

Some stories reference planned agent-native contracts (for example, `/query-schema` in `US-0037`). Use this page for currently documented command/API surfaces and treat story-target contracts as forward-looking unless implemented in runtime.

## Related stories

- Ingestion/jobs: `US-0001` to `US-0008`, `US-0300` to `US-0317`, `US-0039`, `US-0106`, `US-0113`
- Retrieval: `US-0016` to `US-0021`, `US-0051` to `US-0055`, `US-0061`
- Composition: `US-0022` to `US-0026`, `US-0056` to `US-0060`, `US-0040`
