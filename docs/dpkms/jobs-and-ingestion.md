# Jobs and Ingestion

This document defines the ingestion architecture, job lifecycle, execution model, crash-recovery guarantees and worker behavior for ContextHelp.
It complements the minimal ADR set—particularly **ADR-003 (Separate Write/Read Paths)**, **ADR-007 (Transactional Outbox Pattern)**, and **ADR-078 (No Plugin System)**—and provides the implementation blueprint for robust, fault-tolerant, and extensible analysis workflows.

---

## Overview

In ContextHelp, **no ingestion or long-running operation executes inline inside a CLI or API call**.
Instead, all ingestion proceeds through a **durable, observable job system** that guarantees:

- crash safety
- retry safety
- deterministic pipeline behavior
- resumability and replay
- introspection and debuggability
- consistent storage writes

Every capture—text, URL, image, audio, video, feed item—follows this sequence:

1. **A user or a capture source triggers ingestion** (CLI, REST, gRPC, file watcher, feed).
2. The engine creates a **Job** with status `pending`, storing all inputs.
3. The background worker (`ch serve`) picks up pending jobs.
4. The worker executes the appropriate **pipeline**, built-in or custom.
5. Pipeline steps produce outputs.
6. Worker writes a new **Bookmark** entry.
7. Job transitions to `completed` (or `failed` with retry logic).

This pattern ensures ingestion remains reliable, isolated, extensible, and safe.

---

## Job Lifecycle

A job moves through the following states:

- **pending**
  Newly created, awaiting execution.

- **running**
  Worker has locked the job and is executing pipeline steps.

- **completed**
  Bookmark or output successfully created and job finalized.

- **failed**
  Job exceeded retry limits or encountered an unrecoverable error.

- **retrying** (implicit)
  A transient error occurred; the job was returned to `pending`.

The lifecycle mirrors the transactional outbox pattern and is intentionally simple.

---

## Job Store Schema

Jobs are persisted in durable local storage—typically SQLite—before any processing occurs.

### `jobs` table

| Column            | Type        | Description |
|-------------------|-------------|-------------|
| `job_id`          | TEXT (PK)   | Unique ID |
| `status`          | TEXT        | `pending`, `running`, `completed`, `failed` |
| `payload`         | JSON        | Raw input: type, hints, file reference, content |
| `pipeline`        | TEXT        | Pipeline name (e.g. `text.short`, `feed.fetch`, custom) |
| `retry_count`     | INTEGER     | Number of retry attempts |
| `max_retries`     | INTEGER     | Configured globally or per-pipeline |
| `created_at`      | DATETIME    | Job creation timestamp |
| `updated_at`      | DATETIME    | Last status change |
| `locked_at`       | DATETIME    | Worker lock timestamp |
| `error`           | TEXT        | Last error message, if any |

### `job_steps` table (optional but recommended)

Tracks each step during pipeline execution:

| Column       | Type      | Description |
|--------------|-----------|-------------|
| `id`         | TEXT (PK) | Step record ID |
| `job_id`     | TEXT      | Associated job |
| `step`       | TEXT      | Pipeline step name |
| `status`     | TEXT      | `pending`, `running`, `completed`, `failed` |
| `log`        | TEXT      | Any debug or warnings |
| `started_at` | DATETIME  | Start time |
| `ended_at`   | DATETIME  | End time |

This enables introspection and debugging.

---

## Ingestion Flow

### Step 1: Ingestion Trigger

Job creation may come from:

- CLI (`ch analyze`)
- REST / gRPC
- Browser extension
- Feed auto-fetch
- Scheduled refresh

Regardless of source, ingestion **never executes immediately**, ensuring a unified and safe execution model.

### Step 2: Create Job (Transactional Outbox)

The engine atomically stores:

- input type
- raw content or URL
- metadata (hints, tags, mentions)
- pipeline name
- timestamps

If the process crashes at this moment, the job still exists and will run later.

### Step 3: Worker Locks and Executes Job

`ch serve` continuously pulls jobs:

```
SELECT * FROM jobs
WHERE status = 'pending'
ORDER BY created_at ASC
LIMIT 1
FOR UPDATE
```

Job transitions to `running`.

### Step 4: Execute Pipeline Steps

Pipeline execution is deterministic:

- inference step
- fetch step
- parsing step
- summarization
- tagging
- mention extraction
- entity resolution
- installed steps (from a step registry)
- enrichment steps

Each step may:

- produce output
- emit events
- enqueue additional jobs
- fail and retry
- write debug logs

Failure semantics:

- **Retryable** errors → return to `pending`
- **Permanent** errors → mark job as `failed`

### Step 5: Dedup Check and Write

After pipeline execution, the worker computes a **content hash** (SHA-256 of normalized content + source) and checks for an existing bookmark with the same hash.

**Duplicate found** — the worker **reinforces** the existing bookmark:
- Merges new tags and mentions (dedup by label/string, first-seen wins)
- Increments `reinforcement_count`
- Updates `last_reinforced_at`
- Completes the job with the existing bookmark's ID

**No duplicate** — the worker creates a new bookmark:
- Sets `content_hash`, `reinforcement_count = 1`
- Writes bookmark and mention edges (ADR-049)

**Concurrent race** — if two workers process the same content in the same profile simultaneously, the unique index on `(content_hash, profile_id)` (SQLite and Postgres alike) prevents duplicate creation. The second worker's `Create` fails with `storage.ErrAlreadyExists` and it falls back to reinforcement.

This ensures idempotent ingestion: re-ingesting the same content strengthens the signal rather than creating duplicates.

### Step 6: Mark Completed

The job transitions to `completed`.

---

## Crash Recovery

The ingestion system tolerates:

- worker crashes
- CLI/API call interruptions
- step failures
- OS termination
- network/AI provider outages

### Stale Job Detection

A job is stale if:

- `locked_at` older than configured threshold
- worker PID no longer active
- host restarted

Stale jobs are returned to **pending** automatically.

This ensures no zombie jobs or pipeline deadlocks.

---

## Retry Semantics

Jobs have `max_retries`:

- global defaults
- per-pipeline settings

Retry rules:

- transient errors → retry
- permanent errors → fail immediately
- retry exhaustion → `failed`

### Manual retry:

```bash
ch jobs retry <job_id>
```

---

## Running Multiple Workers

Supports:

### Single Worker (default)
- Ideal for local-first operation.

### Multi-Worker Mode
- Requires shared storage
- Workers coordinate locking

---

## Job Introspection (Observability)

Tools for observing pipeline behavior:

```bash
ch jobs list
ch jobs status <job_id>
ch jobs logs <job_id>
```

Example:

```
Job 9128-ABCD
Status: running
Pipeline: feed.fetch
Step 2/5: rss-parse
Retries: 1/3
Last error: network timeout
```

---

## Interaction With Pipelines

### Pipelines Do Not Run Inline

All processing happens inside worker context.

### Pipelines Do Not Write Bookmarks Directly

The worker writes the final output once pipeline execution succeeds.

### Re-analysis Runs on Stored Content

Re-analysis (`ctxt upgrade run`) re-runs an object's pipeline over its stored
content and updates the same object.

- The network fetch steps, the cleaners of a freshly fetched response and
  the dedup step are not run. A library caller can allow the fetch steps;
  there is no CLI flag for it.
- Kept as stored: the inbox note, reminders, expiry, tags and mentions a
  step did not produce (user, manual and imported ones), the caller's
  structured data and the ambient capture metadata. The type, subtype and
  content type are kept when the content did not change.
- A step's own tags, sections, summaries and graph nodes are replaced by
  the new run. Each node and edge a step adds is stamped with the step's name
  (`produced_by`). A reanalysis replaces only the nodes and edges stamped by
  the steps it runs. It keeps the rest: imported, client and user nodes, the
  stored original, and the nodes of a step no longer in the pipeline.
  - A rebuilt node takes the lowest free ordinal of its type at or above the
    step's own position, and a representation section takes the lowest free
    `order`. So IDs and orders come out as before when no node the steps do
    not own sits at an ordinal or `order` the steps use. When one does, the
    later nodes of that type move up by one, and the result is the same on
    every later run.
  - An edge the steps do not own, pointing at a rebuilt node, is put back
    when the node returns with the same ID, type and label. It is dropped
    when the node moved. It is also dropped when any node of that type moved
    and the node's label was carried by two nodes of that type, before the
    run or after it.
  - A graph stored before stamping (no `stamped` mark) goes through the
    earlier rule once: step-kind nodes are dropped and rebuilt, and the run
    marks the graph.
- If an LLM or vision call fails, an embedding call of a model that was
  built fails, or a reply is unusable (empty, or not the JSON a step
  requires), the object fails and nothing is written. This is the same for
  `ctxt upgrade run` and the automatic re-derive. A well-formed reply with
  nothing in it is a valid result. An embedding model whose provider cannot
  be built is skipped, as at ingest, and a failed masked vector falls back
  to the raw one.
- `dependency_enricher` runs in a re-analysis. It rewrites the dependency
  artifact nodes and `metadata.items_to_enqueue` / `deps_enqueued` from the
  text, as ingest wrote them. Nothing is enqueued: only the ingest worker
  consumes that list. `url.github.repo` (needs the fetch) re-derives only with
  the fetch allowed, which is what the automatic re-derive does; the stored
  README is replaced by the mapper's distilled one. `url.github.starred`
  re-derives from stored content.
- The content hash is recomputed. If it equals another object's hash in the
  same profile, both objects are kept: the re-analysed object stores no
  hash and records `metadata.duplicate_of`.
- An object that is encrypted or held, that no pipeline ever derived (an
  import, a raw or inbox capture), or whose pipeline stages items,
  needs a fetch that is not run, or reads its source file, is refused with
  `upgrade.ErrCannotReanalyze`. See [storage](storage.md) for originals.
- An object whose pipeline has an LLM or vision step that cannot run is
  refused the same way, naming the resource and the step. That is a step
  left out when the pipeline was built, a step with no provider, or a step
  holding the `stub` backend. A step removed with `skip_steps` is not
  refused.
- `Service.ReanalysisLocality` classifies the steps a re-analysis of a
  pipeline would run, without running them:

  | Result | Meaning |
  |---|---|
  | `none` | No provider is called. |
  | `local` | Every provider endpoint is a loopback or private address and no proxy is in the way. |
  | `hosted` | A provider is hosted, or its endpoint is a hostname, a public address, empty, or reached through a proxy. |
  | `unknown` | A step is not a built-in, or hands the work to another process (`ibr_fetcher`). |
  | `unavailable` | An LLM or vision step cannot run, or the pipeline was left out at start. |

  Only `none` and `local` are safe to run without asking. No DNS lookup is
  made, so a hostname is always `hosted`, Docker and LAN names included.
- The write emits `object.updated`, rewrites the mention edges and runs
  fan-out when it is enabled.

### Automatic re-derive

Each object carries the derivation signature of the pipeline that derived it
(step versions and the configured LLM and vision models). When a step version
or a model changes, the stored objects are stale. At start, dpkms re-derives
them in the background, as a task job of type `derivation:rederive`.

At start dpkms records the entries behind each signature it can produce.
Stale objects are grouped by stored pipeline and signature with one query,
and each group is decided once, comparing the entries of its stored signature
with the current ones:

| Case | What happens |
|---|---|
| Every added or re-versioned step runs on stored content, or only a model or the order changed | Re-derived from stored content. The source is not asked. |
| An added or re-versioned step only runs when fetching (a fetch step, a cleaner of fresh content) | Re-derived by fetching again, with the refresh guard. |
| The pipeline only lost steps (`skip_steps`) | Stamped current. Nothing runs, no LLM call, no fetch. |
| The pipeline is not registered, or its stored signature has no recorded entries | Deferred. |
| An added or re-versioned step runs in neither mode (pruned, left out) | Deferred, naming the step. |
| The work would use a hosted, unknown or unavailable provider (see `ReanalysisLocality`) | Deferred. |

A job is enqueued only when some group is not deferred. Deferred groups are
not touched and write no record. They are logged once per pipeline with the
reason, listed in the end-of-run summary, and shown by `ctxt upgrade status`
(pipeline, objects, reason) from the `/healthz` upgrade envelope, also when
nothing runs and the daemon is idle. The skips of the latest run are shown
there by reason. A group becomes eligible by itself when the registry, the
recorded entries or the configuration change.

- Objects received by federation are not re-derived here: they arrive without
  a signature, and an object without one is never stale. The exception is a
  replica stored before the instance first recorded signatures: it was signed
  like a local row and counts as local.
- Refresh guard, applied when fetching again. A page fetch keeps the stored
  object unless the new text is the same content with less noise; the host
  check applies to page fetches only. A repository fetch (GitHub) names the
  repository itself, so READMEs may be rewritten.

  | Fetch | Accepted when |
  |---|---|
  | Page | The new text is not empty; at least 60% of its word 3-grams were in the stored text; at least 25% of the stored text's 3-grams are still there; the fetch ended on the host the stored text came from (its recorded final URL, else its source; case and a leading `www.` ignored). A stored text with under 20 3-grams cannot be compared, so it is not refreshed at all: it is a skip at once, before any request (`refresh_rejected`, "stored text too short to compare with a refresh; left as captured"). |
  | GitHub repository | The new text is not empty and not under 10% of a stored text of 500 bytes or more. |

  Text without spaces (Chinese, Japanese, Thai, Lao, Khmer, Myanmar) is
  compared by character. Only the first 20000 words are compared, so a huge
  page costs the same; a page much longer than the stored one is refused
  all the same. A page whose content changed since it was captured is left
  as captured; after three strikes it is a skip, `refresh_rejected`.
- Success stamps the signature of the pipeline the plan ran. Only a local
  determination is a skip at once: `refused` (the object or its pipeline
  cannot be re-derived: encrypted, held, a lost original), or a stored page
  too short to compare with a refresh (decided from the stored object alone,
  no request; `refresh_rejected`). A skipped object
  is not tried again until the pipeline's signature changes. The stored
  detail is the error text, shortened, with every URL reduced to scheme and
  host and token, password and bearer values removed.
- Nothing that comes from the network is a skip at once. Every such outcome
  is a try: no skip, the object stays stale, and a `rederive_attempts` row
  (tries and strikes, per object and target) is written; objects with fewer
  tries go first, so objects that keep failing never hold back the rest. A
  try also counts as a strike at the end of the run, but only on evidence
  from the same run that it is about this object:

  | Outcome | Strike only when, in the same run |
  |---|---|
  | The source says 404 or 410 | some refresh was fetched and accepted, from any host |
  | The guard rejects a refresh | some refresh was fetched and accepted |
  | A rate limit (429, a `Retry-After` answer, a GitHub rate limit) | never: it is about the host, not the object |
  | Any other status (401, 403, 5xx) | a fetch to the same host succeeded and was accepted |
  | No answer: DNS, TLS, connection, timeout, blocked destination | a fetch to the same host succeeded and was accepted |
  | A provider call failed, or its reply was unusable | an object was re-derived and its reanalysis made a call of that provider kind (LLM, vision, embedding) |
  | An error of no known kind | any object was re-derived |

  In a group that fetches, every row above also needs one more thing: a
  refresh was fetched and accepted in the same run, from any host. Without
  it nothing counts against the object, whatever else succeeded.

  The failure after 3 strikes is the skip (`fetch_failed`, `refresh_rejected`,
  `provider_failed`, `unusable_reply` or `failed`), with "after 3 strikes".
  "Same host" is the host (with its port) the failing fetch was addressed to:
  the source URL's for a page, the API's for GitHub. A refresh the guard
  rejects counts as a failure of the fetch, not of the page, until another
  refresh is accepted.
- Proof is per call: a success counts for a provider kind only if its
  reanalysis actually made a call of that kind (an object too short for the
  model, or with nothing to embed, proves nothing). A registry of embedding
  models that cannot be read is never an object's fault and never strikes.
  The trade-off: in a group that does not fetch, a failure of no known
  kind counts as a strike whenever anything else succeeded in the run, so a
  recurring storage error on one object ends in a skip.
- A provider that answered and whose reply no step could use is a try like
  the rest. Until the run has shown the provider working, such replies count
  in the streak below, so a model that answers nothing to everyone stops the
  group; after that they do not. In a group that fetches, "shown working"
  also takes a refresh fetched and accepted in the same run, as a strike
  does. A failed call beside an unusable reply is a
  failed call. The Ollama client treats a non-2xx answer as a failed call,
  not a blank reply.
- Five tries in a row stop the group, not the run: the run goes on with the
  next group (groups run in order of pipeline name), and a success in the
  group resets the count. The stopped group's remaining objects stay stale
  for the next start, fewest tries first. A run can therefore complete with
  fewer objects done than its total. The run ends normally, and the
  status is `failed` only when the run itself broke (a panic, storage
  unusable). Each stopped group is reported with its kind (llm, vision,
  embedding, fetch, refresh rejected, unknown) and the last cause in
  `ctxt upgrade status`, the `/healthz` upgrade envelope (`stopped`), the
  end-of-run log line and the completed event: one entry per pipeline, with
  the first group's cause and a count when more than one group stopped. The
  list of the previous run is cleared when a new run starts.
- Starting on another network. A start on a network that cannot reach a
  source (off the VPN, a captive portal that answers every address with a
  login page, an expired token, a dead resolver) costs tries, never a skip:
  nothing is proved about any object there. (A stored page too short to
  compare is skipped on any network: that is decided from the stored object,
  without a request.) In a group that fetches, no
  failure counts against an object unless a refresh was fetched and
  accepted in the same run, on top of the evidence in the table above. A
  portal page never counts as an accepted fetch: a stored page too short to
  compare is left as captured without a request, and a longer one is only
  replaced by a page that is the same content. Rate limits never count
  against an object, whatever else the host served. The next start on a
  working network takes up the same objects, fewest tries first. Only when
  some other object on the same host (or, for a source that is gone, on any
  host) is fetched and accepted in the same run does a failure start to
  count against its object.
- An object deleted since it was listed is dropped and counted as `gone`:
  no try, no strike, no skip. Only the object lookup says so; another
  not-found inside a reanalysis is an error like any other.
- Skip reasons: `refused`, `refresh_rejected`, `fetch_failed`,
  `provider_failed`, `unusable_reply`, `failed`. Tries, strikes and skips
  whose target no definition can produce are deleted at start (the current
  and baseline signatures of every pipeline are kept, and so are the targets
  of pipelines that cannot be built at this start, a provider being late);
  a signature that comes back later starts clean.
- Discarded objects are not re-derived or stamped. Objects with status
  `active`, `inbox` and `raw` are.
- The run waits `upgrade.rederive.pause` between two objects (default `2s`).
  It stops promptly on shutdown, leaves the status idle, and resumes at the
  next start from what the store holds. It shares the upgrade status with
  the re-projection: if that is busy, the job waits for it, in the worker
  (the queue has no delayed requeue). While it runs, `/healthz` reports
  `upgrading`.
- At the end one log line gives processed, stamped, skipped by reason,
  failed, retried (tries), gone and deferred by pipeline. The same summary is published on
  `dpkms.upgrade.rederive.completed` (`.interrupted` when the run stops
  early).
- Turn it off with `upgrade.rederive.enabled: false`. Nothing is scheduled or
  written then, and a job already pending does nothing. Stale objects stay as
  stored, and start logs one line with how many there are.

### Pipelines May Enqueue Jobs

This is essential for:

- RSS feed parsing → enqueue feed-item ingestion
- Refresh → enqueue refresh jobs

---

## Interaction With Storage

Storage must provide:

- atomic job creation
- safe job claiming
- bookmark writes independent of job writes
- crash-safe journaling

SQLite is default; Postgres is the other supported driver.

---

## Advanced: Delayed, Scheduled, and Background Jobs

The ingestion system supports delayed and scheduled execution, enabling:

- recurring feed refresh
- nightly cleanup routines
- periodic registry sync

These operations are expressed as **jobs** and use the exact same lifecycle.

---

## Summary

The jobs and ingestion system provides:

- **Safety** (crash tolerance, durable writes)
- **Determinism** (idempotent pipelines and replayable jobs)
- **Deduplication** (content-hash based, with reinforcement tracking on re-ingestion)
- **Extensibility** (custom pipelines and installed steps run on the same job model)
- **Observability** (job_steps logs, worker inspection)
- **Scalability** (multi-worker support with concurrent race handling)
- **Correctness** (clear separation of write/read paths)

It is the backbone of ContextHelp's ingestion, keeping every ingestion robust, predictable, and safe.
