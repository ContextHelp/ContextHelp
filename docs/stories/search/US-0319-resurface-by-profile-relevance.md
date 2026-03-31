# Story: Resurface Knowledge Objects by Profile Relevance

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want the system to proactively surface knowledge objects most
relevant to my active focus profile so I rediscover useful context without having to
search for it manually.

---

## Context

Important knowledge gets buried over time. The resurfacing engine scores all objects
against the active profile's entity overlap, tag overlap, and recency decay, then
presents the top candidates. Users can refresh the queue on demand, dismiss entries
they have acted on, and configure scoring thresholds in config.

---

## Acceptance Criteria

- [ ] `ctxt resurface` shows top candidates for the active profile (table: entry ID,
      object ID, score, reason)
- [ ] `ctxt resurface --limit N` overrides the configured max items
- [ ] `ctxt resurface --min-score X` overrides the configured score threshold
- [ ] `ctxt resurface refresh` re-scores all objects and updates the queue
- [ ] `ctxt resurface dismiss <entry-id>` removes an entry from the queue
- [ ] `ctxt resurface --output json` returns structured JSON
- [ ] No active profile → helpful message with setup instructions
- [ ] Empty queue → `No resurfacing candidates.` message
- [ ] Config (`resurfacing.enabled`, `max_items`, `min_score`, `run_interval`) controls
      defaults

---

## Spec Reference

Governed by [ADR-016 – Just-In-Time Surfacing](../../decisions/ADR-016-just-in-time-surfacing.md).

**Implemented (this story):** entity overlap, tag overlap, recency decay.

**Deferred (future stories):** graph proximity score, past-interaction score,
profile-boost score, active/ambient/review surfacing modes, snooze workflow.

---

## Implementation Notes

### CLI surface

```
ctxt resurface                        # show top candidates
ctxt resurface --limit 5
ctxt resurface --min-score 0.6
ctxt resurface refresh                # re-score queue
ctxt resurface dismiss <entry-id>     # dismiss one entry
ctxt resurface --output json
```

### Scoring (`internal/resurfacing`)

- `ResurfacingJob.RunOnce(ctx, profileName, profile)` — scores objects, writes queue
- Score components: entity overlap, tag overlap, recency decay
- `ResurfacingConfig` fields: `Enabled`, `MaxItems`, `MinScore`, `RunInterval`

### Storage

- `ResurfacingQueueStore` — SQLite + Postgres implementations
- `Service.ListResurfacing(ctx, profile, limit, minScore)`
- `Service.MarkResurfaced(ctx, entryID)` — marks as shown (sets surfaced_at)
- `Service.DismissResurfacing(ctx, entryID)` — removes from queue

### Worker integration

- `dpkms serve` runs resurfacing on `run_interval` when enabled

---

## E2E Checklist

- [ ] Ingest 3+ objects; set default profile; run `ctxt resurface refresh`
- [ ] `ctxt resurface` shows ranked candidates with scores and reasons
- [ ] `ctxt resurface --limit 1` returns exactly 1 entry
- [ ] `ctxt resurface dismiss <id>` removes entry; subsequent `resurface` excludes it
- [ ] `ctxt resurface --output json` returns `{"profile":…,"items":[…],"total":…}`
- [ ] No profile set → tip printed, no crash

---

## Related Stories

- US-0318 — Set reminder on knowledge object
- US-0020 — Apply focus profile to search
- US-0055 — Search history and recommendations
