# Key Lifecycle Under Federation — Signing, Trust, and the Missing Revocation Story

> **Date:** 2026-08-04
> **Applies to:** dPKMS, ctxt
> **Extends:** [2026-08-04-storage-federation-addendum.md](2026-08-04-storage-federation-addendum.md) (concern 5: "Key lifecycle outgrows the keychain")

The federation addendum flagged that Ed25519-with-OS-keychain is right for individuals and insufficient for organizations running many instances. This document maps the current signing/verification machinery end-to-end with file evidence, enumerates the lifecycle gaps precisely, and proposes a key lifecycle protocol — org→instance delegation, rotation with overlap, and revocation propagation riding the existing registry-sync loop.

## Current state: two signing systems, one keypair

There are two independent Ed25519 uses in the codebase today. They share primitives but not keys, fingerprints, or trust models.

### System A — signed config bundles (instance signs its own exports)

- **Key generation.** `bundle.GenerateKeyPair` (`internal/bundle/bundle.go:60-70`) creates a raw Ed25519 pair. Fingerprint = hex of the **first 8 bytes** of SHA-256(pubkey) (`bundle.go:73-76`).
- **Key storage.** Private key → OS keychain via shelling out to `security` (macOS) or `secret-tool` (Linux) under the hardcoded service/account `ctxt-signing`/`ed25519-private-key` (`internal/bundle/keystore.go:18-68`, constants at `bundle.go:29-32`; Windows variant in `keystore_windows.go`). Public key → `<configDir>/keys/<fp>.pub` as hex (`bundle.go:98-109`). Entry point: `dpkms key init` (`cmd/dpkms/cmd/key.go:68-102`).
- **What gets signed.** `bundle.Build` (`bundle.go:175-246`) zips config files, embeds a `manifest.json` containing the **public key itself** (`bundle.go:210-216`), then writes a detached `.sig` sidecar containing the raw signature over the zip bytes.
- **Verification.** `bundle.VerifyAndOpen` (`bundle.go:303-378`) verifies the sidecar against the caller-supplied key, **falling back to the key embedded in the manifest** (`bundle.go:342-355`). With the default fallback, verification proves only integrity (the zip matches its own embedded key), not authenticity — any attacker can produce a self-consistent bundle. Optional AES-256-GCM/Argon2id encryption wraps the zip (`internal/bundle/encrypt.go:1-12`).
- **Rotation.** `dpkms key rotate` → `bundle.RotateKeys` (`internal/bundle/rotation.go:102-155`): generates a new pair, appends a **dual-signed** transition entry (old key and new key both sign a canonical `{old_key,new_key,timestamp}` message, `rotation.go:79-128`) to a local `rotation_log.json`, and archives the old key. Verification of an old bundle walks the chain via `FingerprintInChain` (`rotation.go:204-235`), invoked from `VerifyAndOpen` (`bundle.go:361-375`).

### System B — signed registry sync responses (registry signs what subscribers pull)

- **Key declaration.** The registry's manifest carries an optional `public_key` (hex Ed25519) field (`internal/storage/types.go:278-282`). There is no key generation on this side in-repo — the registry operator brings a key.
- **What gets signed.** Sync response bodies — specifically the entity index — via a `Content-Signature` header or a `.sig` sidecar fetched alongside (`internal/registry/signature.go:53-124`, sidecar fetch at `signature.go:126-154`).
- **Verification.** `Syncer.fetchAndVerifyEntityIndex` (`internal/registry/sync.go:464-556`) reads the public key from the **cached manifest**, verifies, and records the outcome as `TrustStatus` ∈ {unknown, verified, failed} plus the key fingerprint on the registry cache row (`internal/storage/types.go:284-294`, `316-324`). Fingerprint here is the **full** SHA-256 hex (`signature.go:44-51`) — a different convention from System A's 8-byte prefix.
- **Enforcement knob.** `registries_global.require_signatures` (`internal/config/config.go:488-493`), **default false** (`config.go:883-884`). When off, a registry with no `public_key` syncs unverified; when a key *is* declared, a missing or bad signature aborts the sync (`sync.go:524-545`).
- **Orthogonal trust layer.** `TrustGate` (`internal/registry/trust.go:41-68`) gates entity *writes* by the per-registry `trust_level` config (trusted/untrusted/sandboxed, `config.go:526-539`). This is an authorization policy, not cryptography — it never consults `TrustStatus` or any key.

### How trust is actually established

Nowhere. System B is **TOFU without pinning**: the verification key is delivered in the registry's own manifest, fetched over the same HTTPS channel as the content it authenticates. Signature verification therefore proves "this response matches the key this registry currently claims," which HTTPS already gave us. Worse, nothing checks key *continuity*: the cached `KeyFingerprint` is overwritten on every successful verification (`sync.go:547-551`), so a registry (or an attacker who controls its manifest endpoint) can swap keys silently between syncs and every subscriber follows along. System A's manifest-embedded-key default has the same shape.

## Lifecycle gaps

**1. Rotation is local-only and one-sided.** The rotation log is a file on the signer's disk. It is never published, and System B has no rotation concept at all — a registry key change is indistinguishable from a key compromise, and both are silently accepted. The dual-signature transition record (`rotation.go:120-128`) is exactly the right primitive; it just never leaves the machine.

**2. Old signatures verify forever — by design.** `FingerprintInChain` deliberately keeps every superseded key valid so old bundles still open. There is no validity window, no expiry on keys or signatures, and no way to say "key X was superseded *because it leaked*; reject everything after date D." Rotation without revocation extends the attack surface rather than shrinking it: compromise of any key ever in the chain still forges acceptable bundles.

**3. There is no revocation anywhere.** No revocation record type, no distribution channel, no subscriber-side check. `RegistryTrustFailed` records that *the last response* failed — key state and response state are conflated. The addendum's phrasing holds literally: verification without revocation is a promise that cannot be taken back.

**4. Rotation archives the old private key in plaintext.** `RotateKeys` writes the outgoing private key as hex to `keysDir/signing.key.bak.<ts>` (`rotation.go:140-147`), mode 0600 but outside the keychain the design otherwise insists on. If the rotation was prompted by suspected compromise, this is exactly backwards.

**5. The staleness check watches a file that never exists.** `SigningKeyAge` stats `keysDir/signing.key` (`rotation.go:159-166`), but `key init` puts the private key in the keychain and writes only `<fp>.pub` — no `signing.key` is ever created. The 365-day rotation-overdue warning (`cmd/dpkms/cmd/key.go:108-116`) can therefore never fire on the standard path.

**6. No org identity, no delegation, no multi-instance story.** One keypair per machine under one fixed keychain account (`bundle.go:30-32`) — an instance cannot even hold two identities, and two instances of the same org are cryptographically unrelated strangers. A subscriber trusting "Acme Corp" has no object to trust; it can only trust ten unrelated instance keys individually, each with an independent (nonexistent) lifecycle.

**7. Assorted seams.** Two fingerprint conventions (8-byte vs full SHA-256) will collide the moment bundle keys and registry keys meet in one trust store. The rotation log has per-entry signatures but no chaining or whole-log signature, so entry deletion/truncation is undetectable. Key handling bypasses the pluggable secrets layer (`internal/secrets/kit.go`) that already abstracts keyring/agefile/1password/ghsecrets — an org that keeps secrets in 1Password cannot keep signing keys there.

## Proposed key lifecycle protocol

The design goal, consistent with the federation addendum: keep storage and instances boring, put lifecycle complexity in signed documents that flow through the **existing pull-based sync protocol**. No new network services, no OCSP-style online checks, no PKI. Prior art borrowed deliberately: TUF (roles, expiry, freshness), OpenSSH CA (minimal delegation certs + KRLs), minisign/signify (sign-new-key-with-old rotation practice), Sigstore (noted, deferred).

### 1. Identity: org root → instance delegation

Introduce a two-tier identity, optional for individuals (an instance key may be its own root — the current model degrades gracefully):

- **Org root key.** Long-lived, ideally kept offline or in the org's secret backend of choice (route through `internal/secrets`, fixing gap 7). Signs only *delegation certificates* and *revocations* — never content.
- **Instance key.** Per-instance, short-lived (90–365 days), lives in the OS keychain exactly as today. Signs sync responses and bundles.
- **Delegation certificate.** A small signed JSON object, SSH-CA-shaped rather than X.509: `{org_fingerprint, instance_pubkey, instance_name, capabilities, not_before, not_after, serial}`, signed by the org root. The registry manifest's `public_key` field (`internal/storage/types.go:278-282`) grows into a `signing` block: `{org_key, certs: [...], revocations_url?}`. Subscribers verify content → instance key → cert → org root, and **pin the org fingerprint**, not the instance key.

This makes "many instances, one identity" a first-class object, gives every instance key a built-in expiry (fixing gap 2's open-ended validity), and reduces instance-key compromise from "re-establish trust with every subscriber" to "org signs one revocation and one new cert."

### 2. Rotation with overlap windows

- **Instance keys** rotate by the org issuing a new cert whose `not_before` precedes the old cert's `not_after` by an overlap window (default 30 days). During overlap both keys verify; signing switches to the new key immediately. No subscriber coordination needed — expiry does the retirement.
- **Org root** rotation reuses the existing dual-signature transition record (`rotation.go:24-29` is already the right shape), but the log becomes a **published, hash-chained document**: each entry carries the hash of its predecessor, and the whole log is served with the manifest. Chaining fixes the truncation gap; publication fixes gap 1. Rule, borrowed from TUF root rotation: a new root is valid only if signed by the previous root (or by a threshold of previous roots, if the org opts into `m`-of-`n`).
- **Verification-time semantics** change from "fingerprint is somewhere in the chain" to "signature was made by a key whose cert was valid *at signing time*, and neither key nor cert is revoked." Bundles and responses should embed a signed timestamp for this; absent one, `created_at` in the bundle manifest (`bundle.go:42-44`) is the fallback.
- Unify fingerprints on full SHA-256 (System B's convention) during this migration; retire `SigningKeyAge` in favor of cert expiry, which actually fires.

### 3. Revocation distribution — piggyback on registry sync

Subscribers already poll registries on a schedule, compare ETags, and surface prioritized reminders with dismissal state and action URLs (the update-notification model, ADR-057). Revocation rides the same loop rather than inventing a channel:

- **Revocation list.** A compact signed document (KRL-spirited, not a full CRL): `{org_fingerprint, serial, revoked: [{fingerprint, serial?, revoked_at, reason}], issued_at, expires_at}`, signed by the org root, served next to the manifest and covered by the same ETag change detection.
- **Freshness is mandatory.** `expires_at` (default 7–30 days, re-issued on every registry publish even when unchanged) is the TUF timestamp-role trick: without it, an attacker who can serve stale documents freezes the revocation state and gap 3 reopens. An expired revocation list is treated per-registry policy — warn under the permissive default, hard-fail under `require_signatures`.
- **Subscriber behavior.** On sync: refresh the list; a revoked fingerprint flips the cached registry to a new `TrustStatus` value `revoked` (extending `types.go:284-294`), aborts the sync, and raises a high-priority, `action_required` reminder through the existing reminders table — precisely the "step removed" severity path. Content previously synced under a now-revoked key is flagged, not silently purged (same posture as the addendum's retraction discussion: revocation of trust and retraction of content are separate propagation problems).
- **Key continuity enforcement** (fixes the TOFU hole independent of revocation): once a subscriber has verified against an org fingerprint, a manifest presenting a different org key is **rejected** unless accompanied by a valid rotation-log entry linking old→new. Silent key swaps become loud failures with a reminder attached.

### 4. Compromise recovery

- **Instance key compromised:** org publishes revocation (effective within one sync interval + list freshness window) and a replacement cert. Subscribers need no action beyond their normal poll. This is the case the whole design optimizes for, because it is the common one.
- **Org root compromised:** unrecoverable in-band by construction — an attacker holding the root can sign rotations. Recovery is out-of-band re-bootstrap (publish new root fingerprint via a channel the attacker doesn't control; subscribers re-pin explicitly), which is TUF's answer too. Orgs that cannot tolerate this adopt the threshold option from §2 so a single key loss is not root loss.
- **Housekeeping fixes now, independent of the protocol:** stop archiving rotated private keys in plaintext (encrypt the archive with the new key's keychain material, or store through `internal/secrets`); flip `require_signatures` to default-true once org identities exist; route all private-key material through the secrets backend abstraction.

### What is deliberately not proposed

A transparency log (Sigstore/Rekor) would give subscribers proof against equivocation — an org showing different revocation lists to different subscribers. It is the right eventual answer for *public* registries and directly serviced by the addendum's ownership-anchored model, but it requires witness infrastructure that contradicts local-first for now. The hash-chained rotation log is designed to be forward-compatible with later anchoring into such a log. Likewise X.509/PKI is rejected outright: the delegation needs of this system are three fields past a raw key, and SSH-CA-style certs deliver that without ASN.1.

## Sequencing

1. Unify fingerprints; fix plaintext key archival and the dead staleness check; route keys through `internal/secrets`. (No protocol change.)
2. Key continuity pinning + loud-failure on key change. (Subscriber-only change, immediate TOFU improvement.)
3. Delegation certs + `signing` manifest block, individuals defaulting to self-rooted. (Wire format change, backward-compatible via the existing bare `public_key`.)
4. Published hash-chained rotation log + signed revocation list with freshness, wired into sync and the reminders table.
5. Threshold roots and transparency-log anchoring, when public multi-org federation is real.

Steps 1–2 are cheap and shrink today's exposure; 3–4 are the actual federation prerequisite. As with retraction propagation, this belongs in the protocol *before* public instances exist — revocation semantics cannot be retrofitted onto subscribers one does not control.
