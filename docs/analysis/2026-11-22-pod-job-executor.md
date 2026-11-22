# Pod-Backed Ingestion Job Executor Design

**Date:** 2026-11-22
**Depends on:** `docs/analysis/2026-11-22-job-pipeline-offload-map.md`, `docs/analysis/2026-10-17-pod-runner.md`

## 1. Goals and Non-Goals

Two payoffs drive pod-backed job execution:

1. **Larger flux:** Run `N` ephemeral pods concurrently during ingestion bursts (feed fan-outs, document/media batches, URL scrapes) without saturating the `dpkms` host CPU or memory.
2. **Shorter wall-clock:** Parallel fan-out across isolated pods for compute- or I/O-heavy pipelines (`ingest:document`, `ingest:image`, `ingest:audio`, `ingest:video`, `ingest:url`, `ingest:feed_item`, `ingest:code`, `ingest:github_release`).

**Invariants (shared with the pod daemon runner):**

- **Strictly opt-in:** `jobs.executor` defaults to `local`. When `local`, `pod` is never probed via `exec.LookPath`, no pod code runs, and behavior is byte-for-byte identical to today.
- **Shell-out boundary only:** `ctxt` / `dpkms` never import `pod` packages and never invoke `docker` or provider APIs directly. All lifecycle calls flow through `internal/podrun`.
- **Single authoritative DB writer:** Pods never touch local SQLite or host state. The host `dpkms` performs all Stage 2 storage, blob externalization, vector writes, mention edges, fan-out enqueues, and queue settlement.
- **No silent fallback when opted in:** When `jobs.executor: pod` and `pod` is missing on `PATH`, startup fails with exit code `70` (`PREREQUISITE`) unless the operator explicitly sets `jobs.pod.fallback_local: true`.
- **Project-layer refusal:** `.contexthelp/dpkms.yaml` may not set `jobs.executor` or `jobs.pod.*` (refused at startup with exit code `2`), preventing untrusted repositories from spawning billed compute pods or choosing arbitrary container images.

## 2. Execution Unit and Ephemeral Lifecycle

### Worker Interface Seam (`internal/jobs/worker.go`)

`WorkerPool.process` splits cleanly at Stage 1 (`processWithHops`):

```go
// PipelineExecutor computes a KnowledgeObject draft from an acquired ingest job.
// Implementations must not write to the job queue or object store; WorkerPool
// performs Stage 2 persistence (binary externalization, dedup, create, vectors,
// mention edges, item fan-out, and queue settlement) on the returned draft.
type PipelineExecutor interface {
    Execute(ctx context.Context, job *storage.Job) (*storage.KnowledgeObject, error)
}
```

- When `WorkerPool.executor` is `nil` (default `jobs.executor: local`), `WorkerPool.process` runs the existing in-process `processWithHops` unchanged.
- When `jobs.executor: pod`, `dpkms serve` installs `podexec.NewRouter(localExec, podExec, allowlist)`.
  - Offloadable topics route to `podExec`.
  - All other ingest topics and all task handlers (`object:reprocess`, `projection:reproject`, `embeddings:migrate`) run on `localExec` unchanged.

### Ephemeral Pod Lifecycle (Per-Job)

For each offloaded job, `podexec.Executor.Execute(ctx, job)` executes:

```
acquire semaphore (max_inflight) & hourly budget (max_pods_per_hour)
  │
  ▼
pod create <ctxt-job-id> [--provider <p>] [--image <img>] --format json
  │  (registers `defer pod destroy <name> --force --note "ctxt job cleanup"`
  │   using context.WithoutCancel + 30s timeout)
  ▼
write 0600 DraftJobRequest JSON → pod upload <name> <local> /tmp/ctxt-job-req.json
  │
  ▼
pod exec <name> -- <dpkms_bin> worker run-draft --input /tmp/ctxt-job-req.json
  │  (bounded by max_pod_duration)
  ▼
parse DraftJobResult JSON from stdout
  │
  ▼
defer pod destroy <name> --force --note "ctxt job cleanup"
  │
  ▼
return *storage.KnowledgeObject to WorkerPool.process (host Stage 2)
```

- **Why upload a request file instead of CLI args?** Job payloads (base64 PDFs/images, long markdown/HTML) can exceed `ARG_MAX` and would expose content in host/pod `ps` listings. Uploading a `0600` JSON file via `pod upload` avoids both issues.
- **Stateless pod-side verb (`dpkms worker run-draft`):** Reads `DraftJobRequest{Job, MaxHops}` from `--input <file>` (or stdin), builds the builtin pipeline registry without opening SQLite, runs the Stage 1 pipeline + hops + ambient metadata + content hash, and writes `DraftJobResult{Draft, Permanent, Error}` JSON to stdout.
- **Host Stage 2 externalization:** Immediately after `Execute` returns a draft, `WorkerPool.process` runs `steps.ExternalizeBinary(ctx, p.store.Blobs(), draft)` before `Objects().Create`, so binary payloads processed in a stateless pod still land in the host's configured blob store (local or S3/Garage).

## 3. Adapter Fit

Provider selection belongs to `pod`, not `ctxt`:

| Workload environment | `pod` adapter | Notes |
|---|---|---|
| Local containerized runs & benchmarks | `docker` | Fast local container isolation using an image containing `dpkms` (`CMD ["sleep", "infinity"]`). |
| Remote burst compute | E2B, Modal, Runloop, Sprites, SkyPilot | Selected via `pod`'s own default provider config, or optionally pinned with `jobs.pod.provider`. |

`ctxt` only passes `--provider` and `--image` to `pod create` when explicitly set in `jobs.pod.*`; otherwise `pod`'s configured defaults apply.

## 4. Concurrency Control, Backpressure, and Cost Ceiling

| Control | Config key | Default | Semantics |
|---|---|---|---|
| **Max in-flight pods** | `jobs.pod.max_inflight` | `4` | Buffered semaphore bounding simultaneous live pods. Workers block waiting for a slot (honoring `ctx.Done()`), applying natural backpressure into the job queue. |
| **Hourly pod ceiling** | `jobs.pod.max_pods_per_hour` | `120` | Sliding 1-hour creation window. When reached, returns a transient backpressure error so runaway retries or misconfigured feeds cannot cause unbounded cloud spend (`0` disables the ceiling). |
| **Per-pod max lifetime** | `jobs.pod.max_pod_duration` | `5m` | Hard context deadline covering `create + upload + exec`. Must be strictly less than `jobs.stale_timeout` so a timed-out pod always fails and cleans up before stale recovery fires. |
| **Topic allowlist** | `jobs.pod.topics` | `ingest:document`, `ingest:image`, `ingest:audio`, `ingest:video`, `ingest:url`, `ingest:code`, `ingest:feed`, `ingest:feed_item`, `ingest:github_release` | Only jobs whose base type (stripping `:nofanout`) matches this list route to pods. `ingest:text` stays local by default because short heuristic text jobs sit below pod startup break-even; operators may add `ingest:text` explicitly. |

## 5. Result Return and Failure/Retry Mapping

| Failure condition | Returned error | `WorkerPool` queue effect | Teardown |
|---|---|---|---|
| `DraftJobResult.Permanent == true` (bad base64, unknown pipeline, invalid input) | `*pipeline.PermanentError` | `markFailed` (0 retries) | `defer pod destroy` |
| Pod exec exit `127` (`dpkms` missing in image) | `*pipeline.PermanentError` | `markFailed` (stops retry storm on broken image) | `defer pod destroy` |
| `DraftJobResult.Error != ""` (transient step/fetch error) | Standard error | `retryOrFail` → `Requeue` up to `max_retries` | `defer pod destroy` |
| `pod create` / `pod upload` / `pod exec` error, timeout, or bad JSON | Standard error | `retryOrFail` → `Requeue` up to `max_retries` | `defer pod destroy` (if created) |
| Host context cancelled (shutdown drain) | `ctx.Err()` | Unsettled / requeued on restart | `defer pod destroy` with detached 30s cleanup ctx |
| Host crashes abruptly mid-pod | Job remains `running` | `RecoverStale` requeues after `stale_timeout` | Orphan pod visible in `pod ls` (`ctxt-job-*` prefix) |

A lost pod therefore always surfaces as a clean failed/requeued job — never a permanently stuck row.

## 6. Configuration Schema

```yaml
jobs:
  executor: local                 # local (default) | pod
  poll_interval: 500ms
  stale_timeout: 30m
  max_retries: 3
  max_hops: 5
  drain_timeout: 30s
  pod:
    provider: ""                  # optional; empty defers to pod's configured default
    image: ""                     # optional; container/sandbox image with dpkms installed
    dpkms_bin: dpkms              # dpkms binary on the pod PATH
    max_inflight: 4               # max concurrent ephemeral pods
    max_pods_per_hour: 120        # cost ceiling (0 = unlimited)
    max_pod_duration: 5m          # hard per-job pod lifetime (< jobs.stale_timeout)
    fallback_local: false         # true allows local fallback if pod is absent
    topics:                       # job types eligible for pod offload
      - ingest:document
      - ingest:image
      - ingest:audio
      - ingest:video
      - ingest:url
      - ingest:code
      - ingest:feed
      - ingest:feed_item
      - ingest:github_release
    timeouts:
      probe: 10s
      query: 15s
      exec: 90s
      lifecycle: 5m
```

Validation rules:

- `jobs.executor` must be `""`, `"local"`, or `"pod"`.
- When `jobs.executor == "pod"`: `max_inflight >= 1`, `max_pods_per_hour >= 0`, `max_pod_duration > 0` (and `< stale_timeout` when `stale_timeout > 0`), P9 token validation on `provider`, `image`, `dpkms_bin`, and non-empty `topics` entries.
- `.contexthelp/dpkms.yaml` setting `jobs.executor` or `jobs.pod` is refused at `dpkms serve` startup with exit code `2`.

## 7. Testing Strategy

1. **L1 (Unit):** Config defaults, strict YAML decode, validation errors, topic matching (including `:nofanout`), P9 token rejection, sliding-window `max_pods_per_hour` rate limiter, and `DraftJobRequest` / `DraftJobResult` round-trip JSON.
2. **L2 (State-machine with fake `podrun.Runner`):**
   - Full `create → upload → exec → destroy` happy path returning a `KnowledgeObject` persisted through a real SQLite `WorkerPool`.
   - Guaranteed `pod destroy` on exec error, timeout, permanent error, and context cancellation.
   - Concurrency cap enforcement (`max_inflight: 2` under 8 concurrent jobs verifies peak in-flight `<= 2`).
   - `PermanentError` marks job failed with zero retries; transient pod death requeues up to `max_retries`.
   - Topic routing: non-allowlisted topic and task jobs run locally without invoking `pod`.
   - Local-default guarantee: `jobs.executor: local` never invokes `podrun.Detect` even when a trap `pod` binary sits on `PATH`.
3. **L3 (`xrr` exec cassettes):** Live-recorded cassettes under `internal/jobs/podexec/cassettes/podcli/` exercising `capabilities`, `create`, `upload`, `exec`, and `destroy` through `podrun.Client` against the real `pod` CLI, replayed hermetically in default test runs.
4. **L4 (End-to-end benchmark on Docker):** Compile Linux `dpkms` into a local worker image and benchmark local `WorkerPool` vs real `pod create/upload/exec/destroy` across job durations and fan-out widths to measure exact startup overhead and break-even point.
