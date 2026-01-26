# ADR-024 – Self-Authenticating Knowledge (Signatures and Trust)

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** dPKMS
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

In a decentralized, federated knowledge system where content flows between registries, users, and devices, establishing trust and provenance becomes critical. When knowledge is shared, synced, or imported, users need assurance about:

**Authenticity Problems:**
- Did this object come from the claimed source?
- Has the content been tampered with since creation?
- Can I trust this registry's taxonomy data?
- Is this entity definition canonical or modified?
- Was this export bundle created by me or someone else?

**Integrity Problems:**
- Has the object been modified in transit?
- Is this the same version I exported previously?
- Are mentions and links still valid after import?
- Has registry data been corrupted?

**Provenance Problems:**
- Who created this knowledge originally?
- What pipeline produced these enrichments?
- Which registry provided this taxonomy?
- When was this object last modified and by whom?

**Trust Problems:**
- Should I accept objects from this unknown source?
- Can I trust unsigned content from registries?
- Is this plugin from a verified developer?
- Should automatic sync trust remote changes?

**Sharing Problems:**
- How to share knowledge with verifiable origin?
- How to prove I authored specific content?
- How to detect tampering after sharing?
- How to establish trust chains in federated networks?

**Design Constraints:**
- Must remain optional (not all users need signatures)
- Must work offline (no online verification dependency)
- Must support multiple signature algorithms
- Must not break existing unsigned content
- Must preserve privacy (don't force identity disclosure)
- Must support both symmetric (HMAC) and asymmetric (Ed25519) signatures
- Must enable trust policies (auto-accept, prompt, reject)
- Must integrate with export/import system

**Affected Subsystems:**
- Object storage (signature metadata)
- Export/import (bundle signing and verification)
- Registry protocol (signed taxonomy/entity data)
- Plugin system (plugin signature verification)
- Pipeline provenance (step attestation)
- Sync protocol (signed changesets)

**Goals:**
- Enable verifiable knowledge provenance
- Detect tampering and corruption
- Support trust policies for automation
- Facilitate secure knowledge sharing
- Preserve user sovereignty (optional signatures)
- Enable federated trust without central authority

---

## Decision

**dPKMS will implement an optional self-authenticating knowledge model supporting cryptographic signatures for objects, exports, registry data, and plugins, with configurable trust policies, multiple signature algorithms, and full verification workflows, while maintaining backward compatibility with unsigned content and preserving user privacy.**

The signature and trust system provides:

1. **Signature Levels:**

   **Level 1: No Signatures (Default)**
   - Existing behavior preserved
   - No signature generation or verification
   - Local trust only (filesystem permissions)
   - Suitable for single-user, local-only use

   **Level 2: HMAC Signatures (Symmetric)**
   - Shared secret-based signing
   - Fast generation and verification
   - Suitable for self-verification (export/import)
   - Device-to-device trust with shared key
   - No public verification possible

   **Level 3: Public Key Signatures (Asymmetric)**
   - Ed25519 signatures (default)
   - Public verification possible
   - Suitable for sharing and federation
   - Establishes identity-based trust
   - Key distribution required

2. **Signature Configuration:**
   ```yaml
   signatures:
     enabled: true
     mode: none | hmac | ed25519 | pgp

     # Key management
     keys:
       # Symmetric key (HMAC)
       hmac:
         key_derivation: pbkdf2 | argon2id
         key_source: passphrase | keyfile | env
         key_id: hmac-key-v1

       # Asymmetric key (Ed25519)
       ed25519:
         private_key: ~/.config/ctxt/keys/id_ed25519
         public_key: ~/.config/ctxt/keys/id_ed25519.pub
         key_id: ed25519-key-v1

       # PGP (future)
       pgp:
         keyring: ~/.gnupg/pubring.kbx
         key_id: 0x1234ABCD

     # What to sign
     sign:
       objects:
         enabled: true
         include_content: true
         include_metadata: true
       exports:
         enabled: true
         mandatory: false  # Fail export if signing fails
       registry_data:
         enabled: true
         verify_on_sync: true
       plugins:
         enabled: true
         require_verified: false  # Reject unsigned plugins

     # Trust policies
     trust:
       # Object trust
       objects:
         unsigned: accept | prompt | reject
         self_signed: accept
         unknown_signer: prompt | reject

       # Registry trust
       registries:
         unsigned_taxonomy: accept | warn | reject
         unsigned_entities: accept | warn | reject
         unknown_signer: prompt | reject

       # Plugin trust
       plugins:
         unsigned: prompt | reject
         unknown_signer: prompt | reject
         trusted_publishers:
           - key_id: ed25519-publisher-ctxt-official
             name: "ContextHelp Official"
           - key_id: ed25519-publisher-community
             name: "Community Verified"

       # Auto-accept from known signers
       known_signers:
         - key_id: ed25519-key-my-device-2
           name: "My Laptop"
           trust_level: full
         - key_id: ed25519-key-coworker
           name: "Alice"
           trust_level: partial  # Only specific content types

     # Verification behavior
     verification:
       automatic: true  # Verify on import/sync
       strict: false    # Reject invalid signatures vs warn
       cache_results: true
       expiry_check: true
   ```

3. **Object Signature Schema:**

   **Signed Object Storage:**
   ```sql
   -- Signature metadata for objects
   CREATE TABLE object_signatures (
       id UUID PRIMARY KEY,
       object_id UUID NOT NULL,
       algorithm TEXT NOT NULL,  -- hmac-sha256, ed25519, pgp
       key_id TEXT NOT NULL,
       signature BYTEA NOT NULL,
       signed_at TIMESTAMP NOT NULL,
       signer_identity TEXT,  -- Optional: "user@example.com" or handle
       metadata JSONB,
       FOREIGN KEY(object_id) REFERENCES objects(id),
       UNIQUE(object_id, algorithm)
   );

   CREATE INDEX idx_signatures_object ON object_signatures(object_id);
   CREATE INDEX idx_signatures_key ON object_signatures(key_id);
   ```

   **Object JSON with Signature:**
   ```json
   {
     "id": "uuid",
     "type": "bookmark",
     "raw_content": "...",
     "created_at": "2026-01-26T10:00:00Z",
     "signature": {
       "algorithm": "ed25519",
       "key_id": "ed25519-key-v1",
       "signature": "base64-encoded-signature",
       "signed_at": "2026-01-26T10:00:00Z",
       "signer": "user@example.com",
       "includes": ["id", "type", "raw_content", "created_at"]
     }
   }
   ```

4. **Signature Generation:**

   **HMAC Signing (Symmetric):**
   ```go
   func SignObjectHMAC(obj Object, key []byte) (Signature, error) {
       // Canonical JSON serialization (deterministic field order)
       payload := canonicalJSON(obj)

       // HMAC-SHA256
       mac := hmac.New(sha256.New, key)
       mac.Write(payload)
       sig := mac.Sum(nil)

       return Signature{
           Algorithm: "hmac-sha256",
           KeyID:     deriveKeyID(key),
           Signature: sig,
           SignedAt:  time.Now(),
       }, nil
   }

   func VerifyObjectHMAC(obj Object, sig Signature, key []byte) error {
       expectedSig := SignObjectHMAC(obj, key)
       if !hmac.Equal(sig.Signature, expectedSig.Signature) {
           return ErrInvalidSignature
       }
       return nil
   }
   ```

   **Ed25519 Signing (Asymmetric):**
   ```go
   func SignObjectEd25519(obj Object, privateKey ed25519.PrivateKey) (Signature, error) {
       // Canonical JSON serialization
       payload := canonicalJSON(obj)

       // Ed25519 signature
       sig := ed25519.Sign(privateKey, payload)

       return Signature{
           Algorithm:      "ed25519",
           KeyID:          keyID(privateKey.Public()),
           Signature:      sig,
           SignedAt:       time.Now(),
           SignerIdentity: "user@example.com",  // Optional
       }, nil
   }

   func VerifyObjectEd25519(obj Object, sig Signature, publicKey ed25519.PublicKey) error {
       payload := canonicalJSON(obj)

       if !ed25519.Verify(publicKey, payload, sig.Signature) {
           return ErrInvalidSignature
       }
       return nil
   }
   ```

5. **Export Bundle Signing:**

   **Signed Export Format:**
   ```json
   {
     "version": 1,
     "exported_at": "2026-01-26T10:00:00Z",
     "exporter": "ctxt v1.0.0",
     "objects": [...],
     "entities": [...],
     "signature": {
       "algorithm": "ed25519",
       "key_id": "ed25519-key-v1",
       "signature": "base64-signature-of-entire-bundle",
       "signed_at": "2026-01-26T10:00:00Z",
       "signer": "user@example.com",
       "public_key": "base64-encoded-public-key"  // Optional: embed for verification
     }
   }
   ```

   **Bundle Signature Verification Flow:**
   ```bash
   # Import signed bundle
   ctxt import bundle.ctxt

   # Output:
   Verifying bundle signature...
   Signature algorithm: ed25519
   Signer: user@example.com
   Signed at: 2026-01-26 10:00:00
   Signature valid: ✓

   Import bundle? [Y/n]:
   ```

6. **Registry Signature Verification:**

   **Signed Taxonomy Response:**
   ```json
   {
     "taxonomy": {
       "tags": [...],
       "version": "2026-01-26"
     },
     "signature": {
       "algorithm": "ed25519",
       "key_id": "uxpatterns-registry-key",
       "signature": "base64-signature",
       "signed_at": "2026-01-26T09:00:00Z",
       "signer": "registry@uxpatterns.io"
     }
   }
   ```

   **Verification on Sync:**
   ```go
   func SyncRegistryTaxonomy(registry Registry) error {
       resp, err := fetchTaxonomy(registry.URL)
       if err != nil {
           return err
       }

       if resp.Signature != nil {
           // Verify signature
           publicKey, err := getRegistryPublicKey(registry.Name)
           if err != nil {
               return handleUntrustedRegistry(registry, resp)
           }

           if err := verifySignature(resp, publicKey); err != nil {
               return fmt.Errorf("invalid registry signature: %w", err)
           }
       } else if config.Trust.Registries.UnsignedTaxonomy == "reject" {
           return fmt.Errorf("unsigned taxonomy rejected by policy")
       }

       return storeTaxonomy(resp.Taxonomy)
   }
   ```

7. **Plugin Signature Verification:**

   **Signed Plugin Package:**
   ```
   plugin.tar.gz
   plugin.tar.gz.sig  (detached signature)

   Or embedded:
   plugin.ctxt.signed:
     metadata:
       name: price-monitor
       version: 1.0.0
       author: "@example"
     signature:
       algorithm: ed25519
       key_id: publisher-key
       signature: base64
     payload: base64(plugin.tar.gz)
   ```

   **Installation Verification:**
   ```bash
   ctxt plugin install price-monitor

   # Output:
   Verifying plugin signature...
   Publisher: ContextHelp Official
   Key ID: ed25519-publisher-ctxt-official
   Signature valid: ✓

   Plugin 'price-monitor' requests the following permissions:
   [...]

   Approve? [y/N]:
   ```

8. **Trust Policy Enforcement:**

   **Automated Trust Decisions:**
   ```go
   type TrustDecision int

   const (
       TrustAccept TrustDecision = iota
       TrustPrompt
       TrustReject
   )

   func (tm *TrustManager) DecideImportObject(obj Object, sig *Signature) TrustDecision {
       // No signature
       if sig == nil {
           return tm.policy.Objects.Unsigned  // accept, prompt, or reject
       }

       // Self-signed (same key as local)
       if sig.KeyID == tm.localKeyID {
           return TrustAccept
       }

       // Known trusted signer
       if signer, ok := tm.knownSigners[sig.KeyID]; ok {
           if signer.TrustLevel == TrustLevelFull {
               return TrustAccept
           }
           return TrustPrompt  // Partial trust → confirm
       }

       // Unknown signer
       return tm.policy.Objects.UnknownSigner  // prompt or reject
   }
   ```

   **User Prompt for Unknown Signer:**
   ```bash
   Unknown signer for object:
   Key ID: ed25519-unknown-abc123
   Signer: alice@example.com
   Signature: valid

   Trust this signer? [y/N/always]:
   ```

9. **Key Management:**

   **Key Generation:**
   ```bash
   # Generate Ed25519 key pair
   ctxt signature keygen --type ed25519 --name "My Device"

   # Output:
   Private key: ~/.config/ctxt/keys/id_ed25519
   Public key: ~/.config/ctxt/keys/id_ed25519.pub
   Key ID: ed25519-key-v1

   Public key (for sharing):
   ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILq...
   ```

   **Key Import/Export:**
   ```bash
   # Export public key for sharing
   ctxt signature pubkey --format ssh > my-pubkey.txt

   # Import trusted signer's public key
   ctxt signature trust add alice-pubkey.txt --name "Alice" --level full

   # List trusted signers
   ctxt signature trust list

   # Revoke trust
   ctxt signature trust revoke ed25519-key-alice
   ```

   **Key Storage:**
   ```
   ~/.config/ctxt/keys/
     id_ed25519           # Private key (600 permissions)
     id_ed25519.pub       # Public key
     trusted/             # Trusted signers' public keys
       alice.pub
       registry-uxpatterns.pub
   ```

10. **CLI Signature Commands:**
    ```bash
    # Signature management
    ctxt signature status
    ctxt signature keygen --type ed25519
    ctxt signature pubkey
    ctxt signature sign <object-id>
    ctxt signature verify <object-id>

    # Trust management
    ctxt signature trust add <pubkey-file> --name <name> --level <full|partial>
    ctxt signature trust list
    ctxt signature trust revoke <key-id>

    # Batch operations
    ctxt signature sign-all
    ctxt signature verify-all

    # Export with signature
    ctxt export --sign --output bundle.ctxt

    # Import with verification
    ctxt import bundle.ctxt --verify
    ```

11. **Provenance Chain:**

    **Pipeline Step Attestation:**
    ```json
    {
      "object_id": "uuid",
      "pipeline": "url.generic",
      "steps": [
        {
          "step": "fetch",
          "completed_at": "2026-01-26T10:00:00Z",
          "signature": {
            "algorithm": "hmac-sha256",
            "signature": "base64",
            "includes": ["object_id", "step", "input", "output"]
          }
        },
        {
          "step": "enrich",
          "provider": "openai:gpt-4",
          "completed_at": "2026-01-26T10:01:00Z",
          "signature": {
            "algorithm": "hmac-sha256",
            "signature": "base64"
          }
        }
      ]
    }
    ```

    **Provenance Verification:**
    ```bash
    ctxt provenance verify <object-id>

    # Output:
    Provenance chain for object abc-123:
    1. fetch (2026-01-26 10:00:00) ✓ verified
    2. enrich (2026-01-26 10:01:00) ✓ verified
    3. summarize (2026-01-26 10:02:00) ✗ signature invalid
    ```

---

## Rationale

### Alternatives Considered

#### 1. **Mandatory Signatures for All Objects (Rejected)**
Require signatures on every object automatically.

**Rejected because:**
- Unnecessary overhead for local-only use
- Key management burden for all users
- Performance impact on ingestion
- Violates simplicity for default case
- Optional signatures sufficient

#### 2. **X.509 Certificates and PKI (Rejected)**
Use X.509 certificates with certificate authority chain.

**Rejected because:**
- Excessive complexity for use case
- Requires certificate management infrastructure
- Central authority dependency (CA)
- Overkill for peer-to-peer trust
- Ed25519 simpler and sufficient

#### 3. **Blockchain-Based Trust (Rejected)**
Use blockchain for immutable provenance ledger.

**Rejected because:**
- Requires network connectivity
- Performance overhead unacceptable
- Complexity vastly exceeds benefit
- Central chain or consensus required
- Local signatures sufficient for goals

#### 4. **Timestamp Authority Integration (Rejected)**
Use external timestamp authority for signature timestamps.

**Rejected because:**
- Network dependency for signing
- Central authority required
- Adds complexity and cost
- Local timestamps sufficient
- Violates offline-first principle

#### 5. **Automatic Signing of Everything (Rejected)**
Sign all objects, exports, registry data automatically.

**Rejected because:**
- Performance overhead
- Key management complexity
- Not all content needs signatures
- User should opt in consciously
- Optional with policies better

### Benefits of Chosen Approach

**Flexibility:**
- Optional signatures (opt-in)
- Multiple algorithms supported
- Symmetric and asymmetric options
- Per-content-type policies

**Sovereignty:**
- User controls signing keys
- No central authority
- Offline key generation
- Self-verification possible

**Trust Without Centralization:**
- Direct key-based trust
- No certificate authority needed
- Peer-to-peer verification
- Federated trust model

**Performance:**
- HMAC for fast self-verification
- Ed25519 for efficient asymmetric signing
- Lazy verification (on demand)
- Signature caching

**Provenance:**
- Full chain of custody
- Pipeline step attestation
- Tamper detection
- Audit trail

### Drawbacks / Risks

**Key Management Burden:**
- Users must manage keys
- Key loss means inability to sign
- Key backup essential
- Distribution complexity for sharing

**Trust Bootstrap Problem:**
- Initial key exchange requires out-of-band channel
- No automatic trust establishment
- Manual trust decisions required
- Key fingerprint verification needed

**Performance Overhead:**
- Signature generation cost (minimal but nonzero)
- Verification on every import
- Canonical JSON serialization required
- Storage overhead for signatures

**Complexity:**
- Trust policy configuration
- Algorithm selection
- Key rotation procedures
- Signature verification logic

---

## Consequences

### Positive

**Verifiable Provenance:**
- Detect tampering reliably
- Establish knowledge origin
- Audit trail complete
- Trust chains traceable

**Secure Sharing:**
- Share knowledge with proof of origin
- Detect modifications in transit
- Establish author identity
- Build reputation systems possible

**Federated Trust:**
- No central authority required
- Peer-to-peer verification
- Registry authenticity verifiable
- Plugin source verification

**Backward Compatible:**
- Unsigned content still works
- Gradual adoption possible
- No breaking changes
- Interoperates with old versions

### Negative

**Key Management Complexity:**
- Users manage keys manually
- Key distribution for trust
- Backup and recovery procedures
- Rotation complexity

**Performance Impact:**
- Signing overhead on creation
- Verification overhead on import
- Canonical serialization cost
- Storage overhead (~100 bytes/sig)

**User Experience Friction:**
- Trust prompts for unknown signers
- Key generation/management steps
- Policy configuration complexity
- Signature failure handling

### Neutral / Considerations

**Key Distribution:**
- Out-of-band key exchange needed
- QR codes for device pairing
- Email/chat for sharing public keys
- Web-of-trust model possible

**Algorithm Evolution:**
- Ed25519 may become obsolete
- Need migration path to new algorithms
- Multi-signature support future-proofs
- Algorithm negotiation per context

**Registry Adoption:**
- Public registries may not sign initially
- Commercial registries likely will
- Mixed signed/unsigned ecosystem
- Policies handle gracefully

**Trust Transitivity:**
- Alice trusts Bob, Bob trusts Carol → ?
- Web-of-trust model not implemented initially
- Direct trust only (no transitive)
- Future enhancement possible

---

## Implementation Notes

### Core Components

**Signature Manager (`dPKMS/signatures/`):**
```go
type SignatureManager interface {
    // Key operations
    GenerateKey(algorithm string) (*KeyPair, error)
    ImportPublicKey(keyData []byte, name string) error
    ListTrustedKeys() ([]*TrustedKey, error)
    RevokeTrust(keyID string) error

    // Signing operations
    SignObject(obj Object, algorithm string) (*Signature, error)
    SignExport(bundle ExportBundle, algorithm string) (*Signature, error)

    // Verification operations
    VerifyObject(obj Object, sig Signature) error
    VerifyExport(bundle ExportBundle, sig Signature) error
}

type Signature struct {
    Algorithm      string
    KeyID          string
    Signature      []byte
    SignedAt       time.Time
    SignerIdentity string
    Metadata       map[string]interface{}
}

type KeyPair struct {
    Algorithm  string
    PrivateKey []byte
    PublicKey  []byte
    KeyID      string
}

type TrustedKey struct {
    KeyID      string
    PublicKey  []byte
    Name       string
    TrustLevel TrustLevel
    AddedAt    time.Time
}
```

**Trust Manager (`dPKMS/trust/`):**
```go
type TrustManager struct {
    policy       TrustPolicy
    knownSigners map[string]*TrustedKey
    localKeyID   string
}

type TrustPolicy struct {
    Objects    ObjectTrustPolicy
    Registries RegistryTrustPolicy
    Plugins    PluginTrustPolicy
}

type ObjectTrustPolicy struct {
    Unsigned      TrustDecision
    SelfSigned    TrustDecision
    UnknownSigner TrustDecision
}

func (tm *TrustManager) DecideImport(obj Object, sig *Signature) TrustDecision
func (tm *TrustManager) PromptUserTrust(sig *Signature) (bool, error)
func (tm *TrustManager) AddTrustedKey(key *TrustedKey) error
```

**Canonical Serialization (`dPKMS/canonical/`):**
```go
// Deterministic JSON serialization for signing
func CanonicalJSON(v interface{}) ([]byte, error) {
    // 1. Sort object keys alphabetically
    // 2. No whitespace
    // 3. Escape special characters consistently
    // 4. Deterministic float representation
    return json.Marshal(normalizeForSigning(v))
}
```

**Storage Schema:**
```sql
-- Trusted public keys
CREATE TABLE trusted_keys (
    id UUID PRIMARY KEY,
    key_id TEXT NOT NULL UNIQUE,
    public_key BYTEA NOT NULL,
    algorithm TEXT NOT NULL,
    name TEXT NOT NULL,
    trust_level TEXT NOT NULL,  -- full, partial
    added_at TIMESTAMP NOT NULL,
    added_by TEXT,
    notes TEXT
);

CREATE INDEX idx_trusted_keys_id ON trusted_keys(key_id);

-- Signature verification cache
CREATE TABLE signature_verifications (
    id UUID PRIMARY KEY,
    object_id UUID NOT NULL,
    signature_id UUID NOT NULL,
    verified_at TIMESTAMP NOT NULL,
    result TEXT NOT NULL,  -- valid, invalid, unknown_key
    verifier TEXT,
    FOREIGN KEY(object_id) REFERENCES objects(id)
);

CREATE INDEX idx_verifications_object ON signature_verifications(object_id);
```

### Integration Points

**With Export/Import:**
1. Sign export bundles automatically if configured
2. Verify signatures on import
3. Prompt user for unknown signers
4. Log verification results

**With Registry Protocol:**
1. Verify registry taxonomy/entity signatures
2. Cache verified registry data
3. Trust policy enforcement per registry
4. Fallback for unsigned registries

**With Plugin System:**
1. Verify plugin signatures before installation
2. Reject unsigned plugins if policy requires
3. Check trusted publishers list
4. Display verification status to user

**With Provenance System:**
1. Sign each pipeline step
2. Chain signatures for multi-step provenance
3. Verify provenance on demand
4. Detect pipeline tampering

### Migration Strategy

**Phase 1: Object Signatures (Skeleton 8)**
- Ed25519 key generation and management
- Object signing and verification
- Basic trust policy enforcement
- CLI signature commands

**Phase 2: Export/Import Signing (Skeleton 8)**
- Bundle signature generation
- Bundle verification on import
- Trust prompts for unknown signers
- Signature metadata in bundles

**Phase 3: Registry and Plugin Verification (Skeleton 9)**
- Registry signature verification
- Plugin signature verification
- Trusted publisher management
- Policy-based auto-accept/reject

**Phase 4: Advanced Provenance (Skeleton 10+)**
- Pipeline step attestation
- Provenance chain verification
- Web-of-trust model
- Multi-signature support

**Backward Compatibility:**
- Signatures optional (default off)
- Unsigned content coexists with signed
- Existing exports/imports work unchanged
- No schema migrations required (additive only)

### Testing Requirements

**Unit Tests:**
- Ed25519 signing and verification
- HMAC signing and verification
- Canonical JSON serialization
- Trust policy decisions

**Integration Tests:**
- End-to-end export/import with signatures
- Registry signature verification
- Plugin signature verification
- Trust prompt workflows

**Security Tests:**
- Signature forgery attempts
- Canonical serialization consistency
- Key fingerprint collisions
- Timing attack resistance

**Performance Tests:**
- Signing overhead measurement
- Verification latency
- Canonical serialization performance
- Batch signature verification

---

## References

- **architecture.md:135-146** – Trusted principle (Skeleton 8)
- **ROADMAP.md** – Skeleton 8: Trust That Travels
- **dpkms/security.md:49-74** – Registry trust model
- ADR-019 – Encryption and Privacy (key derivation)
- ADR-020 – Export/Import Contract (signed bundles)
- ADR-023 – Authentication and Authorization (credential security)

**Related Documents:**
- `dPKMS/signatures/` – Signature implementation (to be created)
- `dPKMS/trust/` – Trust manager (to be created)
- `dPKMS/canonical/` – Canonical serialization (to be created)

**External References:**
- Ed25519: https://ed25519.cr.yp.to/
- RFC 8032 (EdDSA): https://datatracker.ietf.org/doc/html/rfc8032
- HMAC RFC: https://datatracker.ietf.org/doc/html/rfc2104
- Canonical JSON: https://gibson042.github.io/canonicaljson-spec/

---
