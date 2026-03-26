# CTXT Error Code Catalog

Machine-readable codes for all user-facing errors. Format: `CTXT-XXXX`.

Codes appear in:
- CLI JSON output: `{"error": {"code": "CTXT-XXXX", "message": "..."}}`
- HTTP API responses: `{"error": {"code": "CTXT-XXXX", "message": "...", "details": {}}}`

Source of truth: `internal/apierror/apierror.go`

---

## Ingestion (CTXT-1XXX)

| Code       | Name                   | Description                                          | Resolution                                   |
|------------|------------------------|------------------------------------------------------|----------------------------------------------|
| CTXT-1001  | IngestInvalidInput     | Input payload missing required fields or malformed   | Check `content`, `format`, required flags    |
| CTXT-1002  | IngestUnsupportedFmt   | File format not detectable or not supported          | Pass `--format jsonl\|csv\|tsv\|markdown`    |
| CTXT-1003  | IngestFileMissing      | File or directory path does not exist                | Verify path; ensure file is readable         |
| CTXT-1004  | IngestEnqueueFailed    | One or more items failed to enqueue                  | Check storage health; retry                  |
| CTXT-1005  | IngestNothingFound     | No items matched selection criteria                  | Broaden scope flags; check source            |
| CTXT-1006  | IngestTokenMissing     | Auth token required but not provided                 | Pass `--token` or set env var (e.g. `ONEDRIVE_TOKEN`) |

---

## Search (CTXT-2XXX)

| Code       | Name                   | Description                                          | Resolution                                   |
|------------|------------------------|------------------------------------------------------|----------------------------------------------|
| CTXT-2001  | SearchQueryRequired    | `q` parameter missing from search request            | Provide `--query` or `-q`                    |
| CTXT-2002  | SearchInvalidFilter    | Filter expression could not be parsed                | Check RSQL syntax; see `ctxt find --help`    |
| CTXT-2003  | SearchInternalFailed   | Search engine returned unexpected error              | Check storage; run `ctxt status`             |

---

## Config (CTXT-3XXX)

| Code       | Name                   | Description                                          | Resolution                                   |
|------------|------------------------|------------------------------------------------------|----------------------------------------------|
| CTXT-3001  | ConfigPathMissing      | Storage path not set in config                       | Run `ctxt config` and set `storage.path`     |
| CTXT-3002  | ConfigWriteFailed      | Unable to write updated config to disk               | Check file permissions on config dir         |
| CTXT-3003  | ConfigInvalid          | Config file contains invalid or unknown fields       | Validate YAML; see `ctxt config --help`      |

---

## Registry (CTXT-4XXX)

| Code       | Name                    | Description                                          | Resolution                                   |
|------------|-------------------------|------------------------------------------------------|----------------------------------------------|
| CTXT-4001  | RegistryNotFound        | Registry name not present in config                  | Run `ctxt registry add <name> <url>` first   |
| CTXT-4002  | RegistryURLRequired     | Registry URL missing from request                    | Provide `url` field                          |
| CTXT-4003  | RegistryFetchFailed     | HTTP fetch of registry manifest failed               | Check network; verify registry URL           |
| CTXT-4004  | RegistryBundleMissing   | Bundle path does not exist on disk                   | Provide a valid local bundle path            |
| CTXT-4005  | RegistryInvalidBundle   | Bundle directory missing required `manifest.json`    | Add manifest; see CONTRIBUTING.md            |

---

## Pipeline (CTXT-5XXX)

| Code       | Name                   | Description                                          | Resolution                                   |
|------------|------------------------|------------------------------------------------------|----------------------------------------------|
| CTXT-5001  | PipelineNameRequired   | Pipeline name missing from request                   | Provide `name` field                         |
| CTXT-5002  | PipelineStepsRequired  | Pipeline definition missing `steps`                  | Add at least one step to the pipeline        |
| CTXT-5003  | PipelineNotFound       | Named pipeline does not exist                        | Run `ctxt pipeline list` for available names |
| CTXT-5004  | PipelineRunFailed      | Pipeline execution returned an error                 | Inspect step logs; check provider config     |
| CTXT-5005  | PipelineInvalidBody    | Request body could not be parsed as JSON             | Validate JSON syntax                         |

---

## Storage (CTXT-6XXX)

| Code       | Name                   | Description                                          | Resolution                                   |
|------------|------------------------|------------------------------------------------------|----------------------------------------------|
| CTXT-6001  | StoragePathMissing     | Storage path not configured                          | Set `storage.path` in config                 |
| CTXT-6002  | StorageInitFailed      | Driver failed to open or migrate storage             | Check path permissions; disk space           |
| CTXT-6003  | StorageReadFailed      | Read operation returned unexpected error             | Check storage integrity; run `ctxt backup`   |
| CTXT-6004  | StorageWriteFailed     | Write operation failed                               | Check disk space and file permissions        |
| CTXT-6005  | StorageNotFound        | Requested record does not exist                      | Verify slug/ID; object may have been deleted |

---

## Generic (CTXT-9XXX)

| Code       | Name                   | Description                                          | Resolution                                   |
|------------|------------------------|------------------------------------------------------|----------------------------------------------|
| CTXT-9001  | NotFound               | Generic not-found for a named resource               | Verify the resource exists                   |
| CTXT-9002  | InvalidInput           | Generic bad input not covered by a specific code     | Check command arguments and flags            |
| CTXT-9003  | InternalError          | Unhandled internal server error                      | File a bug; include full error output        |
| CTXT-9004  | Unhealthy              | Health check failed; service not ready               | Run `ctxt status`; check storage and deps    |

---

## HTTP status mapping

| HTTP status     | Typical codes                                    |
|-----------------|--------------------------------------------------|
| 400 Bad Request | CTXT-1001, CTXT-2001, CTXT-2002, CTXT-5001–5002 |
| 404 Not Found   | CTXT-4001, CTXT-5003, CTXT-6005, CTXT-9001      |
| 500 Internal    | CTXT-2003, CTXT-5004, CTXT-6002–6004, CTXT-9003 |
| 503 Unavailable | CTXT-9004                                        |
