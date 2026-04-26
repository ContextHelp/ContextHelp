# Inbound Contract — Hooks and Adapters Writing to ctxt

This document defines the contract that external hook frameworks
and session adapters MUST follow when they invoke
`ctxt analyze` to push events, sessions, or other operational
traces into the knowledge graph. It is consumer-facing: the
target audience is an engineer wiring a new adapter, hook
runtime, or bridge tool that will write into ctxt at scale.

The contract is intentionally narrow. ctxt's storage, mention,
and entity semantics are already specified in
[`../dpkms/mentions.md`](../dpkms/mentions.md),
[`../dpkms/schema-entity.md`](../dpkms/schema-entity.md), and
[`../dpkms/registries.md`](../dpkms/registries.md). This page
does not redefine those primitives — it describes the
producer-side conventions every inbound writer is expected to
honor so that retrieval, deduplication, and graph navigation
behave consistently across producers.

## Producers covered

The following two writers are the canonical reference
implementations of this contract:

- **uhp rule engine `ctxt:capture` observer** (live ingestion;
  one ctxt object per matched UHP event). See §3 of
  <https://github.com/hop-top/hop/blob/main/docs/ingestion-retrieval/spec.md>.
- **`usp-ctxt sync`** (batch ingestion; one ctxt object per
  CLI session, idempotent across re-runs). See
  <https://github.com/hop-top/usp/tree/main/cmd/usp-ctxt>
  and the same spec, §4.

Any new inbound writer SHOULD follow the same conventions; the
mention namespaces below are reserved for these producers and
their successors.

## Tag conventions vs. mentions

ctxt has two distinct producer-facing primitives. Inbound
writers MUST use them correctly:

- **Mentions** (`@namespace.slug`) are the identity layer.
  Mentions are canonical, durable, and resolve to entities.
  Every inbound writer SHOULD attach mentions via the
  `--mentions` flag (preferred) or by embedding `@slug` tokens
  in the body that `ctxt`'s parser will extract. Mentions
  drive entity-anchored retrieval (`ctxt find --mention
  @agent.kai`), the bookmark→entity reverse index, and graph
  navigation across producers.
- **Tags** (`#word`) are emergent classifiers. Tags are useful
  for severity, phase, or other classification signals that do
  not warrant a stable entity. Tags do NOT carry identity, do
  NOT create graph edges, and MUST NOT be used to encode the
  agent, CLI, session, event, project, scope, incident, or
  decision attributes — those are mention namespaces.

The full taxonomy of mention namespaces emitted by current
producers is in the registry at
<https://github.com/hop-top/hop/blob/main/docs/ingestion-retrieval/mentions-registry.md>.
New writers MUST land a registry entry before emitting a new
namespace.

### Required mention namespaces

| Namespace | Meaning | When emitted |
|---|---|---|
| `@agent.<id>` | aps profile producing the object | when the agent is known |
| `@cli.<name>` | CLI runtime that produced the trace | always (closed set: `claude`, `codex`, `gemini`, `opencode`) |
| `@project.<slug>` | resolved project (repo basename, lowercased, `.` to `-`) | when cwd resolves; otherwise `@project.unknown` |
| `@scope.<value>` | logical scope routing | when scope is set |
| `@event.<type>` | UHP event class, lowercased | live writers only |
| `@usp.session.<uuid>` | usp session identity | session-shaped writers only |
| `@usp.lineage.<uuid>` | first session in a cross-CLI chain | when the session is part of a lineage |
| `@incident` | flagged incident, retrieval anchor | when explicitly flagged |
| `@decision` | architectural decision marker | when explicitly flagged |

## Object identity rules

ctxt resolves identity in two layers:

1. **Bookmark-level dedup**: ctxt computes a content hash over
   the normalized body. Re-ingesting an identical payload
   short-circuits at this layer — no new bookmark is created.
2. **Entity-level identity**: every mention attaches the
   bookmark to the corresponding entity via the
   bookmark→entity edge. Re-ingesting a payload with the same
   `@usp.session.<uuid>` mention reuses the same entity even
   when the body has changed (more turns appended, for
   example) — both bookmarks then edge into a single canonical
   entity.

For session-shaped objects, the
`@usp.session.<uuid>` mention is the authoritative
session-level identity anchor. For event-shaped objects (one
bookmark per UHP event), there is intentionally no
session-level entity — events are fungible and accumulate
edges into the stable `@agent.*`, `@cli.*`, and `@event.*`
entities.

A `--source-key <prefix>/<id>` MAY be passed as a secondary
dedup hint for ctxt's external-id catalog (e.g. `usp/<session-id>`,
`slack/<message-ts>`, `tweet/<tweet-id>`). Use it to update an
existing bookmark in place rather than creating a new one when
the payload changes. It is not a substitute for mentions —
identity flows through the mention layer.

## Idempotency contract

Inbound writers MUST be safe to re-run. The contract has three
parts:

1. **Stable mention emission.** Given the same logical input
   (same session, same event, same context), the writer MUST
   emit the same mention list every time. Slugs MUST be
   lowercased, MUST NOT contain whitespace, and MUST follow
   ctxt's slug rules in
   [`../dpkms/mentions.md`](../dpkms/mentions.md). UUIDs are
   slug-safe as-is.
2. **Body normalization.** The body the writer pipes to ctxt
   MUST be deterministic for a given input. Floating-point
   timestamps, map iteration order, or unsorted lists will
   defeat content-hash dedup and create silent duplicates.
   Sort fields, round timestamps to a stable resolution, and
   serialize JSON with sorted keys before composing the body.
3. **High-water-mark state.** Batch writers SHOULD maintain a
   per-source progress file so each run only ingests new work.
   The reference implementation lives at
   `~/.local/share/usp-ctxt/last_run.json` and tracks
   `last_started_at` per CLI; a partial-failure run advances
   the high-water-mark only past successfully ingested items
   so failures retry on the next tick.

The `--wait` flag SHOULD be used when the writer needs the
ingested object to be searchable before the writer exits — for
example, when a downstream rule will run `ctxt find` in the
same dispatcher tick. Without `--wait`, indexing is
asynchronous and the new object may not appear in the next
query.

## Wire shape

The recommended invocation for an inbound writer:

```
ctxt analyze \
  --mentions "<space-separated @namespace.slug list>" \
  [--hints "<space-separated #classifier list>"] \
  [--source-key <prefix>/<id>] \
  [--wait] \
  [--file <body-tempfile> | (body on stdin)]
```

- `--mentions` is the identity payload. Pass it explicitly even
  when mentions also appear inline in the body — explicit
  flags are deterministic and survive body truncation.
- `--hints` is optional and carries non-identity classifiers
  only.
- `--source-key` is optional and carries an external-system ID
  for legacy dedup.
- `--wait` blocks until the object is indexed; use it when the
  writer expects the object to be queryable immediately.
- The body is markdown. A `Mentions:` line at the top of the
  body is a useful redundancy: humans see the identity
  attachments at a glance, and ctxt's parser extracts the same
  slugs the flag carries.

## Failure semantics

Inbound writers MUST fail open: if `ctxt analyze` times out,
exits non-zero, or the ctxt server is unreachable, the writer
MUST log the failure and continue. Live writers (the rule
engine observer) operate under a 2-second per-call budget so
they cannot stall the upstream event pipeline. Batch writers
operate under a longer per-call budget (30 seconds is the
reference value) and roll the high-water-mark forward only on
success.

A failed ingest MUST NOT corrupt the high-water-mark, MUST NOT
emit partial mentions, and MUST NOT block the producer's
upstream work. ctxt is intentionally a best-effort sink: when
it is unavailable, the producer keeps producing and the next
batch run catches up.

## Cross-references

The full ingestion + retrieval design — including the live
capture path, the batch bridge, and the three retrieval shapes
that consume the resulting graph — lives in:

- <https://github.com/hop-top/hop/blob/main/docs/ingestion-retrieval/spec.md>
- <https://github.com/hop-top/hop/blob/main/docs/ingestion-retrieval/mentions-registry.md>

ctxt-side primitives this contract builds on:

- [`../dpkms/mentions.md`](../dpkms/mentions.md) — mention
  syntax, parsing, and storage semantics
- [`../dpkms/schema-entity.md`](../dpkms/schema-entity.md) —
  entity schema
- [`../dpkms/registries.md`](../dpkms/registries.md) — entity
  registry federation
- [`../ctxt/api-cli.md`](../ctxt/api-cli.md) — full
  `ctxt analyze` flag reference
