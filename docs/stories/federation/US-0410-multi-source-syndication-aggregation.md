# US-0410: Multi-Source Syndication Aggregation with Dual Filter Ownership

**System Types:** dpkms (self-hosted)
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Platform Integrators](../../personas/platform-integrators.md)

---

## User Goal

As a ctxt user, I want my local dpkms to aggregate content from two or more remote
dpkms instances via syndication, with two distinct filter-ownership models, so I
can pull team-shared knowledge (where the remote owner controls the share scope)
**and** pull from open-permissive remotes (where I, the local owner, pick what is
relevant).

Critical: syndication is **server-to-server at write/sync time**, not query-time
merge. After syndication, all aggregated content lives in the local store, and
queries hit local only.

---

## Context

This story extends federation push (US-0318/US-0319) into a *pull* aggregation
model with explicit filter ownership. Two modes:

- **Mode A — Remote-defined share scope**: the remote dpkms decides what is
  published (server-side filter on the remote). The local dpkms accepts whatever
  the remote shares. Use case: team-shared knowledge with access control owned
  by the team.
- **Mode B — Local-defined aggregation filter**: the remote dpkms is fully open
  (publishes everything). The local dpkms decides what to aggregate
  (client-side filter). Use case: pulling from a public/permissive remote where
  the local owner curates what is relevant.

Both syndications run concurrently into the same local store. `ctxt find`
operates purely on local content but sees the aggregated superset.

This story is *orthogonal* to rux's remote-daemon work (rux moves live PTY
control across the network; this moves knowledge objects across servers at
sync time).

---

## Acceptance Criteria

- [ ] Given a configured Mode A syndication, when the remote publishes a new
      object matching its server-side scope, then the local dpkms receives and
      stores it within the configured pull interval.
- [ ] Given a configured Mode B syndication with a local filter
      (e.g. `mention=@project.foo`), when the remote publishes objects with
      mixed mentions, then the local dpkms stores only those matching the
      filter.
- [ ] Given both modes active concurrently, when `ctxt find @<some-mention>`
      runs, then it returns matches from local-original + Mode A imports +
      Mode B imports merged transparently (single result set, no
      mode-awareness in the query).
- [ ] Given a remote becomes unreachable, when syndication next runs, then
      partial-progress state persists (watermark not advanced past last
      successful batch) and retry happens on next interval — no data loss.
- [ ] Given a syndicated object is updated upstream, when sync runs, then the
      local copy is updated (idempotency contract: same content-hash → no-op;
      same source-id with newer `updated_at` → upsert).
- [ ] Given a Mode B local filter is changed, when sync next runs, then
      previously-imported objects no longer matching the filter are NOT
      retroactively deleted (filter is at-import-time only); future imports
      respect the new filter.
- [ ] Given a Mode A remote that revokes access mid-stream, when sync runs,
      then the local store retains already-imported objects and logs the
      revocation cleanly.
- [ ] Imported objects carry provenance: remote instance ID, original
      source-id, sync mode (A or B), import timestamp.
- [ ] Two independent remotes publishing the same content-hash dedupe at
      import (one row in local store, provenance tracks both origins).
- [ ] Concurrent syndication workers do not deadlock on the same SQLite WAL
      (lock contention bounded; documented retry policy).

---

## Implementation Notes

### config.yaml block

```yaml
syndications:
  - name: team-shared
    url: https://team.dpkms.internal:8080
    mode: A                       # remote-defined share scope
    interval: 5m
    auth:
      token: ENV.TEAM_DPKMS_TOKEN

  - name: public-feed
    url: https://public.dpkms.example:8080
    mode: B                       # local-defined aggregation filter
    interval: 30m
    filter: "mention=@project.foo OR tag=research"
```

### SyndicationConfig struct (pseudocode)

```
SyndicationTarget:
  Name     string
  URL      string             // remote dpkms HTTP endpoint
  Mode     string             // "A" | "B"
  Interval time.Duration
  Filter   string             // RSQL-ish; required for mode B, ignored for A
  Auth     AuthConfig

SyndicationConfig:
  Targets []SyndicationTarget
```

### Pull worker (pseudocode)

```
func runSyndicationWorker(ctx, target, localDB):
  ticker = time.NewTicker(target.Interval)
  for:
    select:
      case <-ticker.C:
        watermark = localDB.GetSyndicationWatermark(target.Name)
        switch target.Mode:
          case "A":
            // remote's /api/v1/published?since=<watermark> already
            // applies the remote's server-side scope
            objects = remoteFetch(target.URL, watermark, target.Auth)
          case "B":
            // remote's /api/v1/published?since=<watermark> returns
            // everything; client filters locally.
            raw     = remoteFetch(target.URL, watermark, target.Auth)
            objects = clientFilter(raw, target.Filter)

        if len(objects) == 0: continue

        tx = localDB.Begin()
        for obj in objects:
          existing = localDB.Objects().GetByContentHash(obj.ContentHash)
          if existing != nil:
            localDB.Provenance().Add(existing.ID, target.Name, obj.SourceID)
            continue
          localDB.Objects().Upsert(tx, obj)
          localDB.Provenance().Add(tx, obj.ID, target.Name, obj.SourceID)
          for edge in obj.Edges: localDB.Edges().Upsert(tx, edge)
          for m in obj.Mentions: localDB.Mentions().Upsert(tx, m)
        localDB.UpdateSyndicationWatermark(tx, target.Name, now())
        tx.Commit()

      case <-ctx.Done():
        return
```

### Provenance table (pseudocode)

```sql
CREATE TABLE syndication_provenance (
  object_id        TEXT NOT NULL,
  syndication_name TEXT NOT NULL,
  source_id        TEXT NOT NULL,
  imported_at      DATETIME NOT NULL,
  mode             TEXT NOT NULL CHECK (mode IN ('A','B')),
  PRIMARY KEY (object_id, syndication_name, source_id)
);
```

### Mode boundary (key invariant)

- Mode A: filter logic lives **on the remote**; local never sees rejected
  objects. Local cannot widen the scope.
- Mode B: filter logic lives **on the local**; remote sees no filter. Local
  cannot narrow what the remote chooses to publish (remote already publishes
  everything by definition of B).

A remote that wants both modes for different consumers must either run two
endpoints or implement per-token scoping (out of scope here).

---

## E2E Test Checklist

- [ ] Configure Mode A with a remote that publishes 3 objects matching scope;
      verify all 3 land locally within interval; verify objects outside scope
      are not present.
- [ ] Configure Mode B with local filter `mention=@project.foo`; remote
      publishes 5 objects (2 matching, 3 not); verify only 2 land locally.
- [ ] Run Mode A and Mode B concurrently; capture content in local; run
      `ctxt find` — results include local-original + A-imports + B-imports
      with no mode flag visible in the result set.
- [ ] Stop remote mid-batch; verify watermark not advanced past last
      successful commit; restart remote; verify objects re-fetched on next
      tick.
- [ ] Update an object upstream (same source-id, new content); verify local
      copy reflects update after next sync.
- [ ] Two remotes publish the same content-hash; verify single row in local
      store, two rows in syndication_provenance.
- [ ] Change Mode B filter at runtime (config reload); verify previously
      imported objects remain; new imports respect new filter.
- [ ] Mode A remote revokes auth token; verify local logs cleanly, retains
      prior imports, retries on next tick.
- [ ] `ctxt find` performance: no measurable regression vs. local-only after
      adding 10k aggregated objects (queries hit local only, by design).

---

## Out of Scope

- Bidirectional sync (this is pull-only; push is covered by US-0318/US-0319).
- Conflict resolution beyond last-write-wins by `updated_at`.
- Syndication-of-syndication chains (transitive pull). If `local` pulls from
  `team`, and `team` itself pulled from `upstream`, `local` does NOT
  automatically see `upstream`'s objects unless `team` re-publishes them.
- Cryptographic verification of remote-published objects (signing).
- Quota / rate-limit negotiation between local and remote.

---

## Open Questions

- Transport: HTTPS pull (client-initiated, polling) is the obvious default.
  Should we also support webhook push (remote-initiated, lower latency) in
  the same story or split it? Recommend split — pull first.
- Authentication: TOFU (trust-on-first-use) with pinned remote pubkey?
  mTLS? Per-target API tokens (matches US-0318 auth)? Recommend per-target
  bearer tokens initially; document mTLS as follow-up.
- Schedule: cron-style intervals (matches US-0318/19) vs. event-driven
  (remote SSE/webhook)? Pull on interval is the consistent choice for
  Phase 1.
- Filter language for Mode B: reuse RSQL exactly, or a restricted subset?
  Restricted subset (mention, tag, source_id) is safer to start.
- Provenance display: should `ctxt list` / `ctxt open` surface
  syndication origin by default, or only with `--verbose`? Recommend
  `--verbose` to keep default output clean.

---

## Dependencies / Risks

- Depends on: dpkms's mention/entity model (per
  `~/.ops/tracks/ingestion-retrieval-pipelines` mentions registry).
- Aligns with: ingestion-retrieval-pipelines architecture (this is a new
  *write-time* ingestion source for the local store).
- Related but orthogonal: rux remote daemon (story 041 in rux). Both are
  about distributed operation but different surfaces — rux moves live
  control of a process across the network; this moves knowledge objects
  across servers at sync time.
- Risk: a misconfigured Mode B filter pulls everything from a noisy remote
  and bloats local storage. Mitigate with import quota + `ctxt
  syndication preview` dry-run before enabling.
- Risk: Mode A trust model — local accepts whatever remote publishes,
  including potentially low-quality or malicious content. Mitigate with
  per-target trust scoring and provenance-aware reranking.

---

## Related Stories

- [US-0318](./US-0318-configure-federation-targets.md) — push-side
  federation config (this story is the pull-side counterpart)
- [US-0319](./US-0319-async-push-local-merged.md) — async push to local
  merged DB (shares watermark + dedup primitives)
- [US-0322](./US-0322-backup-and-federation-rebuild.md) — backup +
  federation rebuild (provenance helps here)
- [US-0323](./US-0323-dag-federation-chain.md) — DAG federation chain
  (Mode A composes naturally with chained DAG)
- rux story 041 — connect to remote rux daemon (orthogonal: control
  plane vs. knowledge plane)

---

## E2E Tests

- planned: `test/e2e/federation/syndication_test.go::TestSyndication_AggregateMultipleSources`
- planned: `test/e2e/federation/syndication_test.go::TestSyndication_DedupeAcrossSources`
- planned: `test/e2e/federation/syndication_test.go::TestSyndication_PerSourceWatermarks`
