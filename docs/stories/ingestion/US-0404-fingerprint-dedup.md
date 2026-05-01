---
status: shipped
---

# US-0404: Fingerprint Dedup at Ingest

**System Types:** dpkms (self-hosted), dpkms cloud, ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Operations](../../personas/operations.md)

---

## User Goal

As a knowledge worker, I want duplicate content detected and
blocked at ingest time — via content hash and embedding distance
— so the knowledge graph stays clean without manual dedup effort.

---

## Context

OB1 uses fingerprint dedup at capture (Slack ts dedup, content
hashing). ctxt currently has no explicit dedup. As capture
channels multiply (webhooks, bots, importers), duplicate risk
grows. Dedup at ingest is cheaper than post-hoc lint dedup
(US-0402).

---

## Acceptance Criteria

- [ ] Content hash (SHA-256 of normalized text) checked at
  ingest
- [ ] Embedding cosine similarity checked against recent
  objects (configurable window)
- [ ] Exact duplicates (hash match) blocked with reference to
  existing object
- [ ] Near-duplicates (similarity > threshold) flagged with
  option to merge or skip
- [ ] Source-specific dedup keys supported (e.g., Slack
  timestamp, tweet ID, email message-id)
- [ ] Threshold configurable per profile
- [ ] Dedup decisions logged for audit

---

## Implementation Notes

### Dedup Flow

```
ingest(content, source_key?)
  -> compute content_hash = sha256(normalize(content))
  -> if exact_match(content_hash):
       return existing_object_id (skip ingest)
  -> compute embedding = embed(content)
  -> if near_match(embedding, threshold=0.95):
       return { existing_id, similarity, action: "ask" }
  -> if source_key and source_key_exists(source_key):
       return existing_object_id (skip ingest)
  -> proceed with ingest
```

### CLI Interface

```bash
# Ingest with dedup (default)
ctxt "some content"
# -> "Duplicate detected: matches o-abc123 (exact hash)"

# Force ingest even if duplicate
ctxt "some content" --force

# Configure dedup threshold
ctxt config set dedup.similarity_threshold 0.92

# Ingest with source key
ctxt "slack message" --source-key "slack:C01ABC:1234567890"
```

---

## E2E Test Checklist

- [ ] Ingest same text twice -> second blocked with existing ID
- [ ] Ingest near-duplicate (>0.95 similarity) -> flagged
- [ ] Ingest with `--force` -> bypasses dedup
- [ ] Ingest with source key that exists -> blocked
- [ ] Ingest with source key that doesn't exist -> proceeds
- [ ] Dedup log entry created for each decision
- [ ] Different profile thresholds respected

---

## Related Stories

- US-0402: Knowledge lint (post-hoc dedup detection)
- US-0001: Text capture minimal friction (ingest path)
- US-0403: Structured metadata extraction (runs after dedup)

---

## E2E Tests

- `test/integration/us0404_fingerprint_dedup_test.go::TestUS0404_ExactDuplicateBlocked`
- `test/integration/us0404_fingerprint_dedup_test.go::TestUS0404_SourceKeyBlocked`
- `test/integration/us0404_fingerprint_dedup_test.go::TestUS0404_SourceKeyNewProceeds`
- `test/integration/us0404_fingerprint_dedup_test.go::TestUS0404_ForceBypasses`
- `test/integration/us0404_fingerprint_dedup_test.go::TestUS0404_DedupAuditLog`
