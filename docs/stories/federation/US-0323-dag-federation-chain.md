---
status: paper
---

# US-0323: DAG Federation Chain Push

**System Types:** dpkms (self-hosted)
**Personas:** [Platform Integrators](../../personas/platform-integrators.md)

---

## User Goal

As a platform integrator, I want objects to flow automatically through a
configured DAG (instance → local-merged → team-server → org-archive) so that
every tier of the hierarchy receives all relevant objects without manual
intervention.

---

## Context

Federation in dPKMS is a push-only DAG (ADR-064). Each instance knows only its
direct downstream targets; no global topology awareness is required or present.
Intermediate nodes (local-merged, team-server) are themselves dPKMS instances
with their own `config.yaml` specifying further downstream targets. The final
node (org-archive) has no downstream targets configured. Cycle detection runs at
startup (DFS over configured targets); a cycle causes immediate startup failure.
Dedup at each hop is content-hash based — idempotent push means the same object
can be pushed multiple times safely. Watermarks advance independently per
federation name within each instance.

---

## Acceptance Criteria

- [ ] objects captured at source instance appear at local-merged, then
  team-server, then org-archive — in that sequence
- [ ] each hop deduplicates by content-hash; re-pushing an existing object is a
  no-op at the target
- [ ] `federation_watermarks` table at each node advances independently (source
  watermark != local-merged watermark != team-server watermark)
- [ ] cycle in DAG (e.g. A→B→A) detected at startup via DFS; instance refuses
  to start with log: `federation: cycle detected: A→B→A`
- [ ] intermediate nodes (local-merged, team-server) push to their own
  configured downstream targets after receiving objects
- [ ] final node (org-archive) has empty `federations:` block; no push
  attempted
- [ ] no special "federation mode" flag; all instances run identical binary;
  only `config.yaml` differs
- [ ] LocalPusher (SQLite-to-SQLite) used for file-path targets; RemotePusher
  (HTTP POST) used for URL targets

---

## Implementation Notes

### DAG validation at startup

```
validateDAG(config):
  visited = {}
  for each root in config.federations:
    dfs(root, visited, path=[])

dfs(node, visited, path):
  if node in path:
    fatal("federation: cycle detected: " + path.join("→"))
  if node in visited: return
  visited.add(node)
  for each child in node.targets:
    dfs(child, visited, path + [node])
```

### Push loop (per federation entry)

```
pusher(federation):
  loop every federation.interval:
    watermark = db.getWatermark(federation.name)
    objects = db.getObjectsAfter(watermark)
    for obj in objects:
      target.push(obj)           # no-op if content-hash exists at target
      db.advanceWatermark(federation.name, obj.id)
```

### Content-hash dedup at target

```
receivePush(obj):
  if db.existsByHash(obj.content_hash): return  # idempotent
  db.insert(obj)
  triggerOwnFederationPush(obj)   # intermediate node: push downstream
```

### Config: 3-hop chain

```yaml
# source instance
federations:
  - name: local-merged
    target: /var/dpkms/merged/db.sqlite
    mode: async
    interval: 10s

# local-merged instance
federations:
  - name: team-server
    target: http://team.internal:8080
    mode: async
    interval: 30s

# team-server instance
federations:
  - name: org-archive
    target: http://archive.internal:8080
    mode: async
    interval: 60s

# org-archive instance
federations: []   # terminal node
```

### Watermark table (per instance)

```
federation_watermarks:
  federation_name  TEXT PK
  last_object_id   INTEGER
  last_synced_at   DATETIME
```

---

## E2E Test Checklist

- [ ] configure 3-hop chain: source → local-merged → team-server → org-archive
- [ ] capture an object at source; note its content-hash
- [ ] wait source→local-merged interval; verify object present in local-merged DB
- [ ] wait local-merged→team-server interval; verify object present in
  team-server DB
- [ ] wait team-server→org-archive interval; verify object present in org-archive
  DB
- [ ] push same object again to source; verify no duplicate at any downstream
  (count = 1 at each hop)
- [ ] verify `federation_watermarks` row exists at each node with distinct values
- [ ] introduce cycle in config (A→B→A); start instance A; verify startup fails
  with cycle error in logs
- [ ] remove cycle; restart; verify normal operation resumes
- [ ] confirm org-archive makes no outbound push (no `federations:` entries;
  no pusher goroutines in logs)

---

## Related Stories

- [US-0318](./US-0318-configure-federation.md) — configure federation topology
- [US-0319](./US-0319-async-federation-push.md) — async push goroutine lifecycle
- [US-0320](./US-0320-inline-federation-push.md) — inline push on ingest
- [US-0321](./US-0321-federation-lifecycle.md) — serve startup / shutdown
- [US-0322](./US-0322-backup-and-federation-rebuild.md) — backup + federation
  rebuild after restore

---

## E2E Tests

- planned: `test/e2e/federation/dag_chain_test.go::TestDAGChain_3HopPropagation`
- planned: `test/e2e/federation/dag_chain_test.go::TestDAGChain_BranchingFanOut`
- planned: `test/e2e/federation/dag_chain_test.go::TestDAGChain_DiamondMergeDedupe`
- planned: `test/e2e/federation/dag_chain_test.go::TestDAGChain_BackpressureUnderLoad`
