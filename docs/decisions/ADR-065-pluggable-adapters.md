# ADR-065 – Pluggable Adapters: Protocol Server + Read/Write Substrate

> **Status:** Proposed
> **Date:** 2026-05-05
> **Author:** $USER
> **Applies to:** dPKMS
> **Supersedes:** None
> **References:** ADR-058 (external step execution), ADR-063 (graph-canonical KnowledgeObject), ADR-064 (federation)

---

## Context

dPKMS today has a **read-side ingestion substrate** at `internal/ingest/` (adapter.go, registry.go, runner.go) with two concrete adapters: `cardamum` (vCard fetch) and `himalaya` (email fetch). The pattern is: adapter `Fetch(ctx)` returns `[]Object`; runner dedups and stores. No write-back, no server-side, no protocol exposure.

Multiple downstream consumers need substantially more:

- **`~/.fam`** (household workspace, ops repo) — needs full IMAP/SMTP server (Stalwart) so family-member devices connect to dPKMS-served mail; CardDAV/CalDAV servers for contacts/calendars; mxhook bridge fetching from existing-provider IMAPs with send-as outbound; full read+write+server surface.
- **Three early-adopter businesses** — each will deploy dPKMS with platform-specific backends (one to Gmail business, one to Microsoft 365 business, one to Proton). Each needs the same substrate pattern with different platform credentials.
- **IC's FIR portfolio** — multiple incubation deployments that each need mail/contacts/calendar surfaces, each picking different backends.

The current `internal/ingest/` registry shape is a poor fit:

- **Registry is unconstrained** (`map[string]AdapterFactory`). Multiple Gmail-shaped adapters could register simultaneously; nothing enforces "one platform per protocol" — which the brainstorm explicitly settled on as the dPKMS-level rule, with multi-platform handled via federation (per ADR-064).
- **No protocol-server adapters** — `Stalwart-as-IMAP-server`, `cardamum-as-CardDAV-server` aren't fetch-style adapters. They serve a wire protocol to clients and react to client writes.
- **No kit primitive integration** — `~/.fam` policy guards rely on `kit/runtime/policy` (kit ADR-0008) intercepting pre-* events. The current adapter substrate has no concept of pre-event emission; there's nothing for kit/policy to subscribe to.
- **No backend taxonomy** — Stalwart-as-MX vs. mxhook-bridge-to-Gmail vs. mxhook-bridge-to-iCloud are *different backends* of the same `email` adapter, but the current registry has no way to express "the email adapter has a backend slot, and exactly one backend is configured per dPKMS instance."

**The question:** What substrate shape supports protocol-server + read+write + bidirectional adapters with kit-primitive integration, while enforcing one-platform-per-protocol per instance and composing with ADR-064 federation for multi-platform deployments?

---

## Decision

**Adopt a typed, pluggable adapter substrate at `internal/adapter/` with five concepts:**

1. **Protocol slot** — a named abstraction for a wire-protocol family (e.g. `email`, `contacts`, `calendar`, `reminders`, `accounts`, `bills`, `files`). Each dPKMS instance has exactly one backend per protocol slot.
2. **Adapter** — the implementation of a protocol slot. Adapters MAY be read-only (fetch from external source), write-only (emit to external sink), bidirectional (read + write + serve a wire protocol to clients). Adapters declare their **capabilities**: `fetch`, `serve`, `submit`, `subscribe-events`, `emit-events`.
3. **Backend** — a concrete configuration of an adapter for a specific platform. The `email` adapter's backends include `stalwart` (bundled MX), `mxhook+gmail`, `mxhook+icloud`, `mxhook+m365`, `mxhook+proton`, etc.
4. **Adapter registration** — registers (protocol slot, adapter implementation) at dPKMS startup. Registration validates the one-platform-per-protocol invariant.
5. **Adapter lifecycle** — start, ready, drain, stop. Lifecycle events emit on the bus per kit's 4-segment convention; pre-* events are veto-able by kit/runtime/policy.

**The existing `internal/ingest/` becomes a special case** — fetch-only adapters (cardamum, himalaya) continue to work, but they live at `internal/adapter/<protocol>/` under the new substrate. Migration is mechanical: existing `Adapter.Fetch` becomes the `fetch` capability of the new typed adapter. No semantic change for current ingest consumers.

**Federation composition (per ADR-064):** if a deployment needs multiple platforms for the same protocol (e.g. Gmail AND iCloud as mail backends), the operator runs **two dPKMS instances**, each with one platform configured, federated per ADR-064. The Adapters substrate itself never multi-backends a single protocol slot; multi-platform is handled at federation.

**Kit primitive integration:** the substrate adopts `kit/runtime/bus` for pre/post events (4-segment past-tense convention), `kit/runtime/policy` for operation gating (CEL rules on pre-* topics), `kit/runtime/sync` for cross-instance/device replication (composes with ADR-064 federation).

---

## Rationale

### Chosen: typed protocol-slot substrate with one-platform-per-protocol invariant

- **One-platform-per-protocol matches operator mental model.** An operator configuring a deployment names *which* mail platform they're using, not "all the mail platforms." Multi-platform is a federation question, not a configuration question.
- **Kit-primitive adoption is required, not optional.** `~/.fam` policy guards (FR-060–FR-066 in the `~/.fam` spec) only work if adapters emit pre-* events. Building a parallel event mechanism would duplicate kit/bus.
- **Backend taxonomy makes operator config legible.** "I'm using `email.stalwart`" is clearer than "I'm using `email` with provider=stalwart and 47 platform-specific options"; the backend selection commits the operator to a platform's specific quirks (Gmail OAuth, iCloud app-passwords, M365 tenant config).
- **Existing `internal/ingest/` is preserved.** Mechanical migration; no semantic change for cardamum and himalaya consumers.
- **Federation composition (ADR-064) handles the hard case.** Multi-platform deployments don't pollute the substrate; they live at federation. ADR-064's push-only DAG is exactly the shape for this.

### Rejected alternatives

1. **Multi-backend single-protocol.** Allow `email` to have N backends concurrently active in one instance. Rejected: configuration explosion (which backend handles which incoming mail?), platform-specific quirks bleed across the abstraction, no clean way to enforce per-platform policy. ADR-064 federation already provides multi-platform composition cleanly.

2. **Extend `internal/ingest/` in place.** Add server/write capabilities to the existing `Adapter` interface. Rejected: the existing interface is fetch-shaped (`Fetch(ctx) []Object`); bolting on server/write semantics produces a kitchen-sink interface where most adapters implement most methods as no-ops. Cleaner to introduce typed protocol slots and let read-only adapters be a capability subset.

3. **Adapter-per-binary plugins** (analogous to ADR-058 external step execution). Rejected for the substrate; appropriate for individual adapters when they need it. Stalwart, for instance, *is* a separate binary the dPKMS adapter wraps; cardamum is in-process Go. Both fit under the same protocol-slot abstraction. ADR-058's external-step protocol stays available for adapter authors who want it; not the substrate primary path.

4. **kit/runtime/peer + kit/runtime/sync as the substrate, not "adapters" per se.** Could express adapters as peer nodes that sync slices of state. Rejected: protocol-server semantics (Stalwart serving IMAP to a phone) don't naturally map to peer-sync; the protocol-slot abstraction is the right level. kit/runtime/peer + sync compose underneath for cross-instance state.

---

## Consequences

### Positive

- **One-platform-per-protocol is enforced at registration.** Misconfiguration (two mail backends both registered) fails at startup with a clear error, not at runtime with mysterious behavior.
- **Adapter capabilities are explicit.** `fetch`-only adapters declare what they do; protocol-server adapters declare differently. Operators reading config or substrate consumers reading capability metadata don't guess.
- **Pre-* event integration with kit/runtime/policy is automatic.** Every adapter emits its pre-* events on the bus; policy authors write CEL rules without per-adapter wiring.
- **Federation composition is unchanged.** ADR-064 keeps working; multi-platform deployments use it.
- **Migration of existing ingest is mechanical.** cardamum and himalaya become `internal/adapter/contacts/cardamum` and `internal/adapter/email/himalaya` (or similar paths) without semantic change.
- **Backend taxonomy makes consumer specs (`~/.fam`, early-adopter businesses, FIR deployments) cleaner.** `~/.fam`'s spec says "configures `email.mxhook+gmail` for jad, `email.mxhook+icloud` for rania" — clear and operator-meaningful.
- **CLI surface composes naturally.** `dpkms adapter list`, `dpkms adapter show <protocol>`, `dpkms adapter configure <protocol> <backend>` — uniform across protocols.

### Negative

- **Backend explosion for popular protocols.** Email may grow many backends (Gmail, iCloud, M365, Proton, Fastmail, Stalwart, Postfix, Maildir-on-disk, …). Each is a real implementation effort. Mitigated by: most are mxhook-shaped variations differing only in auth + send-as quirks; mxhook itself absorbs the variation.
- **Adapter lifecycle adds substrate complexity.** Today's adapters fetch on demand; the new substrate has start/ready/drain/stop. Worth the cost because protocol-server adapters need it (Stalwart can't be "started on demand" — it's a long-running daemon).
- **Pre-* event emission is mandatory for all adapters.** Read-only adapters that don't currently emit events have to start. This is a hard requirement (kit/policy depends on it) but adds boilerplate.
- **Existing ingest tests need migration.** Mechanical but not zero work.

### Neutral / Considerations

- **Backend authentication.** OAuth (Gmail, M365), app-passwords (iCloud), API tokens (Proton/Fastmail) — each is platform-specific. Backend implementations encapsulate this; substrate stays auth-agnostic.
- **Backend quirks.** Each backend has rate limits, message-size limits, MIME quirks. Backends own these; adapters above expose a normalized contract.
- **Coordination with the in-flight `dpkms-cli-review-parity-sweep` track.** CLI surface for `dpkms adapter` commands lands as part of whichever track gets there first. This ADR specifies the substrate; the CLI shape is a downstream concern.

---

## Implementation Notes

### New package: `internal/adapter/`

```
internal/adapter/
├── adapter.go        — Adapter interface, Capability set, lifecycle states
├── registry.go       — typed registry with one-platform-per-protocol enforcement
├── runner.go         — lifecycle orchestrator + bus event emission
├── events.go         — pre-* and post-* event topic builders (kit/bus convention)
├── policy.go         — kit/runtime/policy wiring (subscribe pre-* topics)
├── email/            — protocol slot
│   ├── slot.go       — slot identity + backend selector
│   ├── stalwart/     — bundled MX backend
│   ├── mxhook/       — bridge backend (subdirs: gmail/, icloud/, m365/, proton/, fastmail/)
│   └── himalaya/     — fetch-only legacy backend (migrated from internal/ingest/himalaya)
├── contacts/         — protocol slot
│   ├── slot.go
│   └── cardamum/     — CardDAV vdir backend (migrated from internal/ingest/cardamum)
├── calendar/         — protocol slot
│   ├── slot.go
│   ├── caldav-vdir/  — local CalDAV server backend
│   ├── gcal/         — Google Calendar bridge
│   └── o365/         — Microsoft 365 bridge
├── reminders/        — protocol slot
├── accounts/         — protocol slot (DPKMS-native; references credential refs into 1Password / Apple Keychain)
├── bills/            — protocol slot
└── files/            — protocol slot (deferred, named for completeness)
```

### Adapter interface (pseudo-Go)

```go
package adapter

type Capability string

const (
    CapFetch     Capability = "fetch"
    CapServe     Capability = "serve"
    CapSubmit    Capability = "submit"
    CapEmitEvents Capability = "emit-events"
    CapSubscribeEvents Capability = "subscribe-events"
)

type LifecycleState string

const (
    StateStopped  LifecycleState = "stopped"
    StateStarting LifecycleState = "starting"
    StateReady    LifecycleState = "ready"
    StateDraining LifecycleState = "draining"
)

type Adapter interface {
    // Identity
    Protocol() string         // "email", "contacts", "calendar", ...
    Backend() string          // "stalwart", "mxhook+gmail", "cardamum", ...
    Capabilities() []Capability

    // Lifecycle
    Start(ctx context.Context, bus bus.Bus) error
    Ready() bool
    Drain(ctx context.Context) error
    Stop(ctx context.Context) error

    // Capability-gated methods (only callable if the capability is declared)
    Fetch(ctx context.Context) ([]Object, error)            // CapFetch only
    Submit(ctx context.Context, obj Object) error           // CapSubmit only
    Serve(ctx context.Context, listener net.Listener) error // CapServe only
}
```

### Registry with one-platform-per-protocol invariant

```go
type Registry struct {
    mu      sync.RWMutex
    slots   map[string]Adapter // keyed by Protocol(), exactly one Adapter per protocol
}

func (r *Registry) Register(a Adapter) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    if existing, ok := r.slots[a.Protocol()]; ok {
        return fmt.Errorf("protocol %q already configured with backend %q; "+
            "one-platform-per-protocol — use federation (ADR-064) for multi-platform",
            a.Protocol(), existing.Backend())
    }
    r.slots[a.Protocol()] = a
    return nil
}
```

### Bus event topics (kit/runtime/bus, 4-segment past-tense)

```
dpkms.adapter.lifecycle.started
dpkms.adapter.lifecycle.ready
dpkms.adapter.lifecycle.draining
dpkms.adapter.lifecycle.stopped

dpkms.<protocol>.entity.pre_validated     (e.g. dpkms.email.entity.pre_validated)
dpkms.<protocol>.entity.pre_persisted
dpkms.<protocol>.entity.persisted
dpkms.<protocol>.entity.failed
```

Adapters emit pre-* events before mutating local state; kit/runtime/policy subscribes via `policy.Wire(bus, engine)` and vetoes via `PolicyDeniedError`. Post-* events fire after successful mutations.

### Policy integration

```go
// In dpkms startup:
engine, _ := withcel.New(policyConfig)  // CEL backend, kit ADR-0008
policy.Wire(bus, engine)                 // subscribes to all pre-* topics

// Each adapter's pre-event emission goes through bus; the engine intercepts.
// PolicyDeniedError causes the adapter's mutation to abort.
```

### Federation composition (ADR-064)

Adapters do NOT directly federate. The existing federation substrate (per ADR-064) handles cross-instance object sync; adapters produce the objects that get federated. A multi-platform deployment is:

```
[dpkms-instance-1: email.mxhook+gmail] --federation--> [dpkms-merged]
[dpkms-instance-2: email.mxhook+icloud] --federation--> [dpkms-merged]
```

Each instance has one email backend; the merged instance sees objects from both via ADR-064's push-only DAG.

### Migration phases

> Phase 2 expanded by the 2026-05-06 amendment (see below) to add sensor adapters + ambient capture; files slot pulled forward from Phase 4.

- **Phase 1** — Substrate (`internal/adapter/`) + email + contacts protocol slots + migration of cardamum/himalaya from `internal/ingest/`. Stalwart and mxhook backends out of scope for Phase 1. **(landed 2026-05-06)**
- **Phase 2** — Service adapters (mxhook+gmail, caldav-vdir) + sensor adapters (rss, sshfs/sftp, s3-compatible, local-fs, watched-fs, clipboard, screen, mic) + files slot + calendar slot + os-platform slots (clipboard/screen/mic each its own slot). Adds the `ctxt capture` umbrella verb + `--ambient` mode + `policy/ambient.yaml` config. Closes Acceptance Gate 4 (CEL veto on adapter pre_persisted).
- **Phase 3** — Stalwart backend (bundled MX). Additional mxhook backends (iCloud, M365, Proton, Fastmail). Reminders, accounts, bills slots. Linux + Windows ports of the OS-platform sensors.
- **Phase 4** — External-step adapters (per ADR-058) for adapter authors who want a separate-binary path.

Existing `internal/ingest/` is kept as a thin shim during Phase 1+2 for backwards compat; removal in Phase 3.

### Testing implications

- Each adapter has its own test pkg under its directory.
- Substrate has lifecycle integration tests that verify start/ready/drain/stop.
- One-platform-per-protocol invariant has registration-time tests.
- kit/policy integration has end-to-end tests that veto adapter operations via CEL rules.

### Backwards compatibility

- Existing `internal/ingest/` callers continue to work during Phase 1+2 via a shim.
- Existing cardamum/himalaya CLI invocations unchanged.
- Federation per ADR-064 unaffected.

---

## Amendment 2026-05-06 — Service vs. sensor distinction; ambient capture; OS-platform slots

### Context for the amendment

Phase 1 landed the substrate (PR #24) with two fetch-only adapters (cardamum, himalaya) wrapped through `internal/adapter/legacy.AsLegacy`. Phase 2 was originally scoped narrowly: Stalwart + mxhook+gmail + caldav-vdir + reminders slot. Walking forward into Phase 2 surfaced three issues the original ADR didn't address:

1. **Adapter mental model is bimodal.** Stalwart-as-MX is bidirectional, long-running, and user-initiated (operators turn it on; phones connect to it). An RSS poller, an S3 bucket diff, a clipboard watcher are *passive observers* of a source — same `Adapter` contract, but the operator doesn't think of them the same way. The substrate didn't distinguish these; documentation was generic.
2. **OS-platform sensors don't fit one slot.** Putting clipboard + screen + mic under a single `os-events` protocol slot would conflict with the one-platform-per-protocol invariant — only one of the three could run per dPKMS instance. But ambient capture is meaningless if a user can have screen XOR mic, not both.
3. **No user-facing verb for "read everything now."** The original spec described "Universal Capture Layer" as a featureset but no CLI command; existing `ctxt` / `ctxt analyze` / `ctxt ingest` were single-source verbs.

### Amendment decisions

#### Adapter taxonomy: service vs. sensor

The substrate distinguishes two adapter shapes, both implementing the same `internal/adapter.Adapter` contract:

- **Service adapter** — long-running, user-initiated, typically bidirectional. Examples: Stalwart (IMAP/SMTP server + MX), mxhook+gmail (IMAP bridge + send-as), caldav-vdir (CalDAV server). Capabilities typically include `serve`, `submit` in addition to `fetch`. Operators "turn them on" via config; clients (phones, mail apps) connect to them.
- **Sensor** — passive observer of a source, fetch-only or fetch+emit-events. Examples: rss (poll feed URLs), s3-compatible (diff buckets), sshfs/sftp (poll remote file trees), local-fs (watch Downloads/Documents/Photos), watched-fs (fsnotify on configured paths), clipboard (read pasteboard generation), screen (capture screenshot buffer), mic (record window). Capabilities are `fetch` + `emit-events`. Sensors are the substrate side of **ambient capture** (`ctxt capture --ambient`).

**Both shapes use the same Adapter interface — this is a docs/UX distinction, not a type-system one.** Adapter authors don't need to declare which they are; the operator's mental model differs, and our docs reflect that.

#### Protocol slots for OS-platform sensors

OS-platform sensors get **one protocol slot per device kind**, not a single shared slot:

- `internal/adapter/clipboard/slot.go` — `Protocol = "clipboard"`
- `internal/adapter/screen/slot.go` — `Protocol = "screen"`
- `internal/adapter/mic/slot.go` — `Protocol = "mic"`

Per-device slots honor the one-platform-per-protocol invariant literally (one clipboard backend, one screen backend, one mic backend). Ambient capture (§ below) aggregates across all configured slots.

OS-platform sensor adapters **declare a platform constraint** via a new capability shape: `Capability("platform:darwin")`, `Capability("platform:linux")`, etc. Phase 2 ships darwin-only implementations; Phase 3 adds Linux + Windows ports. The Registry's startup check rejects backends whose platform capability doesn't match the running OS.

#### Files slot pulled forward from Phase 4

The `files` protocol slot (originally Phase 4) lands in Phase 2 to host sshfs/sftp + s3-compatible + local-fs sensors. Without it, those sensors have no slot identity. The Phase 4 entry shifts to "External-step adapters only."

#### `ctxt capture` umbrella verb

Phase 2 introduces `ctxt capture` as the canonical user-facing verb for explicit reads from any source the substrate handles. Existing forms (`ctxt`, `ctxt analyze`, `ctxt ingest`) keep working. Full spec: [docs/ctxt/capture.md](../ctxt/capture.md).

The `--ambient` flag invokes a **sweep across all sensors enabled in the user's `policy/ambient.yaml`**. Sensors run in parallel; objects emit through the existing pipeline registry; results route to inbox or active store per `--inbox` flag.

Per-source forms (`ctxt capture <url>`, `ctxt capture ./file`, etc.) target a single adapter resolved via detector chain (URL pattern → adapter slot → backend) or explicit `--source <name>`.

#### Config layout: `policy/ctxt.yaml` + `policy/ambient.yaml`

PR #23's `$XDG_CONFIG_HOME/contexthelp/policies.yaml` relocates to `policy/ctxt.yaml` (tool-namespaced). The new `policy/ambient.yaml` declares per-sensor enablement, OS-permission requirements, and per-sensor config. Boot-time migration handles the relocation; `CTXT_POLICY_FILE` env override is updated. Full schema: [docs/ctxt/ambient.md](../ctxt/ambient.md).

#### Acceptance Gate 4 closed in Phase 2

`kit/runtime/policy.Wire` already runs in the daemon (PR #23 landed it for pipeline events). Phase 2 wires the same engine to the substrate's adapter pre_persisted topics so CEL rules can veto adapter mutations end-to-end. e2e test required.

### Phase 2 sub-tracks

Phase 2 dispatches as four parallel agent streams plus this foundation track. Each agent owns a coherent slice of the substrate; integration PR after all land:

| Track | Scope |
|---|---|
| `dpkms-pluggable-adapters-phase-2-foundations` (this) | ADR amendment, `ctxt capture` spec, `ambient.yaml` schema, `policies.yaml` relocation, Gate 4 substrate wiring |
| `dpkms-sensors-network` (Agent A) | rss, s3-compatible, sshfs/sftp |
| `dpkms-sensors-localfs` (Agent B) | local-fs (Downloads/Documents/Photos), watched-fs |
| `dpkms-services-bidir` (Agent C) | mxhook+gmail, caldav-vdir |
| `dpkms-sensors-osplatform` (Agent D) | clipboard, screen, mic + Gate 4 e2e test |

### Consequences of the amendment

**Positive.**

- Docs match the operator's mental model: service adapters and sensors are described differently because they ARE different in usage even though they share an interface.
- OS-platform sensors compose: ambient capture can include all three (clipboard + screen + mic) without violating the one-platform-per-protocol invariant.
- `ctxt capture --ambient` gives users a single verb for "read everything now" — closes the gap between the "Universal Capture Layer" featureset and what's invokable.
- Files slot earlier means three real sensors (sshfs, s3, local-fs) can land in Phase 2 instead of waiting for Phase 4.

**Negative.**

- Phase 2 scope tripled (from 3 backends to 10). Mitigated by the four-agent split; each agent owns 2–3 backends.
- OS-platform sensors require platform-conditional code from the start. Phase 2 ships darwin-only; Linux + Windows are an explicit Phase 3 deliverable.
- `policy/` directory introduces a config-path break for PR #23 adopters. Boot-time migration is automatic but operators with custom `CTXT_POLICY_FILE` overrides must update them.

**Neutral.**

- The `Capability("platform:<os>")` pattern is precedent-setting. Other capability dimensions may want this shape later (e.g. `Capability("requires:permission:tcc:Microphone")`). Worth watching for over-extension.

---

## References
- [ADR-063 – Graph-Canonical KnowledgeObject](ADR-063-graph-canonical-knowledge-object.md)
- [ADR-064 – Federation: Multi-Instance Object Sync](ADR-064-federation.md)
- kit ADR-0008 (kit/runtime/policy guard engine) — `~/.w/ideacrafterslabs/kit/hops/main/docs/adr/0008-kit-runtime-policy-engine.md`
- kit-primitive-map — `~/.ops/docs/architecture/kit-primitive-map.md`
- Consumer spec: `~/.fam` workspace design — `~/.ops/docs/superpowers/specs/2026-05-05-fam-workspace-design.md`
- Track spec.md (this ADR's WHAT companion): `.tlc/tracks/dpkms-pluggable-adapters/spec.md`
