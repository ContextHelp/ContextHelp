# US-0008: Batch Import from File

**System Types:** ctxt, dpkms (self-hosted), dpkms cloud
**Personas:** [Maintainers](../../personas/maintainers.md), [Operations](../../personas/operations.md), [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker or operator, I want to import content in bulk from structured files (JSONL, CSV, TSV, OPML) or a directory of Markdown files so that I can migrate existing knowledge into my system without manual re-entry.

---

## Context

Organizations adopting ctxt rarely start from zero. They have bookmarks exported from browsers, notes in Markdown directories, data dumps from previous tools, and feed subscription lists in OPML format. A batch import facility bridges the gap between "empty knowledge base" and "productive system" by accepting common interchange formats and fanning each record out into its own ingestion job.

Batch import must handle real-world messiness: some records will fail validation, some will have missing fields, and files can be large. The system uses a partial-success model where individual record failures do not halt the entire batch. A progress tracker exposes total, completed, and failed counters so operators can monitor long-running imports. Dry-run mode lets users validate a file before committing to ingestion, catching schema errors and malformed records upfront.

The fan-out pattern keeps batch import consistent with single-item ingestion: each record becomes a standard ingestion job, goes through the same pipelines, and produces the same KnowledgeObject structure. A parent batch object tracks the overall import and links to all child objects via edges, providing a single point of reference for "everything that came in from this import."

---

## Acceptance Criteria

- [ ] User can import records from JSONL, CSV, TSV, OPML, or a Markdown directory
- [ ] Each record in the batch is enqueued as a separate ingestion job (fan-out)
- [ ] Batch returns a `batch_id` immediately; processing is fully async
- [ ] User can query import progress via `ctxt import status <batch_id>`
- [ ] Per-record errors do not halt the batch; errors are collected in a batch report
- [ ] CSV/TSV column mapping is configurable (which column maps to content, type, tags, etc.)
- [ ] JSONL records require at minimum `{"content": "..."}`, with optional type, tags, source, metadata
- [ ] OPML import creates feed subscriptions (delegates to US-0007 feed subsystem)
- [ ] Dry-run mode validates the file and reports errors without ingesting any records
- [ ] Batch respects a configurable max concurrent import limit (default: 10 parallel jobs)
- [ ] System rejects batches exceeding the configured max batch size (default: 10,000 records)

---

## Implementation Notes

### CLI Interface
```bash
# Import from JSONL
ctxt import --file exports.jsonl
# → {
# →   "batch_id": "b-abc123",
# →   "format": "jsonl",
# →   "total_records": 342,
# →   "status": "processing",
# →   "created_at": "2026-02-18T10:00:00Z"
# → }

# Import from CSV with explicit format
ctxt import --file bookmarks.csv --format csv
# → {
# →   "batch_id": "b-def456",
# →   "format": "csv",
# →   "total_records": 1205,
# →   "status": "processing"
# → }

# Import from CSV with custom column mapping
ctxt import --file data.csv --format csv \
  --map-content body --map-type category --map-tags labels --map-source url

# Import a directory of Markdown files (recursive)
ctxt import --dir ~/notes/ --format markdown
# → {
# →   "batch_id": "b-ghi789",
# →   "format": "markdown",
# →   "total_records": 87,
# →   "status": "processing"
# → }

# Import OPML (creates feed subscriptions, not knowledge objects)
ctxt import --file feeds.opml --format opml
# → {
# →   "batch_id": "b-jkl012",
# →   "format": "opml",
# →   "total_records": 24,
# →   "status": "processing",
# →   "note": "OPML import creates feed subscriptions (see ctxt feed list)"
# → }

# Check import progress
ctxt import status b-abc123
# → BATCH ID   FORMAT  TOTAL  COMPLETED  FAILED  STATUS
# → b-abc123   jsonl   342    318        3       processing
# →
# → Failed records:
# →   line 42: missing required field "content"
# →   line 119: content exceeds max length (1MB)
# →   line 287: invalid JSON on line

# Dry-run mode (validate without ingesting)
ctxt import --file data.jsonl --dry-run
# → Dry run complete:
# →   Total records: 342
# →   Valid: 339
# →   Invalid: 3
# →     line 42: missing required field "content"
# →     line 119: content exceeds max length (1MB)
# →     line 287: invalid JSON on line
# → No records were ingested.
```

### REST API
```
POST /import
Content-Type: multipart/form-data

------boundary
Content-Disposition: form-data; name="file"; filename="exports.jsonl"
Content-Type: application/x-jsonlines

{"content": "First insight", "type": "text", "tags": ["architecture"]}
{"content": "Second insight", "source": "https://example.com/article"}
------boundary
Content-Disposition: form-data; name="format"

jsonl
------boundary
Content-Disposition: form-data; name="dry_run"

false
------boundary--

--> 202 Accepted
{
  "batch_id": "b-abc123",
  "format": "jsonl",
  "total_records": 2,
  "status": "processing",
  "created_at": "2026-02-18T10:00:00Z"
}
```

```
POST /import
Content-Type: multipart/form-data

------boundary
Content-Disposition: form-data; name="file"; filename="bookmarks.csv"
Content-Type: text/csv

url,title,tags
https://example.com/a,Article A,"go,architecture"
https://example.com/b,Article B,"rust,performance"
------boundary
Content-Disposition: form-data; name="format"

csv
------boundary
Content-Disposition: form-data; name="column_mapping"

{"content": "url", "tags": "tags", "metadata.title": "title"}
------boundary--

--> 202 Accepted
{
  "batch_id": "b-def456",
  "format": "csv",
  "total_records": 2,
  "status": "processing"
}
```

```
GET /import/b-abc123

--> 200 OK
{
  "batch_id": "b-abc123",
  "format": "jsonl",
  "total_records": 342,
  "completed": 318,
  "failed": 3,
  "status": "processing",
  "errors": [
    {"line": 42, "error": "missing required field \"content\""},
    {"line": 119, "error": "content exceeds max length (1MB)"},
    {"line": 287, "error": "invalid JSON on line"}
  ],
  "created_at": "2026-02-18T10:00:00Z",
  "updated_at": "2026-02-18T10:02:15Z"
}
```

### Pipeline Steps

Each import format has its own pipeline for parsing, but all converge on the same fan-out pattern:

- **batch.jsonl** --> FileReader --> JSONLParser --> RecordValidator --> BatchEnqueuer
- **batch.csv** --> FileReader --> CSVParser --> ColumnMapper --> RecordValidator --> BatchEnqueuer
- **batch.tsv** --> FileReader --> TSVParser --> ColumnMapper --> RecordValidator --> BatchEnqueuer
- **batch.opml** --> FileReader --> OPMLParser --> FeedSubscriber (delegates to US-0007)
- **batch.directory** --> DirectoryScanner --> MarkdownParser --> RecordValidator --> BatchEnqueuer

```go
// RecordValidator checks that each parsed record meets the minimum schema.
type RecordValidator struct{}

func (s *RecordValidator) Name() string { return "record_validator" }

func (s *RecordValidator) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	records := draft.Metadata["parsed_records"].([]ImportRecord)
	var valid []ImportRecord
	var errors []RecordError

	for i, rec := range records {
		if rec.Content == "" {
			errors = append(errors, RecordError{
				Line:  i + 1,
				Error: "missing required field \"content\"",
			})
			continue
		}
		if len(rec.Content) > 1<<20 { // 1MB limit
			errors = append(errors, RecordError{
				Line:  i + 1,
				Error: "content exceeds max length (1MB)",
			})
			continue
		}
		valid = append(valid, rec)
	}

	draft.Metadata["valid_records"] = valid
	draft.Metadata["validation_errors"] = errors
	return draft, nil
}
```

```go
// BatchEnqueuer fans out valid records into individual ingestion jobs.
type BatchEnqueuer struct {
	jobStore   storage.JobStore
	maxWorkers int
}

func (s *BatchEnqueuer) Name() string { return "batch_enqueuer" }

func (s *BatchEnqueuer) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	records := draft.Metadata["valid_records"].([]ImportRecord)
	batchID := draft.ID

	sem := make(chan struct{}, s.maxWorkers)
	for _, rec := range records {
		sem <- struct{}{}
		go func(r ImportRecord) {
			defer func() { <-sem }()

			pipeline := selectPipelineForRecord(r)
			job := storage.Job{
				ID:       generateID("j"),
				Type:     "ingestion",
				Status:   storage.JobPending,
				Payload:  r.Content,
				Pipeline: pipeline,
				Source:   r.Source,
			}
			_ = s.jobStore.CreateJob(ctx, &job)

			// Link child job to parent batch via edge
			_ = s.jobStore.CreateEdge(ctx, &storage.Edge{
				FromType: "batch",
				FromID:   batchID,
				ToType:   "job",
				ToID:     job.ID,
				EdgeType: "batch_contains",
			})
		}(rec)
	}

	draft.Metadata["enqueued_count"] = len(records)
	return draft, nil
}
```

### Backend Processing

1. **File upload:** User provides a file (or directory path) and optional format hint. The system detects the format from file extension if not specified.
2. **Create batch object:** A parent KnowledgeObject is created with `Type="batch"` and metadata including format, filename, and total record count. A batch job is created and the `batch_id` is returned immediately.
3. **Parse:** The appropriate parser (JSONLParser, CSVParser, TSVParser, OPMLParser, or MarkdownParser) reads the file and produces a list of `ImportRecord` structs.
4. **Validate:** RecordValidator checks each record against the minimum schema. Invalid records are collected as errors but do not halt the batch.
5. **Dry-run check:** If dry-run mode is enabled, the batch report (valid count, error count, error details) is returned without enqueuing any jobs. The batch status is set to `dry_run_complete`.
6. **Fan-out:** BatchEnqueuer creates one ingestion job per valid record. Each job's pipeline is selected based on the record's content: `text.short` for short text, `text.long` for long text, `url.article` for URLs. Jobs are enqueued respecting the concurrent import limit (default: 10 parallel).
7. **Edge linking:** Each child KnowledgeObject is linked to the parent batch object via a `batch_contains` edge, enabling queries like "show me everything from import b-abc123."
8. **Progress tracking:** The batch object's metadata is updated as child jobs complete or fail. Counters for `total`, `completed`, and `failed` are maintained atomically.
9. **Completion:** When all child jobs have finished (completed or failed), the batch status transitions to `completed` (all succeeded) or `partial` (some failed). The final batch report is persisted.
10. **OPML special case:** For OPML imports, the OPMLParser extracts feed URLs and delegates each to the feed subscription subsystem (US-0007), creating feed subscriptions rather than knowledge objects.

### JSONL Schema
```json
// Minimum valid record:
{"content": "Some text insight or note"}

// Fully specified record:
{
  "content": "Detailed article about distributed systems",
  "type": "text",
  "tags": ["distributed-systems", "architecture"],
  "source": "https://example.com/article",
  "metadata": {
    "author": "Jane Doe",
    "published_at": "2026-01-15T09:00:00Z",
    "project": "infrastructure"
  }
}
```

### CSV Column Mapping
```yaml
# Default column mapping (auto-detected if headers match)
columnMapping:
  content: content        # Required: which column holds the main content
  type: type              # Optional: content type
  tags: tags              # Optional: comma-separated tags
  source: source          # Optional: source URL or reference
  metadata.*: "*"         # Optional: remaining columns become metadata keys

# Custom mapping via CLI flag --map-<field> <column>
# ctxt import --file data.csv --format csv --map-content body --map-tags labels
```

### Configuration
```yaml
# In configuration.yaml
import:
  maxBatchSize: 10000          # Maximum records per batch (rejects larger files)
  maxConcurrentJobs: 10        # Parallel ingestion jobs per batch
  maxRecordSize: 1MB           # Maximum size of a single record's content
  tempDir: /tmp/ctxt-import    # Temporary storage for uploaded files
  retainBatchReport: true      # Keep batch reports after completion
  reportRetention: 30d         # How long to keep batch reports

  # Format-specific settings
  csv:
    delimiter: ","             # Default CSV delimiter
    hasHeader: true            # Expect header row
    encoding: utf-8            # File encoding
  tsv:
    delimiter: "\t"
    hasHeader: true
    encoding: utf-8
  markdown:
    extensions:                # File extensions to scan
      - .md
      - .markdown
    recursive: true            # Scan subdirectories
    frontMatter: true          # Parse YAML front matter as metadata
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt import --file data.jsonl` returns batch_id and begins processing
- [ ] CLI: `ctxt import --file data.csv --format csv` correctly parses CSV with default column mapping
- [ ] CLI: `ctxt import --file data.csv --map-content body --map-tags labels` applies custom column mapping
- [ ] CLI: `ctxt import --file data.tsv --format tsv` correctly parses TSV records
- [ ] CLI: `ctxt import --dir ~/notes/ --format markdown` recursively scans and imports Markdown files
- [ ] CLI: `ctxt import --file feeds.opml --format opml` creates feed subscriptions (not knowledge objects)
- [ ] CLI: `ctxt import status <batch_id>` shows correct total/completed/failed counters
- [ ] CLI: `ctxt import --file data.jsonl --dry-run` validates without ingesting; reports errors
- [ ] Fanout: Each valid record creates a separate ingestion job with correct pipeline
- [ ] Partial success: Invalid records are skipped; valid records are still ingested
- [ ] Batch report: Failed records include line number and descriptive error message
- [ ] Edges: Child KnowledgeObjects are linked to parent batch object via `batch_contains` edges
- [ ] Size limit: Batch exceeding `maxBatchSize` is rejected with a clear error message
- [ ] Concurrency: No more than `maxConcurrentJobs` run simultaneously from one batch
- [ ] REST API: `POST /import` with multipart file upload returns 202 with batch_id
- [ ] REST API: `GET /import/{batch_id}` returns progress with total/completed/failed/errors
- [ ] JSONL: Record missing "content" field is rejected with validation error
- [ ] CSV: File with no header row handled when `hasHeader: false` is configured

---

## Related Stories

- [US-0001: Text Capture with Minimal Friction](./US-0001-text-capture-minimal-friction.md) -- Each imported record reuses the standard ingestion pipeline
- [US-0007: Feed Ingestion and Sync](./US-0007-feed-ingestion-and-sync.md) -- OPML import delegates to the feed subscription subsystem
- [US-0009: Extract Entities and Mentions](../enrichment/US-0009-extract-entities-and-mentions.md) -- Imported records are enriched with entity extraction
- [US-0015: Batch Enrichment with Progress](../enrichment/US-0015-batch-enrichment-with-progress.md) -- Progress tracking pattern shared with batch enrichment
- [US-0034: Export and Backup All Knowledge](../operations/US-0034-export-and-backup-all-knowledge.md) -- Export produces JSONL that can be re-imported
