# ADR-085 – Encryption at Rest and Search Indexing

> **Status:** Accepted
> **Date:** 2026-11-24
> **Author:** jadb
> **Applies to:** dPKMS storage (SQLite, PostgreSQL, BlobStore, snapshots, ambient spool, backups)
> **Supersedes:** ADR-019 (storage encryption & blind-index sections)
> **References:** ADR-019 (encryption and privacy), ADR-021 (multi-backend storage), ADR-022 (vector semantic search), ADR-070 (pipeline and index versioning), ADR-071 (embedding index versioning), ADR-080 (sharing and per-viewer visibility)

---

## Context

ADR-019 proposed selective per-object encryption paired with blind indexes for keyword search. That storage design was never implemented and collides structurally with the storage substrate that actually shipped:

- **SQLite** runs on a single canonical CGO build (`CGO_ENABLED=1 -tags fts5`, cross-compiled to `windows/amd64` via mingw-w64) pairing `github.com/mattn/go-sqlite3` with `github.com/asg017/sqlite-vec-go-bindings/cgo`. Full-text search lives in external-content `FTS5` virtual tables (`objects_fts`, `objects_fts_masked`), and semantic search lives in `vec0` (`vec_objects`) plus `embeddings`. Plain `mattn/go-sqlite3` provides no page encryption.
- **PostgreSQL** runs via `github.com/lib/pq` with `pgvector` and `tsvector` GIN indexes. Open-source PostgreSQL ships no built-in transparent data encryption (TDE): encrypting content columns leaves a plaintext `tsvector` leaking personal-information lexemes in `pg_dump` or disk copies, whereas dropping `tsvector` removes keyword search on encrypted rows.
- **Non-database disk surfaces** hold plaintext copies alongside the primary database: local `BlobStore` files (`datadir/blobs/` and `.meta.json` sidecars), SQLite pre-migration snapshots (`backups/pre-migrate-*.sqlite`), the local ambient event spool (`$XDG_STATE_HOME/ctxt/ambient/events/`), SQLite `-wal`/`-shm` sidecars, and unencrypted backup archives.
- **Blind indexes have no semantic-search analog.** Exact-token HMAC indexes do not support substring/stemming (`FTS5`/`tsvector`), phrase queries, or dense vector cosine similarity (`vec0`/`pgvector`).

Deployments handling personal information (such as Quebec Law 25 environments) require that a raw disk copy, stolen laptop, or unencrypted backup cannot expose stored knowledge, while remaining honest about what search capabilities survive under each encryption boundary.

---

## Decision

Supersede ADR-019's unimplemented selective-encryption and blind-index storage model with a layered, fail-closed at-rest architecture anchored on a single `KeyProvider` port.

### 1. Key Custody (`internal/keys.KeyProvider`)

Key-encryption keys (KEKs) are never stored beside the database or blob directory. All wrapping and unwrapping runs through a fail-closed `KeyProvider` port (`Current`, `Wrap`, `Unwrap`, `Rewrap`, `Health` with associated data binding):

- **Local / laptop deployments:** OS keychain (`keyring`) or `age` identity (`agefile`) via `internal/secrets`, unwrapped into `dpkms` process memory only.
- **Server / multi-user deployments:** Client-owned OpenBao or Vault Transit engine (`derived=true`, token from environment only — `dpkms` never holds the KEK), or an operator-mounted `0600`/`0400` keyfile supplied outside the database and backup volumes (e.g. `systemd LoadCredential` or tmpfs secret mount).
- **PostgreSQL TDE deployments:** Infrastructure/KMS holds the volume or tablespace key separately from `dpkms`.

When encryption is enabled and the `KeyProvider` is unconfigured, unreachable, or fails authentication, `dpkms` fails closed at startup and on every read/write — never silently falling back to plaintext or creating a fresh unencrypted store.

### 2. SQLite: Whole-Database Page Encryption

SQLite uses **whole-database encryption** (encrypting the main `.sqlite` file, `-wal`, and `-shm` pages transparently inside the unlocked envelope):

- **Searchability:** `FTS5`, `vec0`, metadata filters, and `BEGIN IMMEDIATE` transactions operate unchanged inside the unlocked process while leaving zero plaintext headers (`"SQLite format 3"`), tokens, summaries, or vectors on disk.
- **Driver selection:** Empirically validated by spike before production schema/build integration across `sqlite3mc` (SQLite3MultipleCiphers, zero OpenSSL dep), `ncruces/go-sqlite3` VFS cipher + `sqlite-vec` WASM bindings, and SQLCipher. The spike gates on `FTS5` + `vec0` capability probes, cross-compilation (`darwin`, `linux`, `windows/amd64`), zero-plaintext byte scans on `.sqlite`/`-wal`/`-shm`, fail-closed wrong-key open, and plaintext-to-encrypted migration + `rekey`.
- **Key wrapping:** The database master key (DEK) is wrapped by `KeyProvider` and stored in a small versioned key envelope beside the store metadata; `dpkms` unwraps the DEK in memory on startup and applies it to every pooled connection (including read-only handles and snapshot opens).

### 3. Non-Database Disk Surfaces

Every on-disk artifact outside the primary database is protected by the same `KeyProvider`:

- **BlobStore (`internal/storage/blob/encrypted`):** Decorator wrapping any `storage.BlobStore` (local filesystem and S3/BYOB). Each blob receives a random 32-byte DEK wrapped under `KeyProvider` with AAD `"blob:<key>"`; payload and `.meta.json` are sealed with AES-256-GCM. Content-addressed keys, `CountBlobRefs`, `DeleteObject`, and `Sweep` remain unchanged.
- **Pre-migration snapshots (`backups/pre-migrate-*.sqlite`):** Never written in plaintext when store encryption is enabled.
- **Ambient event spool:** Sealed on disk with `KeyProvider` AEAD envelope; decrypted on drain.
- **Backup bundles (`internal/bundle/encrypt.go`):** Mandatory encryption when store encryption is enabled (unencrypted backup refused unless explicitly overridden and audited).

### 4. PostgreSQL: Configurable Orthogonal Controls (`require_tde`, `column_encryption`, `fts_on_encrypted`)

Because PostgreSQL has no universal built-in TDE and deployments vary between self-hosted clusters (`pg_tde`/LUKS) and managed services (RDS/Neon), PostgreSQL exposes three orthogonal settings under `storage.postgres`:

| Setting | Values | Default | Effect |
|---|---|---|---|
| `require_tde` | `bool` | `false` | Startup and `dpkms doctor` require TLS (`sslmode=require`/`verify-full`) and attested TDE/encrypted storage (`pg_tde` extension/tablespace or explicitly attested encrypted volume); fail closed otherwise. |
| `column_encryption` | `off \| sensitive \| all` | `off` | App-layer AEAD envelope via `KeyProvider` (per-row wrapped DEK, AAD `"pg:<table>:<id>:<col>"`) sealing `objects.{raw_content,text_content,summaries,metadata,graph_json}`, `attachments.data`, and `jobs.payload`. |
| `fts_on_encrypted` | `omit \| require_tde` | `omit` | `omit`: encrypted rows store an empty `tsvector` (no plaintext PI lexemes in DB or `pg_dump`; search uses `pgvector` + tags/type/mentions). `require_tde`: allowed only when `require_tde=true`; rejected at startup otherwise. |

This supports three honest deployment postures without silent leakage:

1. **TDE-only (`require_tde=true`, `column_encryption=off`):** Protects raw disk/WAL/indexes against disk or snapshot theft while retaining full `tsvector` + `pgvector` search; does not protect against a compromised DB role or unencrypted logical `pg_dump`.
2. **Envelope-only (`require_tde=false`, `column_encryption=sensitive|all`, `fts_on_encrypted=omit`):** Works on any managed PostgreSQL; protects content, attachments, and jobs even against logical `pg_dump` or a rogue DBA; honestly carves encrypted rows out of `tsvector` keyword search (semantic search via `pgvector` and structural filters remain available).
3. **Both (`require_tde=true`, `column_encryption=sensitive|all`, `fts_on_encrypted=require_tde|omit`):** Defense in depth — disk/WAL/indexes protected by TDE and content/blobs/attachments protected by app-layer keys held outside PostgreSQL.

### 5. Searchability Honesty

- **SQLite (whole-DB encryption):** Full `FTS5`, `vec0`, and RSQL search work identically to an unencrypted database once unlocked; zero search degradation.
- **PostgreSQL (`fts_on_encrypted=omit`):** Encrypted objects are explicitly excluded from `tsvector` keyword matching; `dpkms doctor` and search diagnostics state this carve-out rather than silently returning missing hits.
- **Searchable encryption (ORE/STE/blind indexes):** Explicitly rejected for the current architecture due to incompatibility with stemming, phrase search, and vector similarity, plus well-known frequency-analysis leakage.

---

## Rejected Alternatives

- **Application-level selective encryption on SQLite (ADR-019):** Either leaves `objects_fts` and `vec_objects` plaintext on disk (failing at-rest confidentiality) or excludes the user's most important notes from local search. Whole-DB encryption solves both cleanly on SQLite.
- **Requiring `pg_tde` unconditionally on PostgreSQL:** Excludes managed PostgreSQL hosts (RDS, Neon, Cloud SQL) where operators cannot install custom storage extensions.
- **Silently writing plaintext `tsvector` alongside encrypted PostgreSQL columns:** Leaks personal-information tokens (names, emails, identifiers) into every SQL dump and undermines the confidentiality claim.

---

## Consequences

- `dpkms` gains a unified `KeyProvider` port usable by SQLite, PostgreSQL, local/S3 `BlobStore` (including customer-managed BYOB buckets), snapshots, ambient spool, and backups.
- Existing unencrypted stores require an explicit offline/CLI migration (`dpkms storage encrypt`) rather than silent in-place mutation.
- Key rotation (`KeyProvider.Rewrap` / `dpkms key rotate`) rotates KEKs without re-encrypting bulk blob/row payloads.
- SQLite driver selection is gated on empirical spike measurements appended below before production integration.

---

## Empirical SQLite Spike Findings

Empirically evaluated the candidate SQLite whole-DB encryption stacks against `ctxt`'s real schema requirements (external-content FTS5, `sqlite-vec` `vec0` cosine ANN virtual tables, WAL mode, wrong-key refusal, rekeying, and plaintext-to-encrypted migration):

### Candidate Matrix

| Candidate | Version / Release Age | License | `rsx analyze` | FTS5 + `vec0` | WAL/SHM Zero-Plaintext | Verdict |
|---|---|---|---|---|---|---|
| `utelle/SQLite3MultipleCiphers` + `mattn/go-sqlite3` (`-tags libsqlite3,fts5`) + `sqlite-vec-go-bindings/cgo` | `v2.6.1` (SQLite 3.54.0, released 2026-10-12, 43d old) | MIT / BSD / Apache-2.0 | PASS (6.7/10) | PASS (`sqlite=3.54.0`, `vec=v0.1.6`, `cipher=chacha20`, FTS5 `MATCH` + `vec0` cosine KNN verified) | PASS (`enc.sqlite`, `-wal`, `-shm` all scanned: 0 plaintext bytes, 0 `"SQLite format 3"` headers) | **SELECTED** |
| `ncruces/go-sqlite3` + `vfs/adiantum` + `sqlite-vec-go-bindings/ncruces` | `v0.37.0` (17d old) / `v0.25.2` | MIT | PASS (5.7/10) | **FAIL** (`v0.37.0`: compile error `undefined: sqlite3.Binary`; `v0.25.2`: runtime wazero failure `i32.atomic.store invalid as feature "" is disabled` from stale 2024 `sqlite3.wasm` in `sqlite-vec-go-bindings@v0.1.6`) | N/A | **REJECTED** |
| `sqlcipher/sqlcipher` | `v4.20.0` (28d old) | BSD-3-Clause | PASS (6.4/10) | Requires OpenSSL/CommonCrypto external linkage and custom amalgamation build | — | **REJECTED** (higher cross-platform build friction than self-contained `sqlite3mc` amalgamation) |

### Key Technical Findings (`SQLite3MultipleCiphers` v2.6.1)

1. **URI-level keying (`?cipher=chacha20&hexkey=<64-hex>`)**: Passing `cipher=chacha20` and the 32-byte raw DEK via `hexkey` in the SQLite URI ensures `sqlite3mc` configures the codec inside `sqlite3_open_v2` *before* `mattn/go-sqlite3` executes connection pragmas (`_journal_mode=WAL`, `_foreign_keys=ON`, `_busy_timeout=5000`), and applies uniformly to every pooled `database/sql` connection.
2. **Cipher selection**: `chacha20` (authenticated ChaCha20-Poly1305 per page with 16-byte Poly1305 tag, no external OpenSSL dependency) encrypts the database file, WAL (`-wal`), and leaves no plaintext in `-shm`.
3. **Fail-closed verification**: Opening an encrypted database with an incorrect or missing key fails immediately with `file is not a database`.
4. **Crash-safe plaintext→encrypted migration and DEK rekey**: `VACUUM INTO 'file:<tmp>?cipher=chacha20&hexkey=<new>'` followed by close, WAL/SHM cleanup, and atomic rename cleanly migrates plaintext WAL databases to encrypted databases and rotates DEKs while preserving FTS5 and `vec0` virtual tables.
5. **Zero driver churn**: Linking `sqlite3mc` via `-tags "libsqlite3 fts5"` preserves 100% compatibility with existing `mattn/go-sqlite3` DSNs, `sqlite3.Error` handling, and `sqlite-vec-go-bindings/cgo`.
