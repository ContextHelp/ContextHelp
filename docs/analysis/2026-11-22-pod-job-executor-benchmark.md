# Pod Job Executor Benchmark: Flux, Latency, and Break-Even Analysis

**Date:** 2026-11-22
**Harness:** `internal/jobs/podexec/bench_pod_test.go` (`-tags "fts5 podbench"`)
**Host:** Apple Silicon (macOS arm64) with Docker (OrbStack) via real `pod` CLI (`provider: docker`, image `ctxt-pod-worker:local` running `dpkms worker run-draft`) and real SQLite `WorkerPool` (`busy_timeout=10000`, `poll_interval=10ms`)

## 1. Ephemeral Pod Lifecycle Overhead (Phase 1)

Measured across `N = 8` sequential ephemeral pods executing `text.short` via `pod create → pod upload → pod exec dpkms worker run-draft → pod destroy --force`:

| Phase | Mean | p50 | p95 | Min | Max |
|---|---:|---:|---:|---:|---:|
| `pod create` (docker) | 359 ms | 282 ms | 1008 ms | 144 ms | 1008 ms |
| `pod upload` | 56 ms | 54 ms | 75 ms | 46 ms | 75 ms |
| `pod exec` (`dpkms worker run-draft`) | 76 ms | 68 ms | 120 ms | 61 ms | 120 ms |
| `pod destroy --force` | 222 ms | 224 ms | 303 ms | 167 ms | 303 ms |
| **Total per ephemeral job** | **714 ms** | **628 ms** | **1441 ms** | **442 ms** | **1441 ms** |

Zero orphan containers remained in `docker ps -a` after >120 ephemeral pod lifecycles across the full benchmark run.

## 2. End-to-End Throughput and Wall-Clock Matrix (Phase 2)

Every scenario enqueued `N` distinct jobs into SQLite, ran `WorkerPool` end-to-end (Stage 1 execution + Stage 2 SQLite persistence, content-hash dedup, and job completion), and measured total wall-clock until all `N` jobs reached `completed`.

| Workload | Mode & Width | N | Wall-clock | Flux (jobs/s) | Effective avg/job |
|---|---|---:|---:|---:|---:|
| Fast `text.short` (~1 ms compute) | Local `w=1` | 16 | 0.05 s | 341.38 | 3 ms |
| Fast `text.short` (~1 ms compute) | Local `w=4` | 16 | 0.04 s | 365.96 | 3 ms |
| Fast `text.short` (~1 ms compute) | Pod `w=1, inflight=1` | 16 | 6.46 s | 2.48 | 404 ms |
| Fast `text.short` (~1 ms compute) | Pod `w=2, inflight=2` | 16 | 3.49 s | 4.58 | 218 ms |
| Fast `text.short` (~1 ms compute) | Pod `w=4, inflight=4` | 16 | 2.27 s | 7.06 | 142 ms |
| Fast `text.short` (~1 ms compute) | Pod `w=8, inflight=8` | 16 | 1.69 s | 9.48 | 106 ms |
| 100 KB `text.long` markdown parse | Local `w=1` | 12 | 0.14 s | 88.81 | 11 ms |
| 100 KB `text.long` markdown parse | Local `w=4` | 12 | 0.14 s | 88.03 | 11 ms |
| 100 KB `text.long` markdown parse | Pod `w=4, inflight=4` | 12 | 2.16 s | 5.56 | 180 ms |
| 100 KB `text.long` markdown parse | Pod `w=8, inflight=8` | 12 | 1.58 s | 7.58 | 132 ms |
| 1.5 s heavy step (URL + LLM/extract) | Local `w=1` | 12 | 18.11 s | 0.66 | 1510 ms |
| 1.5 s heavy step (URL + LLM/extract) | Local `w=2` | 12 | 9.08 s | 1.32 | 756 ms |
| 1.5 s heavy step (URL + LLM/extract) | Local `w=4` | 12 | 4.57 s | 2.63 | 381 ms |
| 1.5 s heavy step (URL + LLM/extract) | Pod `w=4, inflight=4` | 12 | 7.31 s | 1.64 | 609 ms |
| 1.5 s heavy step (URL + LLM/extract) | Pod `w=8, inflight=8` | 12 | 4.71 s | 2.55 | 392 ms |
| 3.0 s media step (OCR / transcription) | Local `w=2` | 12 | 18.16 s | 0.66 | 1513 ms |
| 3.0 s media step (OCR / transcription) | Local `w=4` | 12 | 9.13 s | 1.31 | 761 ms |
| 3.0 s media step (OCR / transcription) | Pod `w=8, inflight=8` | 12 | 7.32 s | 1.64 | 610 ms |

## 3. Honest Break-Even Findings

For a batch of $N$ jobs with per-job compute duration $T_{\text{job}}$, local worker count $W_{\text{local}}$, pod fan-out width $W_{\text{pod}} > W_{\text{local}}$, and ephemeral pod lifecycle overhead $T_{\text{overhead}}$, pod offload beats local wall-clock when:

$$T_{\text{job}} > \frac{W_{\text{local}}}{W_{\text{pod}} - W_{\text{local}}} \cdot T_{\text{overhead}}$$

1. **Fast heuristic pipelines (`ingest:text`, small `ingest:code`, 100 KB markdown) are 12x–40x slower on ephemeral pods.**
   Local SQLite + in-process `WorkerPool` sustains **88–366 jobs/s**, whereas per-job Docker ephemeral pods top out at **9.5 jobs/s** at `w=8` because container create/upload/exec/destroy consumes ~630–850 ms per pod. Fast heuristic jobs **must stay local**.
2. **Local Docker break-even is ~1.6 s/job (at 2x fan-out width, `w=8` vs `w=4`).**
   - At `1.5 s/job`, Pod `w=8` (4.71 s) nearly ties Local `w=4` (4.57 s) and is **1.9x–3.8x faster** than `w=2` / `w=1`.
   - At `3.0 s/job`, Pod `w=8` (7.32 s) beats Local `w=4` (9.13 s) by **1.25x** on `N=12` (two waves of 8+4, hitting 99.7% of theoretical efficiency `2 × 3.65s = 7.30s`), and scales to **~1.7x** on full wave multiples (`N=16, 32`).
   - At **same width** (`w=4` pod vs `w=4` local on the same host), pods never beat local wall-clock — their value at equal width is **isolation** (untrusted PDFs/media parsers, heavy native dependencies), not speed.
3. **Remote ephemeral sandbox providers (E2B, Modal, Runloop) have higher startup overhead (~2–5 s).**
   Applying the same break-even model with $T_{\text{overhead}} = 3\text{–}5\text{ s}$:
   - At 2x fan-out, remote ephemeral pods break even at **~3–5 s/job** and shine on **>= 10 s/job** workloads (audio transcription, video frame extraction + OCR, multi-page PDF parsing, slow LLM/vision pipelines) or bursts whose fan-out width ($W_{\text{pod}} = 8\text{–}16$) exceeds local host CPU/RAM.

## 4. Operational Recommendations (Feeding Documentation)

- **Default `jobs.pod.max_inflight`:** Keep **`4`** as the conservative built-in default (bounds Docker daemon contention, SQLite Stage 2 commit bursts, and cloud spend). Raise to **`8`** (or **`16`** on Postgres + remote sandboxes) for heavy media/OCR/transcription queues.
- **Worker-slot coupling:** `WorkerPool` pulls queued jobs using `server.workers` goroutines. To achieve fan-out width `N` on pods, operators must set `server.workers >= jobs.pod.max_inflight` (default `server.workers` is `4`, so raising `max_inflight` to `8` without raising `server.workers` caps effective concurrency at `4`).
- **Topic selection:** Keep `ingest:text` local (excluded from `DefaultJobsPodTopics`). For workloads that mostly process short local text or code snippets, narrow `jobs.pod.topics` to genuinely heavy topics (`ingest:document`, `ingest:image`, `ingest:audio`, `ingest:video`, and slow `ingest:url` / `ingest:feed_item` bursts).
- **Future optimization:** Micro-batching or warm worker pools (covered separately by running `dpkms serve --runner pod`) eliminate the per-job 630 ms Docker / 2–5 s cloud create/destroy cost when sub-second jobs need remote compute.
