# Ingestion Adapters

Pluggable adapters that ingest external data into ctxt as
knowledge objects.

## Architecture

```
External source → Adapter → JSON objects → Runner → Dedup → Store
```

### Adapter interface

Every adapter implements:
```go
type Adapter interface {
    Name() string
    Fetch(ctx context.Context) ([]Object, error)
}
```

### Object contract

Each ingested object:
```go
type Object struct {
    ID       string            // unique per source
    Type     string            // contact, email, thread, ...
    Content  string            // human-readable body
    Tags     []string          // searchable labels
    Metadata map[string]string // structured fields
}
```

### Dedup

Runner deduplicates by `source_key = adapter_name:object_id`.
Same source + same ID = skip (already ingested).

## CLI

```bash
# Ingest from a named adapter
ctxt ingest --source cardamum
ctxt ingest --source cardamum --addressbook work

# Pipe JSON objects from stdin
echo '[{...}]' | ctxt ingest --stdin

# Watch mode (poll on interval)
ctxt ingest --source cardamum --watch --interval 5m
```

## Built-in Adapters

### cardamum (contacts)

Ingests contacts from cardamum vdir or CardDAV addressbooks.

```bash
ctxt ingest --source cardamum --addressbook default
ctxt ingest --source cardamum --account google
```

Source: `internal/ingest/cardamum/`

Transforms vCard fields:
- `FN` → content name
- `ORG` → tag `org:<value>`
- `TITLE` → tag `role:<value>`
- `EMAIL` → tag `email:<value>`
- `NOTE` → content notes
- `UID` → object ID

Stories: US-0320, US-0321

### himalaya (email)

Ingests email threads from himalaya as knowledge objects.

```bash
ctxt import himalaya --account ideacrafters
ctxt import himalaya --folder INBOX --since 2026-04-01
ctxt import himalaya --dry-run
```

Source: `internal/importer/himalaya/`

Features:
- Thread grouping via In-Reply-To + References (union-find)
- Contact scope tagging (`contact:<email>`)
- Reply detection
- Subject keyword extraction

Stories: US-email-ingest, US-contact-scope

### Contact scope enforcement

Email objects are tagged with `contact:<normalized-email>`.
When agents reply to a contact, ctxt filters knowledge to
only show objects within that contact's scope — enforcing
the communication policy.

## Writing a New Adapter

1. Create `internal/ingest/<name>/` package
2. Implement `ingest.Adapter` interface
3. Register in `internal/ingest/registry.go`
4. Add CLI flags in `cmd/ctxt/cmd/ingest.go`
5. Write unit tests + e2e test
6. Add user story in `docs/stories/ingestion/`

## Future Adapters

| Source | Type | Status |
|--------|------|--------|
| mxhook | Inbound webhooks | Planned |
| git log | Commit history | Planned |
| tlc | Task/track state | Planned |
| ash/usp | Session transcripts | Planned |
| ibr | Web captures | Planned |

## Related

- [adapter interface](../../internal/ingest/adapter.go)
- [runner](../../internal/ingest/runner.go)
- [stories](../stories/ingestion/)
