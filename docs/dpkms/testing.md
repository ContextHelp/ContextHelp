# Testing (dPKMS Substrate)

This document outlines the testing strategy for **dPKMS** — the substrate layer providing storage, jobs, query, graph, registries, and security.

dPKMS testing focuses on **mechanical guarantees**: durability, determinism, crash safety, and correctness.

---

## Philosophy

Testing dPKMS follows four core principles:

- **Determinism:** Given identical inputs and configuration, results must be identical
- **Durability:** Jobs and storage must survive crashes and recover correctly
- **Isolation:** Tests run without external dependencies unless explicitly enabled
- **Reproducibility:** All tests run identically in local, CI, and offline conditions

---

## Test Categories

### Unit Tests

Small, fast tests targeting individual substrate components:

**Storage Layer:**
- SQLite backend operations (CRUD, transactions, indexes)
- Postgres backend operations (multi-user scenarios)
- Storage driver interface compliance
- Schema migrations (forward, rollback)
- FTS index correctness
- Vector index operations (when enabled)

**Job System:**
- Job creation and state transitions
- Job queue primitives (enqueue, dequeue, lease)
- Retry logic with exponential backoff
- Idempotency enforcement
- Job step tracing
- Worker crash recovery

**Query Engine:**
- AST parser correctness (Boolean logic, nested expressions)
- Query plan compilation (AST → SQL)
- Filter push-down optimization
- Pagination logic
- Explain output generation

**Graph Layer:**
- Entity creation and resolution
- Mention extraction and normalization
- Edge creation (object ↔ entity, entity ↔ entity)
- Backlink index updates
- Graph traversal algorithms
- Adjacency query performance

**Registry System:**
- Registry client interface
- Sync protocol compliance
- Merge logic (local overrides)
- Entity conflict resolution
- Namespace isolation

**Security:**
- Encryption/decryption correctness
- Key derivation and rotation
- Signature verification
- Permission enforcement
- Auth token validation

Unit tests **must not** require network access or external registries.

---

### Integration Tests

Integration tests verify full substrate subsystems:

**Storage + Jobs:**
- Transactional outbox pattern correctness
- Job creation → worker pickup → completion flow
- Crash recovery (stale Running jobs)
- Job persistence across restarts
- Concurrent worker safety

**Query + Storage:**
- AST → SQL → result execution
- FTS integration with metadata filters
- Vector search integration (when enabled)
- Hybrid query execution
- Result deduplication

**Graph + Storage:**
- Entity persistence and retrieval
- Mention resolution across objects
- Backlink consistency
- Graph traversal correctness
- Orphan cleanup

**Registry + Storage:**
- Registry sync and local caching
- Entity updates from registries
- Conflict resolution with local data
- Multi-registry merging
- Offline fallback behavior

**Security + Storage:**
- Encrypted storage operations
- Key management workflows
- Signature validation on bundles
- Permission checks on operations

Integration tests use **isolated test databases** and **file-based registry fixtures**.

---

### End-to-End Tests

E2E tests validate complete substrate workflows:

**Job Execution:**
- Job enqueued → worker processes → result stored
- Worker crash → job recovery → completion
- Multiple workers processing queue concurrently
- Job step traces captured correctly

**Query Execution:**
- Complex AST queries → correct results
- Scatter-gather across local + registries
- Reranking and deduplication
- Provenance preservation in results

**Storage Durability:**
- Write → crash → restart → data intact
- Migration → rollback → data preserved
- Export → import → identical state
- Concurrent writes → consistent state

**Registry Federation:**
- Subscribe → sync → entities available
- Registry update → local merge
- Conflict → local override preserved
- Offline → cached data used

---

## Test Fixtures

Fixtures live in `dpkms/testdata/`:

**Jobs:**
- `jobs/pending.json` - Example pending jobs
- `jobs/completed.json` - Completed job traces
- `jobs/failed.json` - Failed jobs with errors

**Storage:**
- `objects/basic.json` - Simple knowledge objects
- `objects/with-mentions.json` - Objects with entities
- `entities/definitions.json` - Entity fixtures

**Registries:**
- `registries/taxonomy.json` - Taxonomy fixture
- `registries/entities.json` - Entity registry
- `registries/weights.json` - Scoring weights

**Queries:**
- `queries/ast-examples.json` - Query AST fixtures
- `queries/expected-results.json` - Golden query results

**Migrations:**
- `migrations/v1-to-v2.sql` - Example migration
- `migrations/test-data.json` - Pre/post migration data

---

## Mocking Strategy

### Mock Storage Backends

In-memory storage implementations for testing:
- Implement storage driver interface
- Support full CRUD operations
- Provide deterministic behavior
- Enable fast test execution

### Mock Registry Providers

Simulate registry HTTP endpoints:
- Static JSON fixtures
- Deterministic responses
- Configurable delays
- Error injection for testing resilience

### Mock Encryption Providers

Test encryption without real crypto:
- Noop encryption (passthrough)
- Deterministic key derivation
- Fast operations for unit tests

---

## Critical Test Scenarios

### Job Queue Durability

```go
// Test: Job survives worker crash
1. Enqueue job (state: Pending)
2. Worker picks up job (state: Running)
3. Simulate crash (kill worker)
4. Restart worker
5. Assert: Job recovered to Pending
6. Worker completes job
7. Assert: Job state is Completed
```

### Storage Crash Safety

```go
// Test: Write survives crash before commit
1. Begin transaction
2. Write object
3. Simulate crash (do not commit)
4. Restart
5. Assert: Object does NOT exist
6. Write object again with commit
7. Assert: Object exists
```

### Query Correctness

```go
// Test: Complex AST query produces correct results
1. Insert objects with known properties
2. Parse query: (tag==ui AND type==article) OR mention:@stripe.api
3. Execute query
4. Assert: Results match expected set
5. Assert: Explain output shows correct plan
```

### Graph Consistency

```go
// Test: Mention creates correct edges
1. Create object with @entity.slug mention
2. Assert: Entity exists or placeholder created
3. Assert: Edge created (object → entity)
4. Assert: Backlink queryable from entity
5. Delete object
6. Assert: Edge removed
7. Assert: Orphan entity cleaned up (if placeholder)
```

---

## Performance Testing

dPKMS must maintain predictable performance:

**Storage Benchmarks:**
- Write throughput (objects/sec)
- Query latency (p50, p95, p99)
- FTS search speed
- Vector search speed (when enabled)
- Index build time

**Job Queue Benchmarks:**
- Job enqueue rate
- Job processing throughput
- Worker scaling efficiency
- Queue size vs latency

**Graph Benchmarks:**
- Mention resolution speed
- Traversal performance
- Backlink query speed
- Large graph operations

**Registry Benchmarks:**
- Sync performance
- Merge operation speed
- Multi-registry scatter latency

Performance tests use **large datasets** to validate scaling.

---

## Security Testing

### Input Validation

Fuzz testing for:
- Query parser (malformed AST)
- Registry responses (malicious JSON)
- Entity definitions (XSS, injection)
- Storage inputs (SQL injection prevention)

### Permission Enforcement

Tests for:
- Permission checks before operations
- Capability verification
- Auth token validation
- Plugin sandbox enforcement

### Cryptographic Correctness

Tests for:
- Encryption roundtrip (encrypt → decrypt)
- Signature verification (sign → verify)
- Key rotation (old key → new key)
- Tamper detection

---

## Cross-Platform Testing

dPKMS must work on:
- macOS
- Linux
- Windows

Platform-specific tests:
- File locking behavior
- Path handling
- SQLite WAL mode
- Concurrent access

---

## Offline Mode Testing

Since dPKMS is local-first:

**Tests validate:**
- All operations work offline
- Registry fallback to cached data
- Graceful degradation
- Clear error messages when network required

---

## Continuous Integration

CI for dPKMS:
- **Unit tests:** Every commit
- **Integration tests:** Every PR
- **E2E tests:** Main branch merges
- **Performance benchmarks:** Weekly
- **Security scans:** Daily

**Checks:**
- Go race detector (`-race`)
- Static analysis (golangci-lint)
- Coverage reports (minimum 80%)
- Golden file validation
- Cross-platform builds

---

## Test Coverage Requirements

**Minimum coverage by component:**
- Storage layer: 90%
- Job system: 95% (critical path)
- Query engine: 85%
- Graph layer: 85%
- Registry system: 80%
- Security: 95% (critical path)

---

## Summary

dPKMS testing ensures:
- **Durable execution** - Jobs survive crashes
- **Correct queries** - Results match expectations
- **Safe storage** - Data never corrupted
- **Secure operations** - Permissions enforced
- **Predictable performance** - Scales reliably

See also:
- [../ctxt/testing.md](../ctxt/testing.md) - Brain layer testing
- [storage.md](storage.md) - Storage architecture
- [jobs-and-ingestion.md](jobs-and-ingestion.md) - Job system design
- [security.md](security.md) - Security model
