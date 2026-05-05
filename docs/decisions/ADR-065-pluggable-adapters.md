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

- **Phase 1** — Substrate (`internal/adapter/`) + email + contacts protocol slots + migration of cardamum/himalaya from `internal/ingest/`. Stalwart and mxhook backends out of scope for Phase 1.
- **Phase 2** — Stalwart backend (bundled MX) + mxhook backend (Gmail bridge first). Calendar slot + caldav-vdir backend.
- **Phase 3** — Additional mxhook backends (iCloud, M365, Proton, Fastmail). Reminders, accounts, bills slots.
- **Phase 4** — Files slot. External-step adapters (per ADR-058) for adapter authors who want a separate-binary path.

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

## References

- [ADR-058 – External Step Execution Protocol](ADR-058-external-step-execution-protocol.md)
- [ADR-063 – Graph-Canonical KnowledgeObject](ADR-063-graph-canonical-knowledge-object.md)
- [ADR-064 – Federation: Multi-Instance Object Sync](ADR-064-federation.md)
- kit ADR-0008 (kit/runtime/policy guard engine) — `~/.w/ideacrafterslabs/kit/hops/main/docs/adr/0008-kit-runtime-policy-engine.md`
- kit-primitive-map — `~/.ops/docs/architecture/kit-primitive-map.md`
- Consumer spec: `~/.fam` workspace design — `~/.ops/docs/superpowers/specs/2026-05-05-fam-workspace-design.md`
- Track spec.md (this ADR's WHAT companion): `.tlc/tracks/dpkms-pluggable-adapters/spec.md`
