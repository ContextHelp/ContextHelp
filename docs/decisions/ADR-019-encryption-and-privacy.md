# ADR-019 – Encryption and Privacy Model

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** dPKMS
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Knowledge systems handle sensitive personal and professional information. Users need strong privacy guarantees:

**Data-at-Rest Risk:**
- Knowledge base contains sensitive information (credentials, personal notes, business strategy)
- Local storage accessible to other processes or users
- Backups may be stored on untrusted media
- Device loss or theft exposes all knowledge
- No protection against physical access

**Data-in-Transit Risk:**
- Registry queries expose search patterns
- Remote API calls leak content for enrichment
- Export bundles transmitted without protection
- Collaboration features require sharing sensitive data
- Network interception possible

**Search Privacy:**
- Full-text indexes expose content in plaintext
- Vector embeddings leak semantic information
- Query patterns reveal interests and activities
- No way to search encrypted content

**Key Management Burden:**
- Users struggle with key generation and storage
- Lost keys mean lost data forever
- Key rotation complex and error-prone
- Multi-device sync requires key distribution
- No recovery mechanism for forgotten keys

**Performance Trade-offs:**
- Encryption adds computational overhead
- Search on encrypted data is slow
- Index building requires decryption
- Balance security vs usability

**Constraints:**
- Must remain optional (not all users need encryption)
- Must support selective encryption (some objects, not all)
- Must enable search on encrypted data (privacy-preserving)
- Must integrate with existing storage backends
- Must support key rotation without data loss
- Must work offline (no online key escrow)
- Must be auditable (verify encryption is working)

**Affected Subsystems:**
- Storage layer (encryption at rest)
- Query engine (encrypted search)
- Export/import (encrypted bundles)
- Registry connectors (encrypted transit)
- Key management (generation, storage, rotation)
- Backup/restore (encrypted backups)

**Goals:**
- Protect knowledge from unauthorized access
- Enable search on encrypted content
- Simplify key management for users
- Support selective encryption by profile or sensitivity
- Maintain performance at acceptable levels
- Preserve sovereignty (user controls keys, not vendor)

---

## Decision

**dPKMS will implement an optional, layered encryption model with pluggable encryption providers, user-controlled key management, privacy-preserving search capabilities, and support for both full-database and selective object encryption, while maintaining acceptable performance and usability.**

The encryption system provides:

1. **Encryption Layers:**

   **Layer 1: Storage Backend Encryption**
   - Full database encryption (SQLite/Postgres level)
   - Transparent to application layer
   - Protects against physical access
   - Managed by storage backend

   **Layer 2: Application-Level Encryption**
   - Per-object encryption
   - Selective encryption by profile or tag
   - Field-level encryption (e.g., raw_content only)
   - Managed by dPKMS

   **Layer 3: Transport Encryption**
   - TLS for all network communications
   - Registry queries encrypted
   - API calls encrypted
   - Export bundle encryption

2. **Encryption Configuration:**
   ```yaml
   encryption:
     enabled: true
     mode: full | selective | off

     # Full database encryption
     storage_encryption:
       enabled: true
       provider: sqlcipher | pgcrypto | native
       key_derivation:
         method: pbkdf2 | argon2id
         iterations: 100000
         salt_size_bytes: 32

     # Application-level encryption
     object_encryption:
       enabled: true
       provider: age | nacl | aes-gcm
       fields:
         - raw_content
         - summaries
         - sections
         - metadata.sensitive_keys
       exclude_fields:
         - id
         - created_at
         - type
         - tags  # for filtering

     # Selective encryption rules
     selective_rules:
       - name: sensitive-profile
         match:
           profile: ["founder", "personal"]
         encrypt: true

       - name: sensitive-tags
         match:
           tags: ["confidential", "private", "personal"]
         encrypt: true

       - name: entity-based
         match:
           entities: ["@credential.*", "@secret.*"]
         encrypt: true

     # Privacy-preserving search
     encrypted_search:
       enabled: true
       method: blind_index | ste | ore
       index_fields:
         - tags
         - entities
         - hints

     # Key management
     key_management:
       provider: local | keychain | kms
       key_rotation:
         enabled: true
         interval_days: 90
         retain_old_keys: true
   ```

3. **Key Management:**

   **Key Derivation from Passphrase:**
   ```
   Master Key = PBKDF2(passphrase, salt, 100000 iterations, SHA256)
   or
   Master Key = Argon2id(passphrase, salt, memory=64MB, iterations=3)

   Per-Object Key = HKDF(Master Key, object_id, context="object")
   Search Index Key = HKDF(Master Key, "search", context="index")
   Export Key = HKDF(Master Key, export_id, context="export")
   ```

   **Key Storage Options:**

   **Local Keyring (Default):**
   ```
   ~/.config/ctxt/keyring/
     master.key.enc  # Encrypted with passphrase
     salt            # For key derivation
     metadata.json   # Key rotation history
   ```

   **OS Keychain Integration:**
   ```
   macOS: Keychain Access
   Linux: Secret Service API (gnome-keyring, kwallet)
   Windows: Credential Manager
   ```

   **Hardware Security Module (Optional):**
   ```
   YubiKey, Nitrokey, TPM chip
   Keys never leave hardware
   Signing and encryption in hardware
   ```

4. **Privacy-Preserving Search:**

   **Blind Index Approach:**
   ```
   For each searchable term:
   1. Generate term hash: H = HMAC(Search Index Key, term)
   2. Store hash in index (not plaintext term)
   3. At query time:
      a. Hash query terms with same key
      b. Search for matching hashes
      c. Decrypt matched objects for display

   Pros:
   - Fast search (hash lookup)
   - No plaintext in index
   - Deterministic (same term → same hash)

   Cons:
   - Frequency analysis possible
   - Known plaintext attacks if terms guessable
   - No fuzzy matching
   ```

   **Searchable Encryption (Advanced):**
   ```
   Use Order-Revealing Encryption (ORE) or
   Structured Encryption (STE) schemes

   Pros:
   - Range queries possible
   - Stronger security guarantees

   Cons:
   - Slower performance
   - Complex implementation
   - Limited library support
   ```

5. **Selective Encryption:**

   **Rule-Based Selection:**
   ```json
   {
     "object_id": "uuid",
     "encrypted": true,
     "encryption_rule": "sensitive-profile",
     "encrypted_fields": ["raw_content", "summaries"],
     "plaintext_fields": ["id", "type", "tags", "mentions"],
     "encryption_metadata": {
       "algorithm": "age",
       "key_id": "key-fingerprint",
       "encrypted_at": "timestamp"
     }
   }
   ```

   **Mixed-Mode Storage:**
   ```
   Objects with sensitive tags → encrypted
   Objects in personal profile → encrypted
   Objects with public tags → plaintext
   Objects shared to registries → plaintext (or encrypted with shared key)
   ```

6. **Key Rotation:**

   **Rotation Process:**
   ```
   1. Generate new master key
   2. Mark old key as "rotated" (retain for decryption)
   3. Re-encrypt all objects with new key (background job)
   4. Update keyring metadata
   5. After all objects re-encrypted, archive old key
   ```

   **Rotation Triggers:**
   - Time-based (every 90 days)
   - Compromise suspected (manual trigger)
   - Device change (optional auto-rotation)
   - Export to untrusted medium

   **Key History:**
   ```json
   {
     "keys": [
       {
         "id": "key-v1",
         "created_at": "2024-01-01",
         "rotated_at": "2024-04-01",
         "status": "archived",
         "objects_encrypted": 0
       },
       {
         "id": "key-v2",
         "created_at": "2024-04-01",
         "status": "active",
         "objects_encrypted": 1542
       }
     ]
   }
   ```

7. **Encrypted Export/Import:**

   **Export Format:**
   ```
   bundle.ctxt.enc:
     header:
       version: 1
       encryption: age
       key_derivation: argon2id
       salt: base64
       objects_count: int
       encrypted_at: timestamp
     payload:
       encrypted_data: base64  # Contains JSON bundle
       signature: base64 (optional)
   ```

   **Import Flow:**
   ```
   1. Read encrypted bundle
   2. Prompt for passphrase
   3. Derive decryption key
   4. Decrypt payload
   5. Verify signature (if present)
   6. Import objects (re-encrypt with local key)
   ```

8. **CLI Interface:**
   ```bash
   # Enable encryption
   ctxt encryption enable
   ctxt encryption enable --passphrase-stdin < passphrase.txt
   ctxt encryption enable --selective --rules sensitive-only

   # Key management
   ctxt encryption passphrase change
   ctxt encryption key rotate
   ctxt encryption key status

   # Encrypt existing data
   ctxt encryption migrate --from plaintext --to encrypted
   ctxt encryption migrate --selective --rules sensitive-tags

   # Verify encryption
   ctxt encryption verify
   ctxt encryption status

   # Export with encryption
   ctxt export --encrypt --passphrase-stdin < passphrase.txt
   ctxt import bundle.ctxt.enc
   ```

---

## Rationale

### Alternatives Considered

#### 1. **No Encryption, Rely on OS/Disk Encryption (Rejected)**
Trust OS-level full disk encryption (FileVault, BitLocker, LUKS).

**Rejected because:**
- No protection if device unlocked
- No selective encryption
- No protection for backups on cloud storage
- No control over key management
- Export bundles unprotected

#### 2. **Mandatory Encryption for All (Rejected)**
Force encryption on for all users and all objects.

**Rejected because:**
- Performance penalty for users who don't need it
- Key management burden for everyone
- Search performance degradation
- Collaboration complexity (shared keys required)
- Violates simplicity for common case

#### 3. **Cloud-Based Key Escrow (Rejected)**
Store keys with vendor for recovery.

**Rejected because:**
- Violates sovereignty principle
- Trust required in third party
- Single point of failure
- Privacy concerns (vendor can decrypt)
- Network dependency

#### 4. **Homomorphic Encryption for Search (Rejected)**
Use fully homomorphic encryption to search without decryption.

**Rejected because:**
- Extremely slow (orders of magnitude)
- Limited operation support
- Immature library ecosystem
- Impractical for real-time search

#### 5. **Per-Field Encryption Only (Rejected)**
Encrypt only specific sensitive fields, not full objects.

**Rejected because:**
- Metadata leakage (tags, types, dates visible)
- Complex to determine what to encrypt
- Doesn't protect against database dump analysis
- Partial solution, false sense of security

### Benefits of Chosen Approach

**Layered Defense:**
- Multiple encryption layers
- Defense in depth
- Failure of one layer doesn't compromise all

**User Control:**
- Optional (users choose)
- Selective (encrypt what matters)
- Passphrase-based (user controls key)
- No vendor lock-in

**Privacy-Preserving Search:**
- Can search encrypted content
- Acceptable performance trade-off
- Frequency analysis harder with blind index

**Key Sovereignty:**
- User owns keys completely
- No key escrow
- Offline key management
- Export/import preserves encryption

**Performance Balance:**
- Selective encryption minimizes overhead
- Storage-level encryption is transparent
- Search index optimization
- Lazy decryption on demand

**Extensibility:**
- Pluggable encryption providers
- Can adopt better algorithms later
- Support for HSM/hardware keys
- Future: homomorphic encryption when practical

### Drawbacks / Risks

**Key Loss = Data Loss:**
- Forgotten passphrase means permanent data loss
- No recovery mechanism without backup
- User education critical
- Scary for non-technical users

**Performance Overhead:**
- Encryption/decryption adds latency
- Search on encrypted data slower
- Index building more expensive
- Memory overhead for key caching

**Complexity:**
- Key management is hard
- Rotation logic is complex
- Selective encryption rules confusing
- Debugging encrypted data difficult

**Search Limitations:**
- Blind index doesn't support fuzzy search
- Frequency analysis still possible
- Known plaintext attacks if terms predictable
- No semantic search on encrypted embeddings

---

## Consequences

### Positive

**Strong Privacy:**
- Data protected at rest and in transit
- Device loss doesn't expose knowledge
- Selective encryption for sensitive content
- User controls keys completely

**Sovereignty Preserved:**
- No vendor key escrow
- Offline key management
- Export remains encrypted
- User owns encryption completely

**Flexible Security:**
- Optional (users choose level)
- Selective (encrypt what matters)
- Layered (multiple protection levels)
- Configurable (rules-based)

**Search Privacy:**
- Can search without full decryption
- Blind index protects content
- Acceptable performance trade-off
- Frequency analysis mitigated

### Negative

**Key Management Burden:**
- Users must manage passphrases
- Key loss = data loss forever
- Rotation requires coordination
- Multi-device sync complex

**Performance Impact:**
- Encryption overhead ~10-30%
- Search slower on encrypted data
- Index building expensive
- Memory overhead for keys

**Implementation Complexity:**
- Multiple encryption layers
- Key rotation logic
- Selective encryption rules
- Privacy-preserving search
- Extensive testing required

**Search Limitations:**
- No fuzzy search on encrypted data
- Frequency analysis still possible
- Semantic search on embeddings limited
- Range queries harder

### Neutral / Considerations

**Backup Strategy:**
- Encrypted backups require key backup
- Key loss means backup useless
- Cloud backup of encrypted data safer
- Document backup procedures

**Multi-Device Sync:**
- Keys need secure distribution
- Passphrase sync vs separate keys
- Key rotation across devices
- Conflict resolution

**Collaboration:**
- Shared keys for shared knowledge?
- Separate encryption domains per collaboration
- Registry encryption compatibility
- Export/import key exchange

**Migration Path:**
- Plaintext → encrypted migration
- Encrypted → plaintext (if needed)
- Selective rule changes
- Re-encryption background job

---

## Implementation Notes

### Core Components

**Encryption Layer (`dPKMS/encryption/`):**
```go
type EncryptionProvider interface {
    Name() string
    Encrypt(plaintext []byte, key []byte) ([]byte, error)
    Decrypt(ciphertext []byte, key []byte) ([]byte, error)
    KeySize() int
}

// Built-in providers
type AgeProvider struct { ... }
type NaClProvider struct { ... }
type AESGCMProvider struct { ... }
```

**Key Manager (`dPKMS/keymanager/`):**
```go
type KeyManager interface {
    DeriveKey(passphrase string) ([]byte, error)
    GetObjectKey(objectID string) ([]byte, error)
    GetSearchKey() ([]byte, error)
    RotateKeys() error
    KeyStatus() (*KeyStatus, error)
}

type KeyStatus struct {
    CurrentKeyID      string
    CreatedAt         time.Time
    RotatedAt         time.Time
    ObjectsEncrypted  int
    RotationScheduled bool
}
```

**Selective Encryption Engine:**
```go
type EncryptionRuleEngine struct {
    rules []EncryptionRule
}

type EncryptionRule struct {
    Name    string
    Match   MatchCriteria
    Encrypt bool
}

func (e *EncryptionRuleEngine) ShouldEncrypt(obj Object) bool
```

**Privacy-Preserving Search:**
```go
type BlindIndex struct {
    searchKey []byte
}

func (bi *BlindIndex) IndexTerm(term string) string {
    return HMAC(bi.searchKey, term)
}

func (bi *BlindIndex) Search(query string) []string {
    hashedQuery := bi.IndexTerm(query)
    return db.Query("SELECT * FROM blind_index WHERE hash = ?", hashedQuery)
}
```

**Storage Schema Extensions:**
```sql
-- Object encryption metadata
ALTER TABLE objects ADD COLUMN encrypted BOOLEAN DEFAULT FALSE;
ALTER TABLE objects ADD COLUMN encryption_key_id TEXT;
ALTER TABLE objects ADD COLUMN encryption_algorithm TEXT;
ALTER TABLE objects ADD COLUMN encrypted_at TIMESTAMP;

-- Blind index for encrypted search
CREATE TABLE blind_index (
    id UUID PRIMARY KEY,
    object_id UUID NOT NULL,
    field_name TEXT NOT NULL,
    term_hash TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY(object_id) REFERENCES objects(id),
    INDEX(term_hash),
    INDEX(object_id)
);

-- Key rotation tracking
CREATE TABLE encryption_keys (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL,
    rotated_at TIMESTAMP,
    status TEXT NOT NULL,  -- active, rotated, archived
    objects_encrypted INT DEFAULT 0,
    algorithm TEXT NOT NULL
);
```

### Integration Points

**With Storage Layer:**
1. Storage backend may provide native encryption
2. Application layer encrypts before storage write
3. Decryption on read if object encrypted
4. Selective field encryption support

**With Query Engine:**
1. Blind index used for encrypted search
2. Query terms hashed before lookup
3. Results decrypted before return
4. Fallback to full scan if index miss

**With Export/Import:**
1. Bundle encryption with passphrase
2. Key derivation for export key
3. Signature for integrity verification
4. Re-encryption on import with local key

**With Profile System:**
1. Profile-based encryption rules
2. Profile switch may change encryption status
3. Profile-scoped key management (future)

### Migration Strategy

**Phase 1: Storage Backend Encryption (Skeleton 7)**
- SQLCipher integration for SQLite
- pgcrypto for PostgreSQL
- Passphrase-based key derivation
- Basic key management

**Phase 2: Application-Level Encryption (Skeleton 8)**
- Per-object encryption
- Selective encryption rules
- Field-level encryption
- CLI encryption commands

**Phase 3: Privacy-Preserving Search (Skeleton 8)**
- Blind index implementation
- Encrypted search support
- Index building for encrypted objects
- Performance optimization

**Phase 4: Advanced Features (Skeleton 9+)**
- Key rotation automation
- HSM/hardware key support
- Structured encryption (STE/ORE)
- Homomorphic encryption exploration

**Backward Compatibility:**
- Encryption disabled by default
- Plaintext objects coexist with encrypted
- Migration command for existing data
- No breaking changes to API

### Testing Requirements

**Unit Tests:**
- Encryption/decryption correctness
- Key derivation
- Blind index generation
- Selective rule matching

**Integration Tests:**
- End-to-end encrypted ingestion
- Encrypted search accuracy
- Key rotation without data loss
- Export/import with encryption

**Security Tests:**
- Ciphertext indistinguishability
- Key derivation strength (timing attack resistance)
- Side-channel resistance
- Metadata leakage assessment

**Performance Tests:**
- Encryption overhead measurement
- Search on encrypted data benchmarks
- Key rotation performance
- Memory usage with key caching

### Performance Optimization

**Key Caching:**
- Cache derived object keys in memory
- TTL-based eviction
- Secure memory wiping on eviction

**Lazy Decryption:**
- Decrypt only when accessed
- Preview metadata without decryption
- Selective field decryption

**Blind Index Optimization:**
- Bloom filters for term existence
- Batch index updates
- Incremental index building

**Hardware Acceleration:**
- AES-NI CPU instructions
- GPU acceleration for batch operations
- SIMD for key derivation

---

## References

- **architecture.md:1238-1268** – Security & Privacy specification
- **dpkms/security.md** – Comprehensive security model
- **dpkms/privacy.md** – Privacy guarantees (to be created)
- **ROADMAP.md** – Skeleton 8: Trust That Travels (encryption features)
- ADR-001 – Local-First and Decentralized (sovereignty principle)
- ADR-006 – Storage backends (encryption integration)

**Related Documents:**
- `dPKMS/encryption/` – Encryption implementation (to be created)
- `dPKMS/keymanager/` – Key management (to be created)
- `dPKMS/privacy-search/` – Privacy-preserving search (to be created)

**External References:**
- SQLCipher: https://www.zetetic.net/sqlcipher/
- Age encryption: https://age-encryption.org/
- Argon2: https://github.com/P-H-C/phc-winner-argon2
- Blind Index: https://paragonie.com/blog/2017/05/building-searchable-encrypted-databases-with-php-and-sql
- Searchable Encryption: https://eprint.iacr.org/2016/920.pdf

---
