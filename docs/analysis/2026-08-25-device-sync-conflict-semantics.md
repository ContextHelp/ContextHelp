# Device Sync Conflict Semantics — KnowledgeObjects, Edges, Entities

> **Date:** 2026-08-25
> **Applies to:** dPKMS, ctxt
> **Extends:** [2026-08-25-device-sync-replication-survey.md](2026-08-25-device-sync-replication-survey.md)
> **References:** ADR-049, ADR-062, ADR-063,
> [2026-08-25-device-sync-requirements.md](2026-08-25-device-sync-requirements.md),
> [2026-08-04-retraction-propagation.md](2026-08-04-retraction-propagation.md),
> [2026-08-04-blob-federation.md](2026-08-04-blob-federation.md)

The survey selected op-based replication: a semantic, HLC-stamped `sync_log` replayed through per-type merge rules. This note fixes those rules — the core unsolved problem ADR-064 deferred ("Pull support will need conflict resolution strategy; ADR to follow"). Setting: one owner's concurrent edits across 2–5 devices, cooperative not adversarial. Design posture, justified by the append-mostly corpus: **a simple deterministic default everywhere, plus an explicit surfacing path for the rare conflict where determinism would erase real work** (requirements AC3/AC4).

Vocabulary used throughout: two ops are **ordered** when the later op's device had applied the earlier op before issuing (observable from the sync log); otherwise **concurrent**. Ordered ops never conflict — later wins is causally correct. Every rule below is about concurrent ops, and every rule is commutative, associative, idempotent, and a pure function of (op, state) — the properties the prototype's scripted scenarios must test.

## 1. KnowledgeObjects

### 1.1 Identity across devices

**Replication identity is `objects.id` — the durable id — never `content_hash`.** The federation stream keys on `content_hash` and therefore forks silently on edit ([2026-08-04-retraction-propagation.md](2026-08-04-retraction-propagation.md): "Corrections silently fork instead of superseding"); device sync must not inherit this. `id` is minted once at capture, never changes, and is the subject of every op.

The two unique dedup indexes remain locally enforced and become *merge inputs*:

- **`content_hash` collision (UNIQUE, `migrations/003_content_hash_reinforcement.sql`):** two devices independently capture the same content → two ids, one hash. Replaying the remote `object.create` trips the unique index; the merge rule converts it: the create with the **lower HLC is canonical** (deterministic on every device); the other id is recorded in a new `object_id_aliases` table (`alias_id → canonical_id`; the existing `aliases` table, `migrations/010_aliases.sql`, maps *names*, not ids — different job); the losing create is degraded to a `reinforce` op on the canonical id. Subsequent ops addressed to an aliased id resolve through the alias before apply. No data lost, no duplicate row, reinforcement semantics preserved.
- **`source_key` collision (`migrations/026_source_key.sql`):** same external artifact (same Slack ts, same message-id) captured on two devices — same rule: lower-HLC create canonical, other aliased, metadata merged per §1.2.

**Edits supersede, never fork (AC6).** `object.edit` carries `{id, new_content, prior_content_hash}`; apply rewrites content in place, recomputes `content_hash`, and records the old→new hash supersession locally (the record federation's future `corrected` tombstone needs — same fact, one write). The id is stable through any number of edits on any devices.

### 1.2 Field-class merge matrix

| Field class | Fields (`migrations/001_initial.sql` + later) | Rule for concurrent ops |
|---|---|---|
| **Capture immutables** | `id`, `type`, `subtype`, `source`, `source_key`, `created_at` | Written once at create; no op mutates them → conflicts impossible. Divergence here means an identity bug: surface to anti-entropy repair, never merge. |
| **Content registers** | `raw_content`, `content_type` | LWW by HLC **with loser preservation**: concurrent `object.edit` × `object.edit` → higher HLC wins the row; loser becomes a conflict copy (§5). The only field class where the deterministic rule can erase real work, hence the only one that pays for surfacing. |
| **Curation scalars** | `status`, `inbox_note`, `profile_id` | LWW by HLC, with the status exceptions in §1.3. Cheap, reversible, and any race is between two decisions the same owner made — latest intent is the honest default. |
| **Accretive sets** | `graph_json` nodes (stable node UUIDs per ADR-063), `tags`, `mentions`, `summaries`-as-nodes | **OR-set, add-wins**: union of node sets keyed by node UUID; a remove deletes only the add-instances it observed. Concurrent enrichment on two devices merges losslessly — the dominant "conflict" in an append-mostly corpus, resolved with zero user attention. Per-node *mutation* (rare; nodes are append-typed) is LWW by HLC within the node. Intra-object `graph_json` edges follow their endpoint nodes. |
| **Counters** | `reinforcement_count` | Sum of `reinforce` ops (op-based counter — replay gives exact counts, no max() approximation); `last_reinforced_at` = max HLC. |
| **Derived, never synced** | `fts_indexed`, `vector_indexed`, `embeddings`, `objects_fts` rows, vec0 rows | Not in the op vocabulary at all (§4). |
| **Legacy flat columns** | `sections`, `tags`, `decisions`, `tasks` JSON columns | Projection cache per ADR-063 — regenerated from merged `graph_json`, never merged directly. |

### 1.3 Status transitions as a lattice with two exceptions

Live values: `inbox` → `active` → `discarded` (`internal/storage/types.go:105`, default `active`; the ADR-062 lifecycle's captured/curated/discarded). Rules for concurrent status ops:

1. **Status × status → LWW by HLC.** Two explicit dispositions by the same owner: take the later. Deterministic, reversible (status is not deletion), and any lattice we could impose (e.g. discard-anywhere-wins) would sometimes override the owner's *more recent* decision — worse than honest LWW.
2. **Discard × content-edit → edit revives.** Concurrent `set_status(discarded)` on A and `object.edit` on B: the edit wins; the object lands `active` with B's content; the discard is preserved as a review-queue suggestion (§5), not applied. Rationale: an edit is invested work, a discard is a cheap reversible flick; the asymmetric rule loses nothing (the discard suggestion survives) while discard-wins would bury a fresh edit. This is deliberately the *opposite* of the deletion rule —
3. **Delete (tombstone) × anything-concurrent → delete wins** (§3). Deletion is destructive intent and resurrection is the S4 threat; concurrent edits are preserved as recoverable conflict copies in the review queue, not in the corpus.

The pair (2)/(3) is the design's sharpest choice: *discard is revivable, delete is not*. It gives users a safe verb (discard) and a strong verb (delete) with predictable sync behavior for each.

## 2. Edges

Inter-object edges are the relationship source of truth (ADR-049; `edges` table, `migrations/001_initial.sql:48-58`).

- **Semantic identity:** `(from_type, from_id, to_type, to_id, edge_type)`. The `id` column is instance-minted; concurrent `edge.add` of the same semantic edge on two devices → one edge, **lower-HLC op's id kept**, the other add degrades to a no-op (after id-alias resolution of endpoints). Idempotent by construction.
- **Set semantics: OR-set, add-wins.** `edge.remove` removes only observed adds — a re-add concurrent with a remove survives. Matches user expectation: "I linked these on my laptop" should not be silently undone by an unrelated removal race on the phone.
- **`weight`, `metadata`:** LWW by HLC per edge; no surfacing (derived/advisory values, cheap to recompute — many are pipeline-written).
- **Dangling-edge rule: referential integrity beats add-wins.** `edge.add` concurrent with a tombstone on either endpoint: the edge is **not** applied (or is cascade-removed when the tombstone arrives second — order-independent outcome, same final state). The suppressed edge goes to the review queue attached to the surviving endpoint, so deliberate intent ("this was linked to the thing you deleted elsewhere") is visible rather than silently swallowed. Aligned with the local cascade (`Service.DeleteObject` deletes edges before the object; FK `ON DELETE CASCADE` on adjacent tables) and with delete-wins (§1.3.3).

## 3. Deletions and tombstones

- **Discard is not deletion.** Discard = `set_status` op (§1.3), revivable, content retained. Delete = `object.delete` op producing a **tombstone**; apply invokes the atomic local purge — row, edges, FTS entry, vec row, embeddings, blob refs — the primitive the retraction note names as prerequisite ([2026-08-04-retraction-propagation.md](2026-08-04-retraction-propagation.md): "Even local deletion is incomplete"). Device sync shares this dependency; it is on the foundation checklist.
- **One tombstone table, two consumers.** The retraction note's `Tombstone{ObjectID, ContentHash, Reason, SupersededBy, Scope, IssuedAt, Signature}` gains `hlc` and `origin_device`. A deletion is one fact; federation (outbound retraction) and device sync (roster convergence) are two propagation channels over the same row — parallel tables would let the same deletion exist in one and not the other.
- **Resurrection prevention (AC5/S4):** the tombstone outlives the row. Incoming ops addressed to a tombstoned id: concurrent-with-tombstone → suppressed, preserved in review queue (delete-wins); causally *after* the tombstone (device saw the delete, user re-captured) → legitimate new object (new id, new create — nothing to suppress). A lagging device's re-push of the dead object's create/edit is suppressed by id, not content-hash, so an *independent* later capture of identical content on a clean id is allowed — deletion retracts *that object*, not the content forever (same posture as blob-federation §4: tombstones retract rows, not bytes).
- **GC by causal stability over the roster.** The same-owner advantage over open federation: the peer set is enumerable. A tombstone is GC-able when `min(acked HLC over active roster devices) > tombstone.hlc` **and** a retention floor has passed (default 90 days per the retraction note; the exact-roster argument may justify shorter — operator-tunable). **Revoked devices leave the min** (else a stolen phone blocks GC forever); a device *unrevoked but silent* past the floor is flagged to the owner rather than silently dropped from quorum.
- **Returning past the horizon:** epoch rule (retraction note §3). A device whose last ack predates a tombstone-GC epoch bump reconciles by full resync: current merged state wins; the device's never-synced local ops replay on top through these same rules; its stale rows cannot resurrect because they arrive as state, not ops, and state reconciliation is digest-driven repair (survey §2.C anti-entropy), which takes the epoch side.

## 4. Derived state — the invariant, confirmed

**FTS rows, vec0 rows, and embeddings never appear in the op vocabulary and are never merged.** Each applied op runs the same per-object index maintenance as a local write (the `Update` path already maintains FTS + vec; ADR-070 bucket-1 semantics), so post-sync rebuild cost is O(applied ops). Embeddings are deterministic per (content, model): a device may ship them as an *optional cache hint* alongside ops (they already travel in federation pushes — [2026-08-04-blob-federation.md](2026-08-04-blob-federation.md) "text_content and embeddings do travel"), but a receiver treats a hint as untrusted-if-absent: correctness never depends on it, and a model-version mismatch (`embedding_models` registry, ADR-071) discards the hint and recomputes. `index_signatures` (per-table, ADR-070) stay strictly local — two devices on different pipeline versions converge on *content* and each rebuild their own indexes.

Blob bytes: out of scope here (identity/transport note states the requirement; design defers to [2026-08-04-blob-federation.md](2026-08-04-blob-federation.md) Option C). The op stream carries the ref and the manifest entry; bytes move content-addressed and lazily.

## 5. Conflict surfacing

Where a deterministic rule discarded information, the loser must be visible (AC4):

- **Conflict copies.** A losing concurrent content edit (§1.2) is preserved as a sibling object: same `type`, fresh id, tagged `sync-conflict`, linked `conflicts_with → winner` in `edges`, status `inbox` — so it surfaces through the normal inbox/review flow instead of a bespoke UI, and is excluded from default `active` search (`types.go:105` status filtering) without being hidden. Syncthing's `.sync-conflict` files prove the pattern; CouchDB's preserved losing revisions prove the posture.
- **Review-queue entries.** Suppressed discards (§1.3.2), suppressed dangling edges (§2), and delete-concurrent edits (§3) enqueue into `resurfacing_queue` (`migrations/017_resurfacing_queue.sql`) with `reason = "sync-conflict:<class>"`, plus one `system_reminders` entry per sync batch that produced conflicts (not per conflict — no notification storms).
- **Resolution is ordinary ops.** `ctxt sync conflicts` lists open conflicts; `resolve --keep-mine|--keep-theirs|--merge` emits normal `object.edit`/`set_status`/`edge.add` ops — resolution replicates like any other write, and the wire protocol has no special resolution verb. In topology T1 the hub is the natural review surface (always on, full corpus), but any device can resolve.
- **Audit.** Every applied remote op and every conflict decision (automatic or user) appends to `audit_log` (`migrations/011_audit_log.sql` — append-only by trigger), satisfying AC12 and making "why does this object look like this" answerable after the fact.

## 6. Entities

Entities are keyed by natural key `slug` (PK, `migrations/001_initial.sql:36-45`) — no cross-device id problem. Three provenance classes, three rules:

- **Registry-sourced** (`registry_url != ''`, `migrations/013_entity_thin_sync.sql`): the registry is authoritative; rows arrive via registry sync on every device independently. Device sync does **not** carry them — replicating would race the registry and entangle two protocols. `content_status`/`version_hash` stay local-per-device.
- **Locally derived from mentions** (extraction pipelines): re-derivable at will — ADR-064 already classifies entities as "derived — re-extracted at target". Device sync carries them only implicitly: objects and mention edges sync, and local re-derivation converges. `UpsertThin`'s existing redundancy tolerance (used by both federation receive and registry sync) absorbs double-derivation harmlessly.
- **Owner-curated enrichment** (manual description edits, alias additions, metadata): the part that must sync — it is work, not derivation. `entity.enrich` ops: `description`/`title`/scalar `metadata` keys are LWW registers by HLC; `aliases` is an OR-set union. No conflict copies for entities: scalars are short, low-stakes, and re-editable; LWW loss here does not meet the surfacing bar (posture: deterministic default, surface only where work would vanish).
- **Deletion:** entity tombstones only for curated entities; a derived entity deleted on one device while mentions still exist elsewhere simply re-derives — deletion of derived state is local hygiene, not a replicated fact.

## 7. Determinism obligations (test contract)

The prototype's scripted scenarios (foundation note) must pin, per rule above, the invariant *apply(opA, apply(opB, S)) = apply(opB, apply(opA, S))* and idempotence *apply(op, apply(op, S)) = apply(op, S)* for: create×create same content-hash; edit×edit same object; status×status; discard×edit; delete×edit; delete×edge.add; edge.add×edge.add same semantic edge; edge.remove×edge.add; enrich×enrich same entity; reinforce×reinforce. Apply must be a pure function of (op, state): no wall-clock reads, no config-dependent branches, no map-iteration-order dependence in `graph_json` merge.

## Open questions for the ADR

1. Whether `object_id_aliases` rows ever GC (safe once no device holds ops referencing the alias — same causal-stability rule; cheap to keep forever).
2. Whether the discard-vs-edit exception (§1.3.2) extends to `inbox_note` edits (currently: no — note edits are curation scalars, LWW).
3. Conflict-copy retention: auto-expire unresolved copies (e.g. 180 days) or keep until acted on (leaning: keep — they are ordinary objects, the corpus is the archive).
4. Whether `profile_id` moves need a guard (concurrent move to two different profiles → LWW; a profile-scoped visibility surprise, maybe worth a review-queue entry).
