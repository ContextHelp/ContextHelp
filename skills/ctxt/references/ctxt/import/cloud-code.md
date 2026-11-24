# `ctxt import` — Cloud Drives & GitHub (`gdrive`, `dropbox`, `onedrive`, `github`)

Import files from cloud storage APIs and repository metadata from GitHub. See [index.md](index.md) for shared execution profile.

## `ctxt import gdrive`

| Flag | Default | Purpose |
|---|---|---|
| `--access-token <tok>` | `""` | Google OAuth token (prefer `GDRIVE_ACCESS_TOKEN` env) |
| `--query <q>` | `""` | Raw Google Drive API `q` expression |
| `--folder-id <id>` | `[]` | Restrict to parent folder IDs (repeatable) |
| `--mime-type <mt>` | `[]` | Restrict to MIME types (repeatable) |
| `--include-trashed` | `false` | Include trashed files |
| `--since <date>` | `""` | Modified on/after RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max files (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` URL |
| `--drive-base-url <url>` | `""` | API base URL override (dev/tests only) |

## `ctxt import dropbox`

| Flag | Default | Purpose |
|---|---|---|
| `--access-token <tok>` | `""` | Dropbox OAuth token (prefer `DROPBOX_ACCESS_TOKEN` env) |
| `--path <dir>` | `""` | Folder path to import (default: root) |
| `--recursive` | `false` | Recurse into subfolders |
| `--cursor <cur>` | `""` | Resume incremental sync from prior Dropbox cursor |
| `--since <date>` | `""` | Modified on/after RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max files (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` URL |
| `--dropbox-api-url` / `--dropbox-content-url` | `""` | API overrides (dev/tests only) |

## `ctxt import onedrive`

| Flag | Default | Purpose |
|---|---|---|
| `--token <tok>` | `""` | Microsoft Graph token (prefer `ONEDRIVE_TOKEN` env) |
| `--drive-id <id>` | `[]` | Drive ID to import from root (repeatable) |
| `--drive-folder <ref>` | `[]` | Folder scope as `<drive-id>:/path` (repeatable) |
| `--item-ref <ref>` | `[]` | Specific item as `<drive-id>/<item-id>` (repeatable) |
| `--shared-with-me` | `false` | Include `/me/drive/sharedWithMe` |
| `--include-ext <ext>` | `[]` | Extension filter, e.g. `.pdf` (repeatable) |
| `--since <rfc3339>` | `""` | Modified since RFC3339 timestamp |
| `--max-items <n>` | `0` | Max files (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` URL |

## `ctxt import github`

| Flag | Default | Purpose |
|---|---|---|
| `-t`, `--token <tok>` | `""` | GitHub token (prefer `GITHUB_TOKEN` env) |
| `-u`, `--username <user>` | `""` | GitHub username (**required**) |
| `-l`, `--lists <csv>` | `starred` | Lists to import: `starred,watched,contributed` |
| `--pipeline <name>` | `text.long` | Pipeline override |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` URL |

## Agent Recipes

```bash
# Import GitHub starred and contributed repos
GITHUB_TOKEN="$TOK" ctxt import github --username alice --lists starred,contributed

# Import PDFs from a OneDrive folder
ONEDRIVE_TOKEN="$TOK" ctxt import onedrive --drive-folder "drv123:/Specs" --include-ext .pdf

# Incrementally sync a Dropbox folder
DROPBOX_ACCESS_TOKEN="$TOK" ctxt import dropbox --path /Research --recursive --cursor "$CUR"
```

## Next Steps

- Verify imports: [../list.md](../list.md) · Search: [../find.md](../find.md) · Back to router: [index.md](index.md)
