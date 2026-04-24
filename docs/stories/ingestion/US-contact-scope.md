# US-contact-scope: Contact-Scoped Knowledge Boundary

**System Types:** ctxt
**Personas:** [Operations](../../personas/operations.md)

---

## User Goal

As an agent, my replies are scoped to the contact's knowledge
boundary so I never leak information across contacts.

---

## Context

When agents reply to emails, they must only reference knowledge
within the contact's scope. This enforces the communication
policy: "reply only within thread context + contact's ctxt scope."

Each ingested email is tagged with `contact:<normalized-email>`.
When an agent prepares a reply, ctxt filters search results to
only surface objects tagged with the relevant contact scope.

This story defines the tagging convention and the query-time
filter contract. The actual reply workflow uses these tags via
`ctxt find --tag contact:<addr>`.

---

## Acceptance Criteria

- [ ] Every ingested email object carries a `contact:<addr>` tag
      where addr is the sender's email, lowercased and trimmed.
- [ ] `ctxt find --tag contact:<addr>` returns only objects
      tagged with that contact scope.
- [ ] Thread-level queries (`ctxt find --tag thread:<id>`) are
      further filterable by contact scope.
- [ ] Mixed-contact threads (cc'd parties) produce a contact tag
      per participant address.

---

## Implementation Notes

### Tagging Convention

- Tag format: `contact:<normalized-email>`
- Normalization: lowercase, trim whitespace
- Applied at ingest time by the adapter (not post-processing)
- From address is primary; future: add To/Cc participants

### Query-Time Filtering

- Agents use `ctxt find --tag contact:<addr>` before composing
- Multiple `--tag` flags compose as AND (intersection)
- Contact + thread intersection:
  `ctxt find --tag thread:<id> --tag contact:<addr>`

### Future Extensions

- Contact object linking: `ctxt find --type contact` returns
  contact records; adapter cross-references via email address
- Scope inheritance: org-level contacts inherit from individual
- Opt-out: contacts marked `scope:public` bypass filtering

---

## E2E Test Checklist

- [ ] Ingest email from alice@example.com -> object tagged
      `contact:alice@example.com`.
- [ ] Ingest email from ALICE@EXAMPLE.COM -> tag normalized to
      `contact:alice@example.com`.
- [ ] `ctxt find --tag contact:alice@example.com` returns only
      Alice's emails, not Bob's.

---

## Related Stories

- [US-email-ingest](./US-email-ingest.md) — email ingestion
- [US-0300](./US-0300-importer-extension-interface.md) — importer
  interface

---

## Personas

- [Operations](../../personas/operations.md)

---

## E2E Tests

> Contact scope tagging verified in
> `internal/importer/himalaya/adapter_test.go` (TestContactScopeTag).
> Query-time filtering depends on ctxt find infrastructure.
