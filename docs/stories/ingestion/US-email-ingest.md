---
status: paper
---

# US-email-ingest: Import Email Threads from Himalaya

**System Types:** ctxt
**Personas:** [Operations](../../personas/operations.md)

---

## User Goal

As an agent, I ingest email threads from himalaya so I can search
past communications as knowledge objects in ctxt.

---

## Context

Agents need access to email history for context-aware replies,
decision recall, and thread continuity. Himalaya provides a CLI
interface to IMAP mailboxes. This adapter bridges himalaya's
envelope/message output to ctxt's ingestion contract (T-0381):
JSON array of `{id, type, content, tags[], metadata{}}` objects.

Thread grouping uses In-Reply-To and References headers to chain
messages. Each object carries contact scope tags for enforcing
reply boundaries (see US-contact-scope).

---

## Acceptance Criteria

- [ ] `ctxt import himalaya` lists envelopes via himalaya CLI
      and reads message bodies + headers.
- [ ] Messages are grouped into threads using In-Reply-To /
      References header chains.
- [ ] Output is a JSON array conforming to the T-0381 adapter
      contract: `{id, type, content, tags[], metadata{}}`.
- [ ] Each object has type "email" and a stable dedup ID:
      `himalaya:<account>:<envelope_id>`.
- [ ] Tags include: source, type, from address, to address,
      thread ID, subject keywords, and contact scope.
- [ ] Metadata includes: date, envelope_id, message_id,
      in_reply_to, thread_id, source, account, subject.
- [ ] `--dry-run` lists envelopes without reading bodies.
- [ ] `--folder`, `--account`, `--since`, `--max-items` flags
      filter the envelope listing.
- [ ] Failed message reads are skipped with a warning; they do
      not abort the run.

---

## Implementation Notes

### Parser (internal/importer/himalaya/)

- `ParseEnvelopes(data)` — unmarshal himalaya envelope list JSON
- `ParseHeaders(raw)` — extract Message-Id, In-Reply-To, References
- `ParseBody(raw)` — split headers from body
- `BuildMessage(env, raw)` — combine envelope + raw into Message
- `GroupThreads(msgs)` — union-find thread grouping

### Adapter (internal/importer/himalaya/)

- `ToCtxtObject(msg, source)` — Message -> CtxtObject
- `RenderContent(msg)` — plain-text for indexing
- Tags: `source:himalaya`, `type:email`, `from:<addr>`,
  `to:<addr>`, `thread:<id>`, `subject:<keyword>`,
  `contact:<normalized-addr>`

### CLI (cmd/ctxt/cmd/import_himalaya.go)

- Subcommand under `ctxt import himalaya`
- Uses himalaya binary (auto-detected or `--himalaya-bin`)
- Outputs ctxt objects JSON to stdout; progress to stderr
- Designed to pipe into `ctxt ingest --source himalaya`

---

## E2E Test Checklist

- [ ] Mock himalaya runner -> parse envelopes -> read messages ->
      thread grouping -> JSON output matches contract schema.
- [ ] Thread grouping: In-Reply-To chain produces shared thread_id.
- [ ] Dry-run: outputs envelope list, no message reads.
- [ ] Partial failure: skip broken reads, still output valid objects.
- [ ] Contact scope tag is normalized lowercase.

---

## Related Stories

- [US-contact-scope](./US-contact-scope.md) — contact scope
- [US-0300](./US-0300-importer-extension-interface.md) — importer
  interface
- [US-0309](./US-0309-import-slack.md) — similar adapter for Slack

---

## Personas

- [Operations](../../personas/operations.md)

---

## E2E Tests

- planned: `test/integration/us_email_ingest_test.go::TestEmailIngest_IMAPFetch`
- planned: `test/integration/us_email_ingest_test.go::TestEmailIngest_ThreadGrouping`
- planned: `test/integration/us_email_ingest_test.go::TestEmailIngest_AttachmentExtraction`
- planned: `test/integration/us_email_ingest_test.go::TestEmailIngest_ContactLinkage`
- planned: `test/integration/us_email_ingest_test.go::TestEmailIngest_DedupeMessageID`
