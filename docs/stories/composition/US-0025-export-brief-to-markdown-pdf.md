# US-0025: Export Brief To Markdown PDF

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to export a generated brief to markdown, PDF, HTML, or plain text so I can share it with people who do not have access to the knowledge system.

---

## Context

Briefs generated in ctxt are useful beyond the system itself. Stakeholders may need a PDF for a meeting, a markdown file for a wiki, HTML for an intranet page, or plain text for an email. The export command renders an existing brief into the requested format, preserving all sections, provenance, and metadata.

---

## Acceptance Criteria

- [ ] User can export an existing brief by ID (`--brief <id>`) to a chosen format (`--format <format>`)
- [ ] Supported formats: `markdown`, `pdf`, `html`, `plain`
- [ ] `--output <path>` writes the export to a local file; omitting it writes to stdout
- [ ] `--format` flag is a client-side flag only when not accompanied by server-side format conversion; `--brief <id>` is sent to server as `brief_id`
- [ ] Markdown export preserves all sections and headings from the brief
- [ ] PDF export contains all section titles and body text (`%PDF` magic bytes)
- [ ] HTML export contains all sections wrapped in semantic tags (`<html` present)
- [ ] Plain text export contains no markdown syntax characters
- [ ] Provenance table/block present in all export formats
- [ ] Metadata block (generated date, template, object count) present in all formats
- [ ] Non-existent brief ID returns 404 with descriptive error
- [ ] Unsupported format value returns 400 with list of valid formats
- [ ] Write failure to `--output` path reports filesystem error clearly

---

## Implementation Notes

### CLI Interface

```bash
# Export to stdout (markdown default)
ctxt export brief b-xyz789 --format markdown

# Export to PDF file
ctxt export brief b-xyz789 --format pdf --output brief.pdf

# Export to HTML
ctxt export brief b-xyz789 --format html --output brief.html

# Export to plain text
ctxt export brief b-xyz789 --format plain
```

### REST API

```
POST /compositions/brief/{id}/export
Content-Type: application/json

{
  "format": "pdf"
}

→ 200 OK  (export record stored; content returned or streamed)
→ 400 Bad Request  (unsupported format)
→ 404 Not Found    (non-existent brief ID)

GET /compositions/brief/{id}/exports
→ 200 OK
[
  {"format": "pdf", "created_at": "2025-01-18T10:30:45Z"},
  {"format": "html", "created_at": "2025-01-18T10:35:00Z"}
]
```

---

## E2E Test Checklist

### CLI → Server payload propagation
- [ ] `--format markdown` sends `format: "markdown"` in POST body (or export request)
- [ ] `--format pdf` sends `format: "pdf"` in POST/export request body
- [ ] `--format html` sends `format: "html"` in POST/export request body
- [ ] `--format plain` sends `format: "plain"` in POST/export request body
- [ ] `--brief <id>` sends `brief_id` in export request
- [ ] `--output <path>` is a client-side flag only; not sent to server (local write)

### Server-side receipt and storage
- [ ] POST /compositions/brief/{id}/export stores export record with `format` and `created_at`
- [ ] Repeated export requests for same brief+format return consistent content
- [ ] Export record is retrievable: GET /compositions/brief/{id}/exports lists formats exported

### CLI output validation
- [ ] `ctxt export brief <id> --format markdown` exits 0 and writes valid markdown
- [ ] `ctxt export brief <id> --format pdf` exits 0 and output file starts with `%PDF`
- [ ] `ctxt export brief <id> --format html` exits 0 and output contains `<html` tag
- [ ] `ctxt export brief <id> --format plain` exits 0 and output contains no markdown syntax chars
- [ ] Without `--output`, output written to stdout (pipeable)
- [ ] With `--output <path>`, file written to specified path

### Content correctness
- [ ] Markdown export preserves all sections and headings from brief
- [ ] PDF export contains all section titles and body text (non-empty pages)
- [ ] HTML export contains all sections wrapped in semantic tags
- [ ] Provenance table/block present in all export formats
- [ ] Metadata block (generated date, template, object count) present in all formats

### Error handling
- [ ] Non-existent brief ID returns 404 with descriptive error
- [ ] Unsupported format value returns 400 with list of valid formats
- [ ] Write failure to `--output` path reports filesystem error clearly

---

## Related Stories

- [US-0022: generate-brief-from-objects](./US-0022-generate-brief-from-objects.md) — Brief to export
- [US-0026: share-composition-with-team](./US-0026-share-composition-with-team.md) — Share exported brief
- [US-0060: compose-with-custom-template](./US-0060-compose-with-custom-template.md) — Custom template affects export content
