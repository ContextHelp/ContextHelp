# ADR-064 – Federation: Multi-Instance Object Sync

> **Status:** Proposed
> **Date:** 2026-03-31
> **Author:** $USER
> **Applies to:** dPKMS
> **Supersedes:** None
> **References:** ADR-037, ADR-049, ADR-063

---

## Context

dPKMS instances are per-profile, per-user, per-machine — each owns a separate SQLite DB, port,
and pipeline config. No mechanism exists to merge knowledge across instances or push to shared
team/org servers.

Pain points:

- Founder + researcher + engineer profiles each capture knowledge independently; no merged view.
- Team members cannot share enriched objects with a shared org instance.
- Remote org-level archives require manual export/import today.
- ctxt registry (existing) provides read-only federated search; no write/push path.

Diagram: [topology](ADR-064-federation/topology-v1.mmd)

---

## Decision

Adopt a configurable push-only, DAG federation model.

- Each instance pushes **objects + edges** to zero or more named federation targets.
- Federation target = local SQLite path OR remote dPKMS HTTP URL.
- Target is itself a dPKMS instance; may have its own upstream federations (DAG, no cycles).
- Two sync modes per target:
  - **async** — background goroutine polls on configurable interval.
  - **inline** — pipeline step fires synchronously during job processing.
- Fan-out supported: same DB may appear in multiple federation configs.
- No special "federation mode" — every instance identical; only config differs.

Config shape (pseudocode):

```
federations:
  - name: local-merged
    url: /path/to/merged.sqlite
    interval: 5m
    sync_mode: async
  - name: team
    url: https://team.internal:8080
    interval: 1h
    sync_mode: async
  - name: realtime-mirror
    url: /path/to/mirror.sqlite
    sync_mode: inline
```

What gets federated:

| Artifact       | Federated | Reason                                          |
|----------------|-----------|-------------------------------------------------|
| Objects        | yes       | core knowledge unit; deduped by content-hash    |
| Edges/mentions | yes       | relationship graph travels with objects         |
| Entities       | derived   | re-extracted at target on ingest                |
| Search indexes | derived   | rebuilt at target from objects                  |
| Jobs           | no        | per-instance queue; not portable                |
| Feeds/watches  | no        | per-instance config                             |
| Pipelines      | no        | per-instance config                             |
| Backup         | no        | per-instance; see Backup & Recovery below       |
| Housekeeping   | no        | per-instance; VACUUM/index rebuild stays local  |

---

## Rationale

### Chosen: push-only DAG federation

- Push-only eliminates conflict resolution for v1; reversible when pull added later.
- DAG topology mirrors real org hierarchies (individual → team → org).
- Local SQLite path as valid target enables zero-network personal merge use-case.
- Every instance stays identical; no "federation daemon" or special role required.
- Content-hash dedup at target is idempotent; safe to re-push.
- Async mode reuses existing worker pool in `serve.go`; minimal new infrastructure.
- Inline mode composes as a pipeline step; reuses step abstraction.

### Rejected alternatives

1. **Shared single DB** — SQLite write-lock contention under concurrent instances; destroys
   profile isolation. Rejected.
2. **ctxt registry (read-only federated search)** — search only; no enrichment or write path.
   Does not satisfy push use-case. Rejected as sole mechanism.
3. **Full bidirectional sync (v1)** — conflict resolution, vector clock, tombstones; far exceeds
   v1 scope. Deferred to Phase 3.
4. **Separate federation daemon** — additional process to deploy/monitor; every instance already
   runs a worker pool. Rejected; inline + async modes cover both latency classes.

---

## Consequences

### Positive

- Personal multi-profile merge: zero-network, SQLite-to-SQLite, no auth required.
- Team/org push: HTTP target enables cross-machine knowledge sharing.
- DAG composes naturally; intermediate nodes act as aggregators.
- Content-hash dedup: idempotent push; safe retries; no duplicates at target.
- No new instance type: uniform deployment model preserved.
- Phase 1 ships SQLite-only; adds immediate value before HTTP remote is built.
- Federation DBs self-heal: restore any source instance + `dpkms serve` = full rebuild.
- Backup scope stays simple: one DB + one config file per instance; no cross-instance state.

### Negative

- Push-only: target does not propagate back; instances diverge intentionally.
- Async lag: knowledge not immediately visible at target (tunable via interval).
- Watermark table: small schema addition required in every instance DB.
- Inline mode adds latency to job processing for that pipeline; operator must opt in.

### Neutral / Considerations

- Entities and search indexes rebuilt at target — enrichment pipelines must run at target too.
- Cycle detection in DAG config required at startup (not runtime).
- HTTP pusher (Phase 2) needs auth; token-per-target config shape TBD.
- Pull support (Phase 3) will need conflict resolution strategy; ADR to follow.

---

## Implementation Notes

### New config shape

`FederationConfig` added to `config.go` (pseudocode):

```
type FederationEntry {
  Name     string
  URL      string    // sqlite path OR http(s) URL
  Interval duration  // async only; ignored for inline
  SyncMode string    // "async" | "inline"
}

type Config {
  ...
  Federations []FederationEntry
}
```

### New package: `internal/federation`

```
interface Pusher {
  Push(ctx, objects []Object, edges []Edge) error
  Name() string
}

struct LocalPusher implements Pusher {
  // SQLite-to-SQLite via store.Objects().Upsert() at target
}

struct RemotePusher implements Pusher {
  // HTTP POST to /api/v1/federation/push at target (Phase 2)
}
```

### Async mode — `serve.go`

- New background goroutine alongside worker pool; one per async federation entry.
- On tick: `SELECT objects WHERE updated_at > last_synced_at LIMIT batch`.
- Calls `Pusher.Push(ctx, batch, edges)` per configured async federation.
- On success: update watermark in `federation_watermarks` table.
- On failure: log + retry next tick (no exponential backoff in v1).

### Inline mode — pipeline step

- New step `steps.FederationPush` registered per configured inline federation.
- Step calls `Pusher.Push(ctx, []Object{current}, edges)` synchronously.
- Step fails job on push error (retriable via job retry policy).

### Dedup at target (pseudocode)

```
for each incoming object o:
  existing = store.Objects().GetByContentHash(o.ContentHash)
  if existing == nil:
    store.Objects().Insert(o)
    store.Edges().InsertBatch(edges for o)
  else:
    skip  // same content already present
```

### Watermark table

```
table federation_watermarks {
  federation_name  TEXT      PRIMARY KEY
  last_synced_at   DATETIME
}
```

### Backup & Recovery

- `dpkms backup` is per-instance; archives the instance DB + `config.yaml`.
- `config.yaml` includes `federations:` entries — federation topology is config, not data.
- **Rebuilding a federation DB from scratch:** restore source instance(s) from backup,
  `dpkms serve` → async/inline sync repopulates target DB automatically.
- No special federation backup needed; downstream DBs are derivable from their sources.
- Housekeeping (`dpkms housekeeping`) — VACUUM, index rebuild, stale job cleanup —
  runs per-instance; no federation-awareness required.

### Phases

- **Phase 1** — `LocalPusher` (SQLite-to-SQLite), async + inline modes, watermark table,
  config validation (cycle detection).
- **Phase 2** — `RemotePusher` (HTTP), token auth per target, retry with backoff,
  `/api/v1/federation/push` bulk endpoint.
- **Phase 3** — Pull support; bidirectional sync; conflict resolution ADR required first.

---

## References

- [ADR-037 – Agent-Aware Adaptive Memory](ADR-037-agent-aware-adaptive-memory.md)
- [ADR-049 – Edges Table as Source of Truth for Mentions](ADR-049-edges-table-for-mentions.md)
- [ADR-063 – Graph-Canonical KnowledgeObject](ADR-063-graph-canonical-knowledge-object.md)
- [topology diagram](ADR-064-federation/topology-v1.mmd)

---
