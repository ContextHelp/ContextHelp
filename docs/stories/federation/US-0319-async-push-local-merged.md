---
status: shipped
---

# US-0319: Async Federation Push to Local Merged DB

**System Types:** dpkms (self-hosted)
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want objects I capture in one dpkms instance to appear automatically
in a local merged SQLite database within the configured interval, so I can query across
multiple instances without manual sync steps.

---

## Context

Each dpkms instance runs an async background goroutine per federation target. The goroutine
wakes on `interval`, selects objects updated since `last_synced_at` watermark, and upserts
them into the target SQLite via `LocalPusher`. Dedup is by content-hash — re-pushing the
same object is a no-op. Edges and mentions travel with their objects in the same batch.
Crash recovery: watermark only advances after successful batch commit; next tick retries.

---

## Acceptance Criteria

- [ ] Objects appear in merged DB within configured interval (default 5m)
- [ ] Dedup: re-pushing same content-hash → no duplicate rows in target
- [ ] `federation_watermarks` table updated with `last_synced_at` after each successful batch
- [ ] Worker crash (mid-push) → watermark not advanced → objects re-pushed on next tick
- [ ] Edges associated with pushed objects are upserted at target
- [ ] Mentions associated with pushed objects are upserted at target
- [ ] Zero objects since last watermark → tick is a no-op (no DB writes, no error)
- [ ] Push errors logged with federation name + target URL; worker continues on next tick
- [ ] Goroutine exits cleanly on server shutdown (context cancel respected)

---

## Implementation Notes

### federation_watermarks table (pseudocode)

```sql
CREATE TABLE federation_watermarks (
  federation_name TEXT PRIMARY KEY,
  last_synced_at  DATETIME NOT NULL DEFAULT '1970-01-01T00:00:00Z'
);
```

### LocalPusher logic (pseudocode)

```
LocalPusher.Push(ctx, sourceDB, targetDB, federationName):
  watermark = targetDB.GetWatermark(federationName)
  objects   = sourceDB.Objects().Where(updated_at > watermark).All()
  if len(objects) == 0: return nil

  tx = targetDB.Begin()
  for obj in objects:
    existing = targetDB.Objects().GetByContentHash(obj.ContentHash)
    if existing != nil: continue   // dedup
    targetDB.Objects().Upsert(tx, obj)
    for edge in sourceDB.Edges().ForObject(obj.ID):
      targetDB.Edges().Upsert(tx, edge)
    for mention in sourceDB.Mentions().ForObject(obj.ID):
      targetDB.Mentions().Upsert(tx, mention)
  targetDB.UpdateWatermark(tx, federationName, now())
  tx.Commit()
```

### Async worker goroutine (pseudocode)

```
func runFederationWorker(ctx, target, sourceDB, targetDB):
  ticker = time.NewTicker(target.Interval)
  for:
    select:
      case <-ticker.C:
        err = LocalPusher.Push(ctx, sourceDB, targetDB, target.Name)
        if err: log.Error("federation push failed", target.Name, err)
      case <-ctx.Done():
        return
```

### Startup wiring (pseudocode)

```
for target in cfg.Federation.Targets:
  if target.SyncMode == "async":
    targetDB = sqlite.Open(target.URL)
    go runFederationWorker(serverCtx, target, mainDB, targetDB)
```

---

## E2E Test Checklist

- [ ] Capture object in source instance; wait interval; query merged DB → object present
- [ ] Capture same object twice (same content-hash) → only one row in merged DB
- [ ] Verify `federation_watermarks` row for target has `last_synced_at` > epoch after push
- [ ] Kill worker mid-push (cancel context); restart; object appears in merged DB on next tick
- [ ] Capture object with edges; wait interval; query merged DB edges → edges present
- [ ] Capture object with mentions; wait interval; query merged DB mentions → mentions present
- [ ] No objects since last sync → watermark unchanged; no spurious DB writes (check WAL)
- [ ] Server shutdown (SIGTERM) → federation goroutines exit within 5s

---

## Related Stories

- [US-0318](./US-0318-configure-federation-targets.md) — configure federation targets
- [US-0320](./US-0320-inline-push-on-capture.md) — inline sync mode (Phase 2)
- [US-0321](./US-0321-http-federation-push.md) — remote HTTP target push (Phase 2)
- [US-0322](./US-0322-federation-topology-api.md) — topology API (Phase 2)
- [US-0323](./US-0323-federation-status-monitoring.md) — federation push status monitoring

---

## E2E Tests

- `test/e2e/federation/async_push_test.go::TestFederation_AsyncPush_ObjectAppearsAtMergedDB`
- `test/e2e/federation/async_push_test.go::TestFederation_AsyncPush_DedupeByContentHash`
- `test/e2e/federation/async_push_test.go::TestFederation_AsyncPush_WatermarkAdvances`
- `test/e2e/federation/async_push_test.go::TestFederation_AsyncPush_CrashRecovery`
- `test/e2e/federation/async_push_test.go::TestFederation_AsyncPush_EdgesPushed`
- `test/e2e/federation/async_push_test.go::TestFederation_AsyncPush_MentionsPushed`
- `test/e2e/federation/async_push_test.go::TestFederation_AsyncPush_NoChangesNoOp`
- `test/e2e/federation/async_push_test.go::TestFederation_AsyncPush_GracefulShutdown`
