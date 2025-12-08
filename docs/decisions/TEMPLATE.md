# ADR-XXX – TITLE OF DECISION

> **Status:** Proposed / Accepted / Deprecated / Superseded
> **Date:** YYYY-MM-DD
> **Author:** Your Name or GitHub Handle
> **Supersedes:** ADR-YYY (optional)
> **Superseded by:** ADR-ZZZ (optional)

---

## Context

Describe the situation that led to this decision.

Include:
- the problem being solved
- relevant background
- constraints (technical, legal, UX, privacy, decentralization)
- which systems or subsystems are impacted
- what goals we are optimizing for (performance, reliability, scalability, UX)

If helpful, link to:
- issues
- discussions
- documents
- prototypes
- diagrams

This section should allow someone 2 years from now to understand *why the problem mattered*.

---

## Decision

State the decision clearly and concisely.

This should be a single, affirmative statement such as:

- “We will adopt RSQL as the base query language.”
- “We will implement ingestion via a transactional outbox.”
- “Pipelines will be step-based and plugin-extensible.”

The decision must be unambiguous.

---

## Rationale

Explain *why* this decision was chosen over alternatives.

Include:
- tradeoffs considered
- alternative approaches and why they were rejected
- benefits of chosen approach
- risks or drawbacks
- ecosystem or architectural alignment
- long-term implications

Avoid repeating the Context section; focus on the reasoning.

---

## Consequences

Detail the impact of this decision—both positive and negative.

### Positive
- e.g., “More reliable ingestion due to crash-safe jobs.”

### Negative
- e.g., “Introduces reranking complexity when merging results.”

### Neutral or Considerations
- e.g., “Plugins must support polymorphic configuration.”

Consider: performance, DX, architectural complexity, operations, extensibility, security, privacy, etc.

---

## Implementation Notes

(This section is optional but encouraged.)

Describe:
- how this ADR should be implemented
- affected modules
- migration concerns
- phasing-in plans
- testing implications
- backwards compatibility notes

This section is not binding but helps guide contributors.

---

## References

Links, discussions, related ADRs, external documentation.

Examples:
- ADR-010 (related query language decision)
- https://github.com/...
- Papers, standards, examples from other systems

---