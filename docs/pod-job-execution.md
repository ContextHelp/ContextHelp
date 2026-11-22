# Pod-Backed Ingestion Job Execution

`dpkms serve` can optionally offload eligible Stage 1 ingestion pipeline jobs to ephemeral pods via the standalone `pod` CLI.

This capability is **strictly opt-in**. The default `jobs.executor` is `local`, and when `local` (or unset):

- `pod` is never probed on `PATH`.
- No pod code runs and every queued job executes in-process unchanged.

When `jobs.executor: pod` is configured and `pod` is not installed on `PATH`, `dpkms serve` fails fast at startup with exit code `70` (`PREREQUISITE`) unless you explicitly enable `jobs.pod.fallback_local: true`.

## How It Works

Each ingestion job executes in two stages:

1. **Stage 1 (offloadable draft computation):** For allowlisted topics, `dpkms` creates an ephemeral pod (`pod create`), uploads a `0600` job request JSON (`pod upload`), runs `dpkms worker run-draft` inside the pod (`pod exec`), parses the returned `KnowledgeObject` draft JSON, and always destroys the pod (`pod destroy --force`) on success, failure, timeout, or cancellation.
2. **Stage 2 (host persistence):** The host `dpkms` process remains the single authoritative writer to SQLite/PostgreSQL, blob storage (`ExternalizeBinary`), vector storage, mention edges, item fan-out, and queue settlement. Pods never open the host database.

Failure mapping preserves existing queue semantics:

- Permanent pipeline errors (bad payload encoding, unknown pipeline, missing `dpkms` binary in the pod image) mark the job failed with zero retries.
- Transient step errors, pod creation/exec failures, or pod timeouts requeue the job up to `jobs.max_retries`.
- A lost pod becomes a failed/requeued job rather than a stuck row.

## Configuration

Configure `jobs.executor` and `jobs.pod.*` in your **user config** (`~/.config/contexthelp/dpkms.yaml`).

> **Security boundary:** Project-layer `.contexthelp/dpkms.yaml` files are refused at startup with exit code `2` if they set `jobs.executor` or `jobs.pod.*`.

```yaml
server:
  workers: 8                      # set >= jobs.pod.max_inflight to saturate all pod slots

jobs:
  executor: pod                   # local (default) | pod
  stale_timeout: 30m
  pod:
    provider: ""                  # optional; empty defers to pod's own default provider
    image: ctxt-pod-worker:local  # image with `dpkms` on PATH
    dpkms_bin: dpkms
    max_inflight: 4               # max concurrent ephemeral pods (default: 4)
    max_pods_per_hour: 120        # cost ceiling per sliding hour (0 = unlimited)
    max_pod_duration: 5m          # hard per-job lifetime (must be < jobs.stale_timeout)
    fallback_local: false         # true warns and runs locally if pod is absent
    topics:
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

## Topic Routing

| Job type | Default routing under `jobs.executor: pod` | Notes |
|---|---|---|
| `ingest:document`, `ingest:image`, `ingest:audio`, `ingest:video` | **Pod** | Highest payoff: long compute/media duration easily amortizes pod startup |
| `ingest:url`, `ingest:feed`, `ingest:feed_item`, `ingest:github_release`, `ingest:code` | **Pod** | Offloads burst fan-out (`:nofanout` suffix is matched automatically) |
| `ingest:text` | **Local** | Sub-second heuristic text stays local unless explicitly added to `jobs.pod.topics` |
| `object:reprocess`, `projection:reproject`, `embeddings:migrate` | **Local** | Lease-tracked maintenance tasks always stay local |

## Concurrency, Cost Controls, and Break-Even Guidance

Benchmarks against the real `pod` CLI (local Docker worker image + SQLite `WorkerPool`) establish clear break-even rules:

- **Ephemeral pod overhead:** Local Docker pods spend ~630 ms p50 (~715 ms mean) per job across `create`, `upload`, `exec`, and `destroy`. Remote ephemeral sandboxes (E2B, Modal, Runloop) add ~2–5 s startup overhead.
- **Keep fast heuristic jobs local:** Local `WorkerPool` sustains ~88–366 jobs/s on short text and 100 KB markdown parses, vs ~7.6–9.5 jobs/s on per-job Docker pods (`w=8`). Do not add `ingest:text` to `jobs.pod.topics`, and remove `ingest:code` if your code pipelines are purely local heuristic parsing.
- **Where pod fan-out wins:**
  - **Local Docker:** Breaks even around **~1.5–2 s/job** when pod fan-out width is 2× local workers (`max_inflight: 8` vs 4 local workers) and delivers **1.25×–1.7×** faster wall-clock at **≥ 3 s/job** (or isolation value at equal width for untrusted PDF/media parsers).
  - **Remote burst sandboxes:** Break even around **~5–10 s/job** (OCR, audio transcription, video frame analysis, multi-page PDF parsing, slow LLM/vision pipelines) or bursts exceeding local CPU/RAM.
- **Recommended concurrency cap:** Keep `jobs.pod.max_inflight: 4` as the default (safe for SQLite Stage 2 commit bursts and cloud spend). Raise to `8` (or `16` on PostgreSQL + remote sandboxes) for heavy media/OCR/LLM queues, and always set `server.workers >= jobs.pod.max_inflight` so worker goroutines can saturate every pod slot.
- **Cost ceiling:** `jobs.pod.max_pods_per_hour` (default `120`) bounds runaway retries or misconfigured feed bursts; set to `0` to disable.
- **Stale-job guard:** `jobs.pod.max_pod_duration` (default `5m`) must stay strictly below `jobs.stale_timeout` (default `30m`) so timed-out pods fail and clean up cleanly before stale recovery runs.

## Related

- [`pod-runner.md`](pod-runner.md) — running long-lived `ctxd` and `dpkms` daemons on an adopted pod
- [`ctxt/configuration.md`](ctxt/configuration.md) — configuration layers and validation
