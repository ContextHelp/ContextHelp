---
status: paper
---

# US-0321: Cardamum Contacts Ingestion Adapter

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As an agent, I ingest contacts from cardamum so I can search
them via `ctxt find`.

---

## Context

Cardamum is a CLI contact manager backed by CardDAV. Contacts
are stored as vCards. This adapter reads `cardamum cards list
--json`, transforms each vCard into a ctxt object (type:
contact), and deduplicates by card UID.

---

## Acceptance Criteria

- [ ] Adapter calls `cardamum cards list --json <addressbook>`
- [ ] vCard fields extracted: FN, ORG, TITLE, EMAIL, NOTE, UID
- [ ] Objects tagged: `source:cardamum`, `org:<ORG>`,
      `role:<TITLE>`
- [ ] Metadata includes: name, email, org, title, addressbook
- [ ] Content is human-readable summary of contact
- [ ] Dedup by UID: re-running does not create duplicates
- [ ] `--addressbook` flag selects cardamum addressbook
- [ ] `--account` flag selects cardamum account
- [ ] `--binary` flag overrides cardamum binary path

---

## Implementation Notes

- `internal/ingest/cardamum/cardamum.go` — adapter + vCard parser
- vCard line folding handled (RFC 6350 continuation lines)
- vCard backslash escapes unescaped (`\,` -> `,`)

---

## E2E Test Checklist

- [ ] Create temp vdir with test vCards
- [ ] `ctxt ingest --source cardamum` ingests contacts
- [ ] `ctxt find <name>` returns ingested contact
- [ ] Re-running ingest does not create duplicates
- [ ] Contacts have correct tags and metadata

---

## Related Stories

- [US-0320](./US-0320-adapter-ingestion-interface.md) —
  generic adapter interface

---

## E2E Tests

- planned: `test/integration/us0321_cardamum_contacts_test.go::TestCardamumContacts_ImportVCards`
- planned: `test/integration/us0321_cardamum_contacts_test.go::TestCardamumContacts_DedupeByEmail`
- planned: `test/integration/us0321_cardamum_contacts_test.go::TestCardamumContacts_LinksToObjects`
- planned: `test/integration/us0321_cardamum_contacts_test.go::TestCardamumContacts_ScopeBoundary`

## Personas

- [Solo Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/solo-developer.md)
