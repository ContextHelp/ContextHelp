---
status: paper
---

# US-0320: Inline Federation Push During Ingest

**System Types:** dpkms (self-hosted)
**Personas:** [Platform Integrators](../../personas/platform-integrators.md)

---

## User Goal

As a platform integrator, I want objects pushed to a federation target
synchronously as part of the pipeline job, so the remote instance has the
object available immediately after ingestion completes — no lag, no separate
tick.

---

## Context

Federation supports two push modes: async (background worker, eventual) and
inline (synchronous, within the job). Inline mode is critical for integrators
who need the remote to reflect new objects before returning control to the
caller (e.g., a write-through pattern). Configured via `sync_mode: inline`
per target entry in `config.yaml`. The pipeline scheduler registers a
`FederationPush` step for each inline target; the step runs as part of the
normal job DAG. Failure propagates: if push fails, job fails and is eligible
for retry under the normal job retry policy.

---

## Acceptance Criteria

- [ ] Inline push fires during job execution, not after job completes
- [ ] Push failure causes job to fail (error surfaced on job record)
- [ ] Failed job is retried under normal job retry policy (count, backoff)
- [ ] Push success → object available at target immediately after job completes
- [ ] `sync_mode: inline` in config activates this path for that target
- [ ] Multiple inline targets: all must succeed for job to succeed
- [ ] Partial success (some targets ok, one fails) → job fails; no partial commit
- [ ] Inline push step appears in job step trace with target label
- [ ] `sync_mode: async` (or absent) does not activate inline path

---

## Implementation Notes

### Config

```
federation:
  targets:
    - id: mirror-west
      url: http://10.0.0.5:8080
      sync_mode: inline        # triggers FederationPush step in job DAG
    - id: mirror-east
      url: http://10.0.0.6:8080
      sync_mode: inline
    - id: archive
      url: http://archive.internal:8080
      sync_mode: async         # background worker; not inline
```

### Pipeline Registration

```
// on job start, scheduler injects FederationPush steps for inline targets
for each target in config.federation.targets where sync_mode == "inline":
    job.steps.append(FederationPush{target: target})
```

### FederationPush Step

```
step FederationPush(target):
    obj = store.get(job.object_id)
    resp = http.POST(target.url + "/api/v1/objects", obj)
    if resp.status != 201 && resp.status != 200:
        return StepError{
            msg: "federation push failed: " + resp.status,
            retriable: true,
        }
    update federation_watermarks(target.id, obj.id, obj.updated_at)
    return StepOK
```

### Job Failure Propagation

```
// job runner: if any step returns StepError, mark job failed
// retry scheduler picks it up per retry policy
// all inline steps must return StepOK for job to complete
```

---

## E2E Test Checklist

- [ ] Configure one inline target; ingest object; verify object at target
      before next scheduler tick (i.e., synchronously)
- [ ] Configure two inline targets; kill one; ingest; verify job fails
- [ ] Retry failed job after restoring target; verify both targets receive object
- [ ] Verify `federation_watermarks` updated at source after successful push
- [ ] Verify job step trace includes `FederationPush[mirror-west]` entry
- [ ] Configure `sync_mode: async` target; ingest; verify job succeeds even if
      async target is unreachable (push not in job critical path)
- [ ] Confirm object available at inline target immediately after `ctxt analyze`
      returns (no poll/wait required)

---

## Related Stories

- [US-0318](./US-0318-configure-federation-target.md) — configure federation target
- [US-0319](./US-0319-async-push.md) — async background push
- [US-0322](./US-0322-backup-and-rebuild.md) — backup/restore per instance
- [US-0323](./US-0323-dag-chain.md) — DAG chain across instances

---

## E2E Tests

- planned: `test/e2e/federation/inline_push_test.go::TestInlinePush_PushOnIngest`
- planned: `test/e2e/federation/inline_push_test.go::TestInlinePush_FailureFallsBackAsync`
- planned: `test/e2e/federation/inline_push_test.go::TestInlinePush_RespectsTargetPause`
- planned: `test/e2e/federation/inline_push_test.go::TestInlinePush_EdgesIncluded`
