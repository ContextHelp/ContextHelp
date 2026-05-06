# ADR-066 – Ambient Capture Substrate (Local-Side Daemon)

> **Status:** Accepted
> **Date:** 2026-05-05
> **Author:** $USER
> **Applies to:** ctxt CLI, new `ctxd` local daemon
> **Supersedes:** None
> **References:** ADR-007 (transactional outbox), ADR-053 (KnowledgeObject as pipeline draft), ADR-056 (unified enqueue API), ADR-064 (federation), ADR-065 (pluggable adapters), prior art: OpenChronicle (`~/.p/sandbox/OpenChronicle`)

---

## Context

ctxt today ingests knowledge through **explicit, user-initiated** paths:

- `ctxt analyze <input>` for one-shot CLI captures (text, files, URLs, clipboard via auto-detect).
- 18 bulk importers under `internal/importer/` (GitHub, Chrome, Notion, …) — each a one-shot run.
- Two read-only fetch adapters under `internal/ingest/` (himalaya, cardamum) — invoked one-shot via CLI.

There is **no ambient/continuous capture**. Everything requires the user (or a cron job they wrote) to remember to run something. This is a real gap — the user's day produces a steady stream of context-worthy signals (clipboard contents, web pages they read, files they save, apps they switch between, screenshots they take) that ctxt currently misses entirely.

OpenChronicle (`~/.p/sandbox/OpenChronicle`) demonstrates a working ambient-capture model: a local daemon subscribes to OS-level signals, runs them through a deterministic compression funnel (S0 → S1 → Timeline → S2 → durable memory), and exposes the result over MCP. Its scope (full screen-context memory via macOS Accessibility tree) is wrong for ctxt — too invasive, too narrow, macOS-only — but the **substrate pattern** (event sources, debounce, dedup, session boundaries, retention tiers, MCP read-surface) is exactly what ctxt is missing.

**Critical deployment constraint:** in the canonical ctxt topology, **dpkms is often deployed remote** (server-side worker process; could be a household NAS, a managed instance, a federated peer per ADR-064). A remote dpkms cannot read the user's clipboard, see their foreground window, or watch `~/Inbox`. Ambient capture **must run locally on the user's machine** and enqueue against whichever dpkms is configured (local or remote) via the existing HTTP API surface.

This rules out the obvious-but-wrong design of putting an ambient runner inside `cmd/dpkms/`. The ambient substrate belongs on the client side.

The brainstorm produced this analysis (see `~/.claude/plans/check-p-sandbox-openchronicle-which-capt-structured-shell.md`). The track `ambient-capture` carries the implementation plan; this ADR locks the substrate decisions before any code lands.

**The question:** What substrate shape supports pluggable ambient sources running on the user's local machine, feeding cheap-deduped events into the existing dpkms enqueue API, while integrating with kit/bus + kit/policy and composing cleanly with the ADR-065 typed adapter substrate?

---

## Decision

**Adopt a typed ambient capture substrate at `internal/ambient/` (in the ctxt repo, hostable from either the `ctxt` CLI or a new `ctxd` standalone daemon) with five concepts:**

1. **AmbientSource** — a long-running event producer reading some local-machine signal (clipboard, browser history, foreground window, file-system, hotkey-triggered screenshot, …). Each source declares an identity and emits a stream of `RawEvent` values on a Go channel.

2. **RawEvent** — a typed signal envelope: `{ Source, OccurredAt, Kind, Payload, Fingerprint, Suggested Pipeline }`. Sources populate Fingerprint (a SHA-256 over the normalized payload) and Suggested Pipeline (e.g. `text.short`, `url.generic`, `image.ocr`); the runner decides whether to enqueue.

3. **Runner** — multiplexes sources, applies fingerprint dedup at the **client-side enqueue boundary**, attaches the active SessionID, and POSTs the envelope to whichever dpkms is configured via the existing `ctxt analyze` HTTP path. Lifecycle: Start → Ready → Drain → Stop, mirroring ADR-065's adapter lifecycle.

4. **Registry** — typed map of source-name → AmbientSource. Allows multiple sources active concurrently (unlike ADR-065's one-platform-per-protocol invariant — different concept). Enforces unique source names.

5. **Pluggable raw-capture buffer** — sources write to a staging area so the daemon can survive transient network failures (dpkms unreachable) and replay. The buffer is a `Buffer` interface with multiple backends:
    - **Local filesystem (default)** — XDG-compliant path resolution: `$XDG_STATE_HOME/ctxt/ambient/` (falls back to `~/.local/state/ctxt/ambient/` per the XDG Base Directory spec). Configurable override via `$CTXT_AMBIENT_BUFFER_DIR` and `[ambient.buffer] path = ...` in the config file.
    - **S3-compatible object store** — for users who want cross-machine or cross-host buffering (multi-laptop households, ephemeral worker hosts, hosted-ctxd deployments). Backends: AWS S3, Cloudflare R2, Backblaze B2, MinIO, any S3-API-compliant store. Authentication via standard env vars (`AWS_ACCESS_KEY_ID`, etc.) or kit/runtime/secrets bound credentials.
    - **In-memory (test only)** — for unit tests and ephemeral CI runs.

    Buffer backend is selected per-deployment in config; sources are backend-agnostic (they write `RawEvent` to the buffer interface). Tiered retention: raw 7d, normalized 30d, then evicted. Total cap configurable (default 2GB local / unlimited S3 with lifecycle policy) with LRU eviction on the local backend. dpkms-side durability is unchanged.

**Hosting:** the substrate ships as a library (`internal/ambient/`) and a standalone daemon binary (`ctxd`). The user-facing surface is the existing `ctxt capture` command, extended:

- **`ctxt capture --ambient`** — starts (or attaches to) the `ctxd` daemon with auto-config: discovers configured sources, picks default buffer location, wires bus + log, daemonizes. Primary entrypoint for most users. Without `--ambient`, `ctxt capture` retains its existing one-shot behavior.
- **`ctxt capture --ambient stop|status|sources|tail`** — flag/subcommand shape for managing the running daemon.
- **`ctxd` (standalone binary)** — the actual long-running process the CLI launches. Also runnable directly under a process supervisor (`brew services start ctxd`, `launchctl`, `systemctl`) for users who want service-manager lifecycle instead of CLI-launched.

`ctxt capture --ambient` and `brew services start ctxd` are **two ways to launch the same `ctxd` binary**, not parallel implementations. The CLI gets ergonomics + auto-config; the service-manager path gets supervisor-managed restart semantics. Both share `internal/ambient/`. dpkms gains zero capture responsibilities.

**Enqueue path:** the runner POSTs to the same `/api/v1/analyze` endpoint that `ctxt analyze` already uses (per ADR-056 unified enqueue API). The endpoint gains optional fields: `session_id`, `ambient_source`, `fingerprint`. dpkms's existing post-pipeline ContentHash dedup (`internal/jobs/worker.go:215-243`) stays as a network-tolerant safety net.

**Kit primitive integration:** sources emit a comprehensive event taxonomy on the local `kit/runtime/bus` (4-segment past-tense topics: `ctxt.<category>.<object>.<action>`). Every stage of the capture-to-enqueue pipeline emits, so policy authors and ops dashboards can observe (and veto) at any granularity.

| Topic | Emitted when |
|-------|--------------|
| **Source lifecycle** | |
| `ctxt.ambient.source.started` | Source's `Start()` returned successfully |
| `ctxt.ambient.source.ready` | Source has produced its first event or declared readiness |
| `ctxt.ambient.source.drained` | `Drain()` returned (in-flight events flushed) |
| `ctxt.ambient.source.stopped` | `Stop()` returned (hard stop) |
| `ctxt.ambient.source.failed` | Source error (will trigger restart per policy) |
| **Event capture (raw)** | |
| `ctxt.ambient.event.captured` | Source emitted a RawEvent (pre-anything) |
| `ctxt.ambient.event.redacted` | Source-side redaction modified the payload before emission |
| `ctxt.ambient.event.filtered` | kit/policy CEL rule vetoed the event (drop, no enqueue) |
| **Compression / dedup** | |
| `ctxt.ambient.event.debounced` | Event collapsed into in-flight window (e.g. AXValueChanged debounce) |
| `ctxt.ambient.event.deduped` | Fingerprint matched recent event; dropped pre-enqueue |
| `ctxt.ambient.event.compressed` | Multiple source events merged into one RawEvent (e.g. window-focus burst → one focus-change) |
| **Buffer** | |
| `ctxt.ambient.buffer.appended` | RawEvent persisted to staging buffer |
| `ctxt.ambient.buffer.evicted` | Retention/cap eviction removed older entries |
| `ctxt.ambient.buffer.replayed` | Buffered event re-emitted to enqueue path (post-network-recovery) |
| **Enqueue (network)** | |
| `ctxt.ambient.enqueue.queued` | Event queued for HTTP POST to dpkms (in-memory ring) |
| `ctxt.ambient.enqueue.waiting` | dpkms unreachable; event held in buffer (paired with retry-after metadata) |
| `ctxt.ambient.enqueue.attempted` | HTTP POST initiated |
| `ctxt.ambient.enqueue.succeeded` | dpkms returned 2xx; KnowledgeObject ID in payload |
| `ctxt.ambient.enqueue.failed` | HTTP error (4xx/5xx/transport); will retry per backoff policy |
| **Session boundaries** *(per ADR-067)* | |
| `ctxt.ambient.session.opened` | Cutter started a new session |
| `ctxt.ambient.session.closed` | Cutter ended a session (idle/soft-cut/timeout) |

Local kit/runtime/policy CEL rules can subscribe to any of these to gate behavior:

- Veto on `*.captured` to drop sensitive events before fingerprint or buffer (e.g. "drop clipboard while bundle-id matches `com.1password.*`").
- Observe on `*.deduped` / `*.debounced` to tune dedup windows by source.
- Alert on `*.enqueue.waiting` lasting > N minutes (dpkms-down detection).
- Audit on `*.redacted` to verify privacy guards are firing.

Privacy enforcement happens **before** the event leaves the user's machine — a property a remote dpkms cannot give. Every transformation (capture, redact, filter, dedup, compress, buffer, enqueue) is independently observable and independently policy-gateable.

---

## Rationale

### Chosen: client-side substrate, library-shared between CLI and standalone daemon

- **dpkms remoteness is non-negotiable.** Putting capture in dpkms breaks the common deployment. Putting it client-side trivially handles both cases: local-only dpkms users still work, remote-dpkms users gain ambient capture they couldn't otherwise have.
- **One daemon binary (`ctxd`), two ergonomic launch paths (CLI auto-config or service-manager).** `ctxt capture --ambient` is the auto-config zero-config UX; `brew services start ctxd` / `launchctl` / `systemctl` is the supervisor-managed UX. Same binary, same capture pipeline — only the launch ergonomics differ.
- **Client-side fingerprint dedup avoids pointless network calls.** OpenChronicle's content-hash check (their `event_dispatcher.py`) catches lock-screen spam and idle-app no-ops. ctxt today re-hashes content **after** enrichment cost has burned (`internal/jobs/worker.go:215`); for ambient sources that emit ten near-duplicate clipboard events when the user copies the same URL twice, that's wasteful. Cheap pre-enqueue fingerprint at the source side eliminates the noise.
- **Privacy enforcement at the source machine is the only correct boundary.** A CEL rule like "never enqueue clipboard text while 1Password is foreground" cannot run on a remote dpkms — the secret has already left the user's machine. kit/runtime/policy on the local daemon enforces this where it must be enforced.
- **OpenChronicle's session abstraction (3-rule cutter) is the right unit of work, but lives client-side.** Sessions are computed where the foreground-window signal lives. Once cut, the SessionID is sent with each enqueued event; dpkms persists it as KnowledgeObject.SessionID without doing the cutting itself. Detailed in ADR-067.
- **Existing `ctxt analyze` HTTP path is sufficient.** No new wire protocol. Per ADR-056, enqueue is one endpoint; ambient sources are just another producer.

### Rejected alternatives

1. **Embed ambient runner in dpkms.** Rejected: works only when dpkms is co-located with the user. Remote/federated/managed-dpkms deployments lose capture entirely. This was the initial sketch in the brainstorm; user surfaced the constraint and we reframed.

2. **One-and-only `ctxt capture --ambient` (no separate `ctxd` binary).** Rejected: users who want a launchd/systemd/brew-services–managed service shouldn't have to wrap the CLI in a hand-written supervisor or use the CLI's PID. A minimal standalone binary is ~50 LoC of `main.go` over the same library; trivial to ship and exactly what `brew services` / `launchctl` / `systemctl` expect.

3. **Make ambient sources another flavor of ADR-065 typed Adapter.** Rejected: adapters are protocol-shaped (one-platform-per-protocol; bidirectional fetch/serve/submit). Ambient sources are event producers; multiple can run concurrently; they have no protocol identity. Forcing the abstraction would require gutting ADR-065's invariants. Cleaner to keep them as a sibling concept.

4. **Run capture inline in `ctxt analyze` invocations (no daemon).** Rejected: defeats the purpose. Ambient capture means "while you're not running ctxt." A daemon is required by definition.

5. **Continuous accessibility-tree capture (OpenChronicle's whole model).** Rejected for ctxt's scope. ctxt captures *signals* (here's a URL, here's a copied paragraph, here's a foreground app), not *recordings* (here's the AX tree of every window every second). Different product, different privacy posture. We borrow OC's substrate patterns, not its capture surface.

6. **OpenChronicle's 60s LLM timeline aggregator.** Rejected for v1, with caveat (see below).

   **What it does in OpenChronicle** (per `~/.p/sandbox/OpenChronicle/docs/timeline.md` and `timeline/aggregator.py`): a daemon tick fires every 60 seconds, takes the closed wall-clock-aligned 1-minute window of raw S1 captures (focused-element value, visible_text, URL, window metadata — *not* the raw AX tree), and runs an LLM call that emits a normalized JSON array of "what happened" entries — one per distinct conversation/context/tab/file in that minute. The prompt is explicitly **verbatim-preserving, not summarizing**: authored text from editable fields must round-trip in quotes; URLs, titles, file paths, proper nouns must stay exact; passive reads collapse, but in-progress drafts keep their longest version. Output rows look like `[Notes] Shopping list: user drafted a list, latest "milk, eggs, flour, butter".` Stored as `timeline_blocks` rows and consumed by the next stage (the S2 session reducer) instead of feeding it raw captures.

   **Why OC needs it:** OC's downstream session reducer takes a *batch* of timeline blocks per flush and produces a single session-summary entry. Feeding the reducer raw S1 captures directly would blow the prompt budget — Electron-app AX trees hit 200–400 KB each. The timeline stage is a **prompt-budget normalization layer** between S1 (per-capture, big) and S2 (per-session, prompt-bounded). It exists because OC compresses many small events into one large session memory, and that compression has a prompt-size ceiling.

   **Why ctxt v1 doesn't need it:**
   - ctxt has no "session reducer" stage in v1. Each ambient event becomes its own KnowledgeObject via existing pipelines (`text.short`, `url.generic`, `image.ocr`, …) — there is no batched-many-into-one stage with prompt-size pressure.
   - ctxt's RawEvent payload is already small (a clipboard string, a URL, a file path, a focused-window tuple). No 200KB AX trees. Per-capture pipelines absorb this directly.
   - Adding the timeline stage would burn an LLM call per minute per machine for output that ctxt's downstream has no use for.

   **Caveat — when to revisit:** if ADR-067 (session reducer) lands and the reducer's prompt budget can't accommodate a busy session's worth of raw events directly, the timeline aggregator pattern (verbatim-preserving normalization, wall-clock-aligned windows, idempotent production, fallback to heuristic on LLM failure) is exactly the right shape and we should adopt it then. **Treated as a deferred Phase 6+ option, not a permanent rejection.**

---

## Consequences

### Positive

- **Ambient capture works equally well for local-dpkms and remote-dpkms deployments.** Dominant deployment topology is supported.
- **Privacy guards run on the user's machine.** kit/policy CEL rules can drop sensitive events before they cross the network. Auditable + enforceable in the only place that matters.
- **Cheap client-side dedup eliminates pipeline-cost waste on no-op events.** Dominant case for clipboard, foreground-window, and screenshot sources where signal repeats.
- **dpkms stays a pure pipeline+storage worker.** No new responsibilities; existing API surface absorbs ambient enqueues unchanged.
- **One daemon binary, two launch paths.** `ctxt capture --ambient` for zero-config CLI users; `brew services start ctxd` / launchd / systemd for supervisor-managed installs. Identical runtime; user picks ergonomics.
- **Substrate composes with ADR-065 (adapters) and ADR-067 (sessions, separate ADR).** No competing abstractions; each does one thing.
- **OpenChronicle's proven patterns (debounce, content-fingerprint dedup, session cutter, tiered retention) ported without their constraints (macOS-only, AX-tree-shaped, screen-memory scope).**
- **Bus-emitted lifecycle + capture events make the daemon observable via the existing kit tooling** (`kit/runtime/bus` taps, kit/log integration).

### Negative

- **Two binaries to ship and version (`ctxt` and `ctxd`).** Mitigated by sharing all logic in `internal/ambient/`; `cmd/ctxd/main.go` is a thin shell. `ctxt capture --ambient` exec's or fork-exec's `ctxd` rather than reimplementing supervision.
- **Local raw-capture buffer adds disk-state to client deployments.** Default 2GB cap with LRU eviction; surfaced in `ctxt capture --ambient status`. Power users can disable buffering (sync enqueue mode).
- **Network-loss mode requires careful design.** When dpkms is unreachable, the buffer absorbs events; when it returns, the daemon replays. Replay ordering, fingerprint preservation across replay, and "did this already make it?" semantics need care. Inherits ADR-007's transactional-outbox lessons.
- **Source-level privacy posture varies by source.** Clipboard daemons see passwords; browser-history readers see adult-content URLs; screenshot sources see anything visible. CEL guards mitigate but every new source is a new privacy review.
- **Linux + Windows parity is a per-source concern.** Some sources (foreground-window via macOS AX) are macOS-only in v1. The substrate is portable; sources gate on `runtime.GOOS` build tags.
- **Buffered events can become stale.** If a clipboard event sits in the buffer 6 hours waiting for dpkms to come back, is it still worth enqueuing? Configurable max-staleness per source, default 24h, drop older.

### Neutral / Considerations

- **Local MCP read-surface (separate ADR-068).** When dpkms is remote, agents may want to query *what's been captured locally but not yet enqueued* (the buffer). ADR-068 will specify whether `ctxd` exposes a tiny read-surface or whether local-buffer state is opaque to agents.
- **Session boundary computation lives client-side (separate ADR-067).** Since the foreground-window signal is local, the cutter is local. dpkms receives SessionID as opaque metadata.
- **CLI vs. daemon process identity.** Either binary writes to the same buffer + lockfile; only one instance runs at a time. `ctxt capture --ambient` errors out if `ctxd` is already supervising (e.g. via `brew services`), and vice versa. Health-check via well-known socket.
- **Coordination with `~/.fam` deployments.** A household-deployed dpkms is the only case where local-and-remote could mean the same machine. Substrate works either way; no special-casing.

---

## Implementation Notes

### New package: `internal/ambient/`

```
internal/ambient/
├── ambient.go         — AmbientSource interface, RawEvent type
├── runner.go          — multiplex + fingerprint dedup + enqueue
├── registry.go        — typed source registry (unique names, multi-active)
├── events.go          — kit/bus topic builders (4-segment past-tense)
├── policy.go          — kit/runtime/policy wiring (subscribe pre-enqueue)
├── buffer/            — pluggable raw-capture buffer (write-ahead, replay, retention)
│   ├── buffer.go      — Buffer interface + factory (XDG path resolution)
│   ├── retention.go   — tiered retention + LRU eviction (local backend only)
│   ├── local/         — filesystem backend (default; XDG_STATE_HOME/ctxt/ambient/)
│   ├── s3/            — S3-compatible backend (AWS S3 / R2 / B2 / MinIO)
│   └── memory/        — in-memory backend (test only)
├── session/           — ADR-067 session cutter (3-rule heuristic)
│   └── cutter.go
├── redact/            — capture-time redaction hooks (passwords, OAuth tokens)
│   └── redact.go
├── clipboard/         — first source: cross-platform clipboard watcher
├── filewatch/         — drop-folder watcher (fsnotify)
├── browserhistory/    — Chrome/FF/Safari SQLite history poll
├── foreground/        — macOS AX foreground-app watcher (build-tagged)
└── screenshot/        — hotkey-triggered or scheduled screenshot
```

### New binary: `cmd/ctxd/`

```go
// cmd/ctxd/main.go — minimal standalone daemon
//
// All capture logic lives in internal/ambient/. This binary is just the
// process supervisor: signal handling, log/bus wiring, lockfile, healthcheck.
package main

import (
    "context"
    "os/signal"
    "syscall"

    "github.com/ideacrafterslabs/ctxt/internal/ambient"
    "github.com/ideacrafterslabs/ctxt/internal/ambient/clipboard"
    "github.com/ideacrafterslabs/ctxt/internal/ambient/filewatch"
    // ...
)

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    runner := ambient.NewRunner(ambient.Config{ /* enqueue endpoint, buffer dir */ })
    runner.Register(clipboard.New())
    runner.Register(filewatch.New(/* watch dirs */))
    // ...
    runner.Run(ctx) // blocks until ctx done
}
```

### CLI surface (`cmd/ctxt/cmd/capture.go` — new leaf under existing CAPTURE category)

In ctxt's command taxonomy (`cmd/ctxt/cmd/root.go`), CAPTURE is a category that groups `analyze`, `import`, `ingest`, `inbox`, `feed`, `watch`. We add a sibling `capture` leaf with an `--ambient` flag for daemon launch, and rely on existing leaves for one-shot work. (Open question: whether to also expose `ctxt analyze --ambient` as an alias is bikeshed-able post-merge.)

```
ctxt capture --ambient                # auto-config: launches ctxd, configures sources, daemonizes
ctxt capture --ambient --foreground   # run ctxd in foreground (for tmux/debug)
ctxt capture --ambient stop           # stop the running ctxd
ctxt capture --ambient status         # daemon health + active sources
ctxt capture --ambient sources        # list registered + enabled sources
ctxt capture --ambient tail [--source X]  # follow live capture stream
ctxt capture --ambient buffer flush   # force-replay buffered events
ctxt capture --ambient buffer purge   # drop buffer (confirms)
```

### Service-manager surface (alternative to CLI launch)

```
# Homebrew
brew services start ctxd
brew services stop ctxd

# launchd (macOS) — formula installs ~/Library/LaunchAgents/io.ctxt.ctxd.plist
launchctl load   ~/Library/LaunchAgents/io.ctxt.ctxd.plist
launchctl unload ~/Library/LaunchAgents/io.ctxt.ctxd.plist

# systemd (Linux) — package installs ~/.config/systemd/user/ctxd.service
systemctl --user enable --now ctxd.service
```

`ctxt capture --ambient status` detects and reports whichever launch path is in effect.

### AmbientSource interface (illustrative)

```go
package ambient

type AmbientSource interface {
    Name() string                                    // unique source identity
    Start(ctx context.Context, b bus.Bus) error      // begin emitting
    Events() <-chan RawEvent                         // event stream
    Drain(ctx context.Context) error                 // stop accepting; flush in-flight
    Stop(ctx context.Context) error                  // hard stop
}

type RawEvent struct {
    Source            string         // matches AmbientSource.Name()
    OccurredAt        time.Time
    Kind              string         // "text", "url", "image", "file", "window-focus"
    Payload           []byte         // pre-redacted by source
    Fingerprint       string         // SHA-256 over normalized payload
    SuggestedPipeline string         // "text.short", "url.generic", "image.ocr", ...
    SessionID         string         // populated by runner from session.Cutter
    Metadata          map[string]any // source-specific (app bundle id, file path, ...)
}
```

### Phasing

Per the `ambient-capture` track plan:

- **Phase 1 (design):** ADR-066 (this), ADR-067 (sessions), ADR-068 (MCP read-surface).
- **Phase 2 (substrate):** `internal/ambient/` skeleton + client-side fingerprint dedup at the enqueue boundary + local-filesystem buffer (XDG-compliant). S3 buffer ships in Phase 4.
- **Phase 3 (sources):** clipboard → file-watch → browser-history → foreground (macOS) → screenshot. Smallest-surface first.
- **Phase 4 (quality):** session cutter, retention, redaction hooks, S3 buffer backend.
- **Phase 5 (UX):** `ctxt compose --session`, user docs, ops runbook.

### Migration concerns

- **None for existing users.** Ambient is opt-in via `ctxt capture --ambient` or `brew services start ctxd`. Default install behavior of `ctxt capture <input>` unchanged.
- **`ctxt analyze` HTTP payload gains optional fields** (`session_id`, `ambient_source`, `fingerprint`). Backwards compatible; older clients keep working.
- **`internal/storage/types.go` gains `KnowledgeObject.SessionID` (optional)** — covered by ADR-067's migration.

### Testing implications

- Sources are isolated; each tests in-process with a fake event producer.
- Runner tests use a fake source + an HTTP recorder for the enqueue side.
- Replay/buffer tests use a fake clock + a network-fault injector. Run against all buffer backends (local, S3-via-MinIO, memory) via the same test suite.
- Privacy/redaction tests assert that known-sensitive shapes never reach the enqueue HTTP recorder, AND that the corresponding `ctxt.ambient.event.redacted` / `…filtered` topics fire.
- Bus-event taxonomy tests: every transformation emits the documented topic; topic names validate against `bus.ValidateTopic` (4-segment past-tense rule).
- End-to-end smoke test: `ctxt capture --ambient --foreground` against a local dpkms, copy text to clipboard, verify KnowledgeObject created with matching `ambient_source=clipboard` and Fingerprint, AND verify the expected event sequence (`captured` → `deduped`-or-not → `buffered` → `enqueue.attempted` → `enqueue.succeeded`) lands on the bus tap.

### Backwards compatibility

- Existing 18 importers + 2 ingest adapters: unchanged. Ambient is additive.
- Existing `ctxt analyze` invocations: unchanged. The HTTP path absorbs new optional fields.
- Existing dpkms deployments: zero changes required to receive ambient enqueues; the worker dedup-by-ContentHash path catches anything the client missed.

---

## Diagrams

Source-of-truth `.mmd` files live in [`../diagrams/ambient/`](../diagrams/ambient/) and render natively in GitHub, VS Code, Obsidian, and the [Mermaid Live Editor](https://mermaid.live). See [`../diagrams/README.md`](../diagrams/README.md) for authoring conventions.

### [High-level architecture](../diagrams/ambient/066-architecture.mmd)

User's local machine on the left; dpkms (often remote) on the right. `ctxd` and `ctxt capture --ambient` are two launchers of the same daemon library.

### [Per-event flow](../diagrams/ambient/066-event-flow.mmd)

Each RawEvent traverses these stages; every stage emits a kit/bus topic from the §Decision taxonomy.

### [Buffer backend selection](../diagrams/ambient/066-buffer-backends.mmd)

Local-filesystem (default, XDG-compliant), S3-compatible (AWS/R2/B2/MinIO), and in-memory (tests only) backends. Retention strategy differs per backend.

---

## References

- ADR-007 — transactional outbox (informs buffered-replay semantics)
- ADR-053 — KnowledgeObject as pipeline draft (target of ambient enqueues)
- ADR-056 — unified enqueue API (the HTTP path ambient sources POST to)
- ADR-064 — federation (informs the multi-instance/remote topology)
- ADR-065 — pluggable adapters (sibling substrate; distinct concern)
- ADR-067 *(planned)* — Session/WorkUnit as a first-class type
- ADR-068 *(planned)* — dpkms MCP read-surface (and possible local-side mirror)
- OpenChronicle prior art: `~/.p/sandbox/OpenChronicle` — patterns borrowed: event dispatcher dedup (`capture/event_dispatcher.py`), session cutter (`session/manager.py`), tiered retention (`docs/architecture.md`), MCP-as-read-surface (`mcp/server.py`).
- Brainstorm + scope analysis: `~/.claude/plans/check-p-sandbox-openchronicle-which-capt-structured-shell.md`
- tlc track: `ambient-capture` — `tlc track show ambient-capture`
