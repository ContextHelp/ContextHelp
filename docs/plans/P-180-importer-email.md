# Email Importer Plan

> **Status:** Proposed
> **Date:** 2026-02-19
> **Depends on:** Phase 10 (complete), US-0300 importer interface (partially implemented), US-0106 enqueue API (complete)

## Goal
Ship an out-of-box email importer for dPKMS/ctxt that can ingest newsletters, billing mail, alerts, and general mailbox traffic with:
- protocol-flexible collection,
- deterministic filter-based routing, and
- per-filter pipeline assignment.

## Fit With Current Roadmap
- **Phase 10 (complete):** Reuse persistent pipelines, step discovery, enqueue path, and worker durability.
- **Importer contract (US-0300):** This feature should advance the generic importer run/checkpoint model that other source importers can share.
- **Pipeline management (US-0101..US-0113):** Routing should target built-in/custom pipelines and keep per-source override behavior.

## Data Surface
- **Primary objects:** email message body, headers, attachments (metadata first; content extraction optional by pipeline).
- **Core identifiers:** account_id + provider + message_id (+ mailbox UID when Message-ID missing).
- **Incremental sync:** protocol-specific checkpoint cursors.

## Protocol Matrix

### Recommended for MVP
1. **IMAP4rev1 + STARTTLS/TLS**
- Pros: widely supported, mailbox/folder semantics, no provider lock-in.
- Cursor: UIDVALIDITY + UID per mailbox.
- Notes: support IMAP SEARCH to reduce transfer.

2. **Local file import (`.eml`, `mbox`, `maildir`)**
- Pros: deterministic fixtures, offline/backfill friendly, easy testing.
- Cursor: file fingerprint + last offset/UID map.

### Recommended for Phase 2
3. **Gmail API**
- Pros: robust labels/search/history sync, modern OAuth.
- Cursor: `historyId`.

4. **Microsoft Graph (mail)**
- Pros: modern auth, delta query support.
- Cursor: delta link / delta token.

### Recommended for Phase 3
5. **Inbound webhook gateways** (SES inbound, Mailgun routes, SendGrid Parse, Postmark inbound)
- Pros: near-real-time push into dPKMS HTTP endpoint.
- Cursor: provider event ID + idempotency key.

6. **SMTP listener (optional, self-hosted advanced mode)**
- Pros: direct receive path.
- Tradeoff: highest ops/security burden (TLS, anti-abuse, queueing, spam control).

### Explicitly Out of MVP
- POP3 primary path (limited metadata/folder support, weak incremental semantics).
- JMAP (good future option, lower immediate ecosystem priority).

## Filter and Routing Model

### Rule Engine Requirements
- First-match-wins by priority (smallest `priority` first), with explicit fallback rule.
- Deterministic and side-effect free evaluation.
- Dry-run mode for rule testing against sample messages.
- Explainability output: matched rule ID + matched predicates.

### Predicate Surface (minimum)
- Envelope/headers: `from`, `to`, `cc`, `reply_to`, `subject`, `list_id`, `message_id`.
- Domain and address helpers: `from_domain`, `to_domain`, `sender_in`.
- Header arbitrary match: `header["X-..."]`.
- Structural: `has_attachment`, `attachment_ext`, `attachment_mime`, `size_bytes`.
- Mailbox metadata: `folder`, `label`, `is_unread`, `received_at`.
- Content hints: `subject_contains`, `body_contains` (optional for MVP, guarded by size cap).

### Actions (minimum)
- `route_pipeline`: pipeline name (for example `email.newsletter`, `email.billing`, `text.long`).
- `set_type` / `set_subtype`: defaults `type=email`, subtype by category.
- `set_tags`: append tags (`newsletter`, `invoice`, `receipt`, `security-alert`).
- `set_source`: canonical source string (example: `email:gmail:acct_123`).
- `drop`: skip enqueue and record skip reason.

### Example Rule Config (YAML)
```yaml
rules:
  - id: billing-invoices
    priority: 10
    when:
      from_domain_in: ["stripe.com", "aws.amazon.com", "openai.com"]
      subject_regex: "(invoice|receipt|payment)"
    action:
      route_pipeline: "email.billing"
      set_subtype: "billing"
      set_tags: ["billing", "invoice"]

  - id: newsletters
    priority: 20
    when:
      any:
        - header_exists: "List-Id"
        - from_regex: "newsletter|digest|substack"
    action:
      route_pipeline: "email.newsletter"
      set_subtype: "newsletter"
      set_tags: ["newsletter"]

  - id: fallback
    priority: 9999
    when: { always: true }
    action:
      route_pipeline: "text.long"
      set_subtype: "email"
```

## Pipeline Strategy
- **`email.newsletter`** (new): extract meaningful body text, normalize links, summarize, tag.
- **`email.billing`** (new): prioritize structured fields (vendor, amount, due date); route invoice attachments to `doc.pdf`/`doc.office` helper path.
- **`email.alert`** (optional): short noisy operational/security alerts with high-signal tagging.
- Reuse existing built-ins (`text.short`, `text.long`, `doc.pdf`, `doc.office`) where possible.

## API/CLI Touchpoints

### CLI
- `ctxt import email --provider imap --host imap.example.com --user ...`
- `ctxt import email --provider gmail --account <name>`
- `ctxt import email --file ./archive.mbox`
- Common flags: `--since`, `--folder`, `--max-items`, `--dry-run`, `--rules <file>`, `--pipeline <override>`, `--server`.

### API (target contract)
- `GET /api/v1/importers` (discover email importer capability)
- `POST /api/v1/importers/email/run`
- `GET /api/v1/importers/runs/{run_id}`
- `POST /api/v1/importers/email/validate-rules` (optional but recommended)
- `POST /api/v1/importers/email/webhooks/{provider}` (Phase 3)

## Storage and State
- **Importer checkpoint store:** key `(profile, importer=email, account, folder)` -> cursor payload (UID/history/delta token).
- **Importer run store:** status + scanned/imported/skipped/failed counters + error summary.
- **Optional rule persistence:** profile-scoped ruleset registry with versioning and rollback.
- **Dedup key:** `provider + account + message_id` fallback to canonical content hash when missing.

## Security and Compliance Notes
- OAuth2 for Gmail/Graph; IMAP app password only where required.
- Encrypt auth tokens at rest; redact secrets from logs.
- Treat email as sensitive by default (PII/financial data).
- Attachment guardrails: size caps, MIME allowlist, optional malware scan hook.
- Webhook mode requires signed request verification and idempotency keys.

## Implementation Plan
1. **Contract + schemas (0.5-1 day)**
- Define `email` importer config and rule schema.
- Add parser/validator for rules and predicate DSL.
- Add fixtures for `.eml` and `mbox`.

2. **MVP protocol adapters (1.5-2.5 days)**
- Implement local file adapters (`.eml`, `mbox`, `maildir`).
- Implement IMAP adapter with mailbox pagination and cursor checkpoint.
- Normalize headers/body/attachments metadata into importer records.

3. **Filtering + routing engine (1-1.5 days)**
- Build rule evaluator (priority, predicate matching, explain output).
- Apply action mapping to enqueue request (pipeline/type/subtype/tags/source).
- Add dry-run explain CLI output.

4. **CLI and enqueue integration (0.5-1 day)**
- Add `ctxt import email` command.
- Support `--rules`, `--dry-run`, `--since`, `--folder`, `--max-items`.
- Preserve compatibility with existing enqueue/analyze surface.

5. **Provider adapters (Phase 2, 2-3 days)**
- Add Gmail API and Graph adapters with token refresh and incremental sync.
- Persist provider-specific checkpoints.

6. **Webhook ingestion (Phase 3, 1.5-2 days)**
- Add inbound webhook endpoints + signature verification.
- Add idempotent event processing and dead-letter handling.

7. **Hardening + docs (0.5-1 day)**
- Metrics: scanned/imported/skipped/failed, rule hit rates, dedup rate.
- Error taxonomy: auth, rate_limit, malformed, transient, internal.
- Troubleshooting docs and sample rule packs (`newsletter`, `billing`, `alerts`).

## Key Risks
- IMAP provider quirks (UID resets, folder naming, server limits).
- Rule complexity drift without guardrails (performance/explainability).
- Attachment-heavy mailboxes increasing processing cost.
- Compliance constraints for personal/financial mailbox ingestion.

## Verification
- Unit tests for parser normalization and rule evaluation edge cases.
- Integration tests for `.eml` and `mbox` backfill.
- IMAP incremental sync test with checkpoint resume.
- Idempotency test: repeated import produces no duplicate objects.
- Routing tests: known fixture set maps to expected pipeline per rule.
- Failure recovery: resume after simulated interruption/restart.

## Exit Criteria
- MVP protocols (`.eml`/`mbox`/IMAP) ingest successfully with checkpoint resume.
- Filter engine deterministically routes to correct pipelines on golden fixtures.
- Duplicate rate < 1% on repeated imports.
- Dry-run and explain outputs make rule debugging practical.
- Security baseline complete (token protection, webhook verification where applicable, redacted logs).
