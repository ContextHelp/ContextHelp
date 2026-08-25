# Device Sync — Identity, Pairing, and Transport Security

> **Date:** 2026-08-25
> **Applies to:** dPKMS, ctxt
> **Extends:** [2026-08-25-device-sync-requirements.md](2026-08-25-device-sync-requirements.md) (§4, §5)
> **References:** ADR-019, ADR-023,
> [2026-08-04-key-lifecycle.md](2026-08-04-key-lifecycle.md),
> [2026-08-04-blob-federation.md](2026-08-04-blob-federation.md),
> [2026-08-25-device-sync-conflict-semantics.md](2026-08-25-device-sync-conflict-semantics.md)

How two devices prove they belong to the same owner and establish a secure channel, with no required third-party service. ADR-023 reserved exactly this slot — "Layer 4: Device Authentication (Future): device pairing for multi-device sync; device-specific keys; trust establishment protocol; device revocation support" — and the key-lifecycle analysis already designed the delegation primitives for federation. **Design rule for this note: reuse those primitives; invent nothing parallel.** An owner is the degenerate case of the key-lifecycle org — a root with a handful of member instances — so device sync and federation share one identity scheme, one fingerprint convention, one revocation mechanism.

## 1. Device identity

### 1.1 Key material

- **Owner root key** — Ed25519, generated on the first device (`dpkms key init` lineage). Signs **only** delegation certificates, roster updates, and revocations — never content, never sync envelopes ([2026-08-04-key-lifecycle.md](2026-08-04-key-lifecycle.md) §1: the org-root/instance split, applied verbatim). Stored in the OS keychain; routed through the `internal/secrets` abstraction (fixing the same gap 7 the key-lifecycle note flags); exportable only as an encrypted backup (age/Argon2id passphrase file — the recovery path if every device is lost).
- **Per-device identity** — two keypairs, both minted on-device, private halves never leaving the device: an **Ed25519 signing key** (signs sync envelopes, roster/departure records) and an **X25519 agreement key** (channel establishment and sealed store-and-forward envelopes). Both public halves live in the device's certificate. Short-lived relative to the root: certs valid 90–365 days, renewed by rotation-with-overlap (key-lifecycle §2).
- **Delegation certificate** — SSH-CA-shaped signed JSON, exactly the key-lifecycle shape with a device-capability vocabulary: `{owner_fingerprint, device_sign_pubkey, device_kem_pubkey, device_name, capabilities, not_before, not_after, serial}`. Capabilities: `sync` (exchange ops/blobs), `admit` (issue certs to new devices), `revoke` (issue revocations). Fingerprints are full SHA-256 hex — System B's convention, per the unification the key-lifecycle note mandates.
- **Verification chain:** envelope → device signing key → delegation cert → owner root. Every peer pins the **owner fingerprint**, not device keys; devices come and go under a stable owner identity.

### 1.2 The roster

The device roster is a **signed, hash-chained, monotone document** (the key-lifecycle rotation-log construction, reused): an append-only sequence of `admit`, `renew`, `depart`, `revoke` records, each carrying the previous record's hash and an HLC, signed by a key whose cert grants the relevant capability. The roster replicates through device sync itself as a special always-first stream, so:

- every device learns roster changes on next contact — no separate distribution channel;
- the roster is the single authority for the three places the protocol needs a peer set: transport authentication (who may connect), device-sequence vectors (whose ops to track), and tombstone-GC quorum (whose acks count — conflict-semantics §3);
- admission and revocation are *auditable events in the corpus's own history*, satisfying requirements S5 (membership is cryptographic) and AC12.

Chain conflicts (two devices concurrently appending roster records) merge like any other op stream — records are self-signed facts, union is safe, HLC orders presentation; the hash-chain forks and re-joins are recorded rather than forbidden (a roster is a grow-set with revocation, not a blockchain).

## 2. Pairing ceremony

### 2.1 First pair (device N+1 = 2)

1. New device generates both keypairs; runs `ctxt device pair`.
2. **Out-of-band channel:** existing device displays a QR code (or, headless, a short code) containing `{owner_fingerprint, one-time pairing secret (≥120 bits), transport hints (LAN addr / hub URL)}`. The QR *is* the trust root of the ceremony — S3's MITM defense is that the secret traveled over a channel the network attacker does not see.
3. **PAKE handshake:** both devices run a balanced PAKE (SPAKE2 class) over the discovered channel, keyed on the pairing secret — yielding a mutually authenticated, encrypted session *without* the secret ever crossing the wire, and with offline-guess resistance if an attacker observed traffic. Headless fallback: short-authenticated-string comparison (6 words displayed on both screens, user confirms).
4. Inside the session: new device sends its public keys + requested name; existing device (holding `admit`) displays name + fingerprint for owner confirmation, then issues the delegation cert, ships the current roster + owner trust anchors, and appends the `admit` record.
5. Initial corpus transfer = the epoch full-resync path (conflict-semantics §3) — pairing is deliberately *not* a special data path, it is "join with empty state."

### 2.2 Device N+1 via any admitting device — and the hub

Any device whose cert carries `admit` can run the ceremony — including the T1 hub, which is just a device with `admit` and an always-reachable address; pairing a phone against the hub over TLS uses the identical ceremony with the QR shown by any already-paired device (the QR's secret authenticates the *new device to the owner*, not to a location). Policy default: `admit` granted to desktop/hub devices, withheld from phones — owner-tunable. Chain depth stays 1 (root signs all certs is the *conceptual* model; operationally the admitting device holds no root key — it forwards a **cert signing request** which the root-holding device countersigns on next contact, with an interim cert marked `provisional` accepted by peers for a bounded window, default 7 days). If the owner keeps the root on exactly one machine, that machine is the only *finalizer* of admissions — a deliberate S5 trade the owner can make; the provisional window keeps usability.

### 2.3 Unpair and revocation

- **Voluntary unpair:** departing device signs a `depart` record (and locally wipes keys + corpus per owner choice); any device propagates it.
- **Stolen device (S1):** owner runs `ctxt device revoke <name>` on any device holding `revoke`; a signed `revoke` record enters the roster stream. Effects at every peer, on receipt: transport handshakes from the revoked keys rejected; envelopes signed by the revoked key with HLC after the revocation record rejected (before: still valid — its history was legitimate); device leaves the GC quorum min() (conflict-semantics §3); relay queues drop its pending envelopes at fetch time.
- **Revocation cascades to un-countersigned descendants.** If the revoked device held `admit`, every cert it issued that is still `provisional` (not yet root-countersigned) is **quarantined** on receipt of the revocation: peers suspend sync with — and drop from GC quorum — each device whose only chain runs through the revoked issuer, pending root disposition. The root holder reviews the pending CSRs and either countersigns (device readmitted, standing restored, its quarantine-period local writes sync normally) or revokes (quarantine hardens into revocation). Root-countersigned descendants are untouched: their chain no longer depends on the issuer.
- **Ops under an expired provisional cert.** A provisional cert that lapses (bounded window passed, never countersigned) does not retro-invalidate history — the revocation rule applies unchanged: ops a peer accepted while the cert was inside its window remain valid corpus history. Envelopes arriving *after* lapse are refused. Undelivered ops on the lapsed device are not lost: after re-admission (same `device_id`, fresh cert) it re-signs and re-sends its batches; seqs are unchanged, so peers holding a prefix dedup by `(device, seq)`.
- **Propagation latency is the honest limit:** revocation is effective per-peer only at next roster contact. The hub shrinks the window materially (always-on distribution point — every connected device learns within its poll interval). State plainly: bytes already on the stolen device are protected only by at-rest encryption (ADR-019 Layer 1/2) and device-OS disk encryption; revocation stops *future participation*, nothing else.
- **Rotation:** device certs renew with overlap windows; owner-root rotation uses the dual-signed, hash-chained, published log from key-lifecycle §2 — the roster simply references the new root fingerprint. Root compromise = out-of-band re-bootstrap (key-lifecycle §4); unrecoverable in-band, by construction.

## 3. Transport

### 3.1 One envelope, four carriers

All tiers carry the same unit: a **sync envelope** — canonically serialized, carrying one or more **origin-signed op batches** (+ tombstones + blob manifest; conflict-semantics defines contents), then encrypted. For live channels, encryption is the channel's (3.2/3.3); for store-and-forward (3.4/3.5), the envelope is additionally sealed per-recipient: payload key wrapped to each recipient device's X25519 key. Envelopes are idempotent to apply (op replay is idempotent) and self-contained — which is what makes the relay and file tiers *correct by construction* rather than degraded modes (AC8, K6 from the survey).

**Attribution unit vs carrier signature.** The signature that satisfies AC12 is per **origin-signed op batch** — a contiguous per-origin seq range signed by the origin device's Ed25519 key — not per individual op and not the outer envelope. Batches stay intact through forwarding, so an envelope from device A can carry device B's batches with B's attribution preserved (store-and-forward and hub-as-relay depend on exactly this). The outer envelope signature authenticates the *carrier*, and whether it is required is a per-transport rule: on live mutually authenticated channels (tiers 1–2) the channel binding already authenticates the carrier, so the envelope signature is redundant and omitted; on store-and-forward (tiers 3–4) there is no channel, so envelope signing **and** per-recipient sealing are mandatory. Per-op signatures are rejected: O(ops) signature volume buys no attribution the batch signature lacks.

| Tier | Carrier | Channel security |
|---|---|---|
| 1 | Direct to hub (internet) | TLS to the hub's endpoint + device-auth inside (3.2) |
| 2 | LAN peer-to-peer | mDNS discovery + Noise-pattern mutual auth on device keys (3.3) |
| 3 | Untrusted relay | per-recipient sealed envelopes; relay sees ciphertext + queue metadata (3.4) |
| 4 | File / sneakernet | sealed envelope(s) written as a signed bundle file (3.5) |

### 3.2 Tier 1 — hub, composing with inbound auth

The hub is a public-bind dpkms instance, so ADR-023's inbound-auth surface applies unchanged for the ordinary API (tokens per ADR-023 Layer 2; transport encryption per ADR-019 Layer 3). Device/sync authentication is **not a bespoke parallel channel**: it is a **device-auth Provider behind the same provider-agnostic `server.auth` seam** as every other inbound authenticator — ADR-023 Layer 4 realized as one more Provider, not a second front door. The Provider authenticates via mTLS on device certs and/or a signed-nonce exchange (server nonce → client signs `nonce ‖ channel-binding` → server verifies cert chain against its roster copy — the handshake is Provider *internals*, invisible above the seam) and yields a standard **Principal** whose metadata carries the device identity: device fingerprint, owner fingerprint, cert serial, and **roster status** (`active` / `provisional` / `quarantined` / `revoked`). From the seam down, sync requests ride the same principal → entitlement → metering → security-event pipeline as every authenticated request (ADR-032 entitlement predicates; metering events; `internal/security/events.go` auth-failure alerting keyed by principal) — sync policy (only active-roster principals exchange; provisional-in-window limited; quarantined and revoked refused) is policy over principal metadata, never transport special-casing. Requirements S6 holds *at the seam*: a bearer-token Principal carries no roster status, so it can never satisfy the replica-membership predicate — and a device Principal is not a substitute for token entitlements elsewhere. Devices always dial the hub (NAT asymmetry — the same reason federation is push-only, [2026-08-04-blob-federation.md](2026-08-04-blob-federation.md) Option B's direction argument); "hub pushes" is expressed as the device holding a long-poll/stream open.

### 3.3 Tier 2 — LAN

- **Discovery:** mDNS/DNS-SD service `_dpkms-sync._tcp`, TXT carrying a *rotating pairwise-derivable* identifier (HMAC of owner fingerprint + coarse time window) rather than the raw owner fingerprint — LAN observers shouldn't get a stable owner identifier for free. Discovery is a hint only (requirements S3): nothing discovered is trusted until the handshake proves roster membership.
- **Channel:** Noise-pattern handshake (XX class) with the device keys as statics — mutual authentication, forward secrecy, and identity hiding from passive observers; roster check on the peer's static before any payload. TLS-with-pinned-raw-keys is an acceptable implementation substitute if a vetted Noise dependency is not; the *requirements* are mutual auth on roster keys, PFS, and no CA involvement. On the accepting side, the verified Noise static feeds the same device-auth Provider path as tier 1 — one Principal shape, one policy pipeline, regardless of carrier.
- **Resumption:** session tickets for fast re-handshake across sleep/IP changes; protocol state needs none of it — the sequence-vector exchange is stateless-resumable (an interrupted transfer redelivers ops the peer already acked; replay is idempotent).

### 3.4 Tier 3 — untrusted relay

A deliberately minimal, self-hostable queue: `POST /q/{recipient-pseudonym}` (append envelope), `GET /q/{pseudonym}` (drain), auth via per-relay pseudonymous tokens established at configuration. The relay learns sizes, timing, and pseudonyms — the metadata leak requirements S2 accepts and documents — and can drop or delay (liveness, never integrity: envelopes are signed and sealed). Any dpkms instance can serve the relay endpoints; **the hub doubles as relay** for device pairs that cannot reach each other directly (answering requirements open question 2: yes — envelopes are E2E sealed regardless, so relaying through the hub grants it nothing it lacks as a replica; for the rarer owner who wants store-and-forward *without* a hub replica, the same endpoints run on any small host holding no corpus).

### 3.5 Tier 4 — file

`ctxt sync export --for <device>` writes sealed envelopes as a signed bundle (the `internal/bundle` machinery, extended exactly as the blob-federation note's knowledge-pack direction anticipates); `ctxt sync import` verifies signature chain against the roster, decrypts, applies. Covers air-gapped moves and is the recovery import path for epoch resync.

### 3.6 Blobs across devices — requirement only

Full design belongs to the blob-federation work; device sync states its requirements and inherits the mechanism ([2026-08-04-blob-federation.md](2026-08-04-blob-federation.md) Option C + B): blob keys are **content addresses** (SHA-256 of stored bytes), so cross-device dedup is a `check`-manifest handshake and every received blob is verifiable by re-hash; transfer is sender-driven within the authenticated channel (blobs before rows — the ordering rule); **lazy fetch is a per-device policy**: metadata and rows always sync, bytes fetch on demand or by class (phone: on-demand; desktop/hub: mirror-all). A device holding rows without bytes reports degraded content status rather than dangling refs.

## 4. Honest limits

1. **A paired device is trusted.** Compromise of an unrevoked device is compromise of the replica set's write authority (requirements S7) — bounded by attribution (signatures), the audit log, and revocation; not prevented.
2. **Revocation is next-contact, not instant** (§2.3). The window is operationally small with a hub and unbounded for a fully offline mesh; the CLI must show per-device "last seen / revocation delivered" so the owner can see the window instead of guessing.
3. **Relay metadata** (sizes/timing/pseudonyms) leaks activity patterns to a relay operator; mitigations (padding, batching) are cheap partial measures, listed as tuning, not guarantees.
4. **Root loss without the encrypted backup** means no new admissions/revocations — existing devices keep syncing (certs remain valid until expiry), so the failure is soft and detectable; re-bootstrap re-pairs the fleet under a new root.

## Handoff to the ADR

Identity: owner-root → device-cert delegation (shared with federation key lifecycle, one fingerprint convention). Membership: signed hash-chained roster, replicated in-band, feeding transport auth, sequence vectors, and GC quorum. Pairing: PAKE/QR ceremony, admit capability, provisional certs, hub-as-ordinary-device. Revocation: roster records, next-contact semantics, quarantine cascade for un-countersigned descendants, honest limits stated. Transport: origin-signed op batches in one envelope over four carriers (carrier signature per transport); device auth is a Provider behind the `server.auth` seam (ADR-023 Layer 4), sharing the principal → entitlement → metering → security-event pipeline; the hub doubles as relay; blobs content-addressed and lazy per policy.
