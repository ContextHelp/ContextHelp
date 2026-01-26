# ADR-021 – Multi-Backend Storage Strategy (SQLite Default, Pluggable Architecture)

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** dPKMS
> **Supersedes:** ADR-006 (JSON Default Storage)
> **Superseded by:** N/A

---

## Context

The storage layer is the foundation of dPKMS, directly impacting performance, reliability, scalability, and user experience. Previous decision (ADR-006) chose JSON filesystem as default, but experience revealed limitations:

**JSON Filesystem Limitations:**
- Full-scan queries unacceptable at scale (1000+ objects)
- No transactional guarantees across files
- Concurrent access requires complex locking
- FTS and vector search impossible without external indexing
- Graph traversal (mentions, entities, edges) requires in-memory loading
- No query optimization or planning

**Storage Requirements Evolution:**
- Entity graph with backlinks needs efficient traversal
- Mention-based filtering requires indexed lookups
- Hybrid retrieval (metadata + FTS + vector + graph) needs coordination
- Profile-scoped queries need performant filtering
- Plugin metadata needs flexible schema
- Multi-user scenarios emerging (teams, shared workspaces)

**User Personas Drive Needs:**

**Solo Developer (Most Common):**
- Local-first, zero-install preference
- Occasional queries (10-50/day)
- Knowledge base: 100-5000 objects
- Performance: "fast enough" (< 100ms queries)
- Deployment: Single device, offline-capable

**Power User / Knowledge Worker:**
- Heavy daily usage (100+ queries/day)
- Knowledge base: 5000-50000 objects
- Performance: Critical (< 50ms queries)
- Deployment: Multiple devices, sync needed
- Willing to configure for performance

**Team / Organization:**
- Shared knowledge base
- Concurrent users: 5-50
- Knowledge base: 10000-100000+ objects
- Performance: Must scale (< 100ms under load)
- Deployment: Server-based, high availability
- Backup and disaster recovery required

**Constraints:**
- Must remain local-first by default
- Must work offline without degradation
- Must support zero-install for solo users
- Must scale to power user and team scenarios
- Must support pluggable backends (no lock-in)
- Must preserve data portability (export/import)
- Must maintain single codebase across backends

**Affected Subsystems:**
- Query engine (backend-specific optimizations)
- Graph index (entity/mention lookups)
- Job system (transactional job queue)
- FTS indexing (full-text search)
- Vector indexing (semantic search)
- Encryption (backend-native vs application-level)
- Migration tools (between backends)

**Goals:**
- Default to SQLite (zero-install, local-first, performant)
- Support PostgreSQL (teams, scale, multi-user)
- Enable pluggable backends (LEANN, custom, future)
- Provide clear migration paths between backends
- Maintain backend-agnostic application layer
- Optimize per-backend without breaking abstractions

---

## Decision

**dPKMS will use SQLite with WAL mode as the default storage backend, providing zero-install local-first operation with excellent performance for solo and power users, while supporting PostgreSQL as an optional backend for team/organization deployments requiring multi-user concurrency, and maintaining a pluggable storage architecture enabling custom backends (LEANN, KV stores, graph databases) without application layer changes.**

The storage strategy provides:

1. **Default Backend: SQLite**

   **Configuration:**
   ```yaml
   storage:
     type: sqlite
     path: ~/.local/share/ctxt/knowledge.db
     options:
       journal_mode: WAL         # Write-Ahead Logging
       synchronous: NORMAL       # Balance safety/performance
       cache_size: -64000        # 64MB cache
       mmap_size: 268435456      # 256MB memory-mapped I/O
       temp_store: MEMORY        # Temp tables in memory
       busy_timeout: 5000        # 5s wait on lock
       foreign_keys: ON
   ```

   **Why SQLite Default:**

   **Zero Install:**
   - Embedded (no server process)
   - Single file database
   - Cross-platform (macOS, Linux, Windows)
   - No configuration required
   - Ships with Go standard library

   **Local-First Perfect Fit:**
   - Fully offline capable
   - No network dependency
   - User owns the file completely
   - Easy backup (copy file)
   - Simple migration (export/import)

   **Performance Characteristics:**
   - Read-heavy workloads: Excellent (< 1ms per query)
   - Write throughput: Good (1000+ inserts/sec with WAL)
   - Concurrent reads: Excellent (unlimited readers)
   - Concurrent writes: Limited (single writer)
   - FTS5: Built-in, performant
   - JSON operators: Native support
   - Graph traversal: Good with proper indexes

   **Scalability:**
   - Objects: 1M+ (tested, performant)
   - Database size: 100GB+ (tested)
   - Queries/sec: 1000+ (single device)
   - Concurrent users: 1 (primary writer)
   - Read-only users: Unlimited

   **When SQLite is Optimal:**
   - Solo users (most common)
   - Power users (single device or synced)
   - Offline-first requirements
   - < 50K objects
   - Single primary writer
   - Local development and testing

2. **Optional Backend: PostgreSQL**

   **Configuration:**
   ```yaml
   storage:
     type: postgres
     connection:
       host: localhost
       port: 5432
       database: ctxt
       user: ctxt_user
       password: ${CTXT_DB_PASSWORD}
       sslmode: require
       pool:
         max_connections: 25
         min_connections: 5
         max_idle_time: 5m
         max_lifetime: 1h
     options:
       search_path: ctxt,public
       statement_timeout: 30s
       idle_in_transaction_session_timeout: 60s
   ```

   **Why PostgreSQL Optional:**

   **Multi-User Support:**
   - True concurrent writes (MVCC)
   - Row-level locking
   - Multiple writers without blocking
   - Strong ACID guarantees
   - Transaction isolation levels

   **Enterprise Features:**
   - Streaming replication (HA)
   - Point-in-time recovery
   - Logical replication (sync)
   - Advanced indexing (GiST, GIN, BRIN)
   - Partitioning for large datasets

   **Performance at Scale:**
   - Optimized for concurrent access
   - Better query planner for complex queries
   - Advanced statistics for optimization
   - Parallel query execution
   - Better memory management at scale

   **Operational Maturity:**
   - Battle-tested in production
   - Extensive monitoring tools
   - Rich extension ecosystem
   - Professional support available
   - Well-documented administration

   **When PostgreSQL is Optimal:**
   - Team/organization deployments (5+ users)
   - Concurrent write access required
   - High availability needed
   - > 50K objects
   - Server-based architecture
   - Advanced querying requirements
   - Compliance/audit requirements

3. **Pluggable Storage Architecture**

   **Storage Interface (dPKMS):**
   ```go
   type Store interface {
       // Lifecycle
       Open(ctx context.Context) error
       Close() error
       Migrate(ctx context.Context) error

       // Objects
       CreateObject(ctx context.Context, obj *Object) error
       GetObject(ctx context.Context, id string) (*Object, error)
       UpdateObject(ctx context.Context, obj *Object) error
       DeleteObject(ctx context.Context, id string) error
       QueryObjects(ctx context.Context, query *Query) ([]*Object, error)

       // Entities
       CreateEntity(ctx context.Context, ent *Entity) error
       GetEntity(ctx context.Context, id string) (*Entity, error)
       QueryEntities(ctx context.Context, query *Query) ([]*Entity, error)

       // Edges (Graph)
       CreateEdge(ctx context.Context, edge *Edge) error
       QueryEdges(ctx context.Context, query *EdgeQuery) ([]*Edge, error)
       GetBacklinks(ctx context.Context, entityID string) ([]*Object, error)

       // Jobs
       CreateJob(ctx context.Context, job *Job) error
       GetJob(ctx context.Context, id string) (*Job, error)
       UpdateJobStatus(ctx context.Context, id string, status JobStatus) error
       QueryPendingJobs(ctx context.Context, limit int) ([]*Job, error)

       // Transactions
       BeginTx(ctx context.Context) (Tx, error)

       // Capabilities
       SupportsFullText() bool
       SupportsVectors() bool
       SupportsJSONQueries() bool
       SupportsTransactions() bool
       SupportsConcurrentWrites() bool
   }

   type Tx interface {
       Commit() error
       Rollback() error
       Store() Store  // Store operations within transaction
   }
   ```

   **Backend Registration:**
   ```go
   type StorageProvider interface {
       Name() string
       Open(config Config) (Store, error)
       ValidateConfig(config Config) error
       MigrationPath(from string) ([]Migration, error)
   }

   // Built-in providers
   func init() {
       RegisterStorageProvider("sqlite", &SQLiteProvider{})
       RegisterStorageProvider("postgres", &PostgresProvider{})
   }

   // Plugin providers
   func RegisterStorageProvider(name string, provider StorageProvider)
   ```

4. **Backend Comparison Matrix**

   | Feature | SQLite | PostgreSQL | Custom/Plugin |
   |---------|--------|------------|---------------|
   | **Install Complexity** | Zero | Server setup | Varies |
   | **Local-First** | ✓ Perfect | Limited | Varies |
   | **Offline Operation** | ✓ Always | ✗ Server required | Varies |
   | **Concurrent Reads** | ✓ Unlimited | ✓ Unlimited | Varies |
   | **Concurrent Writes** | ✗ Single writer | ✓ Multiple writers | Varies |
   | **Max Objects** | 1M+ | 100M+ | Varies |
   | **Query Performance** | Excellent | Excellent | Varies |
   | **FTS Built-in** | ✓ FTS5 | ✓ tsvector | Varies |
   | **JSON Support** | ✓ Native | ✓ JSONB | Varies |
   | **Vector Support** | Plugin | pgvector | Native (LEANN) |
   | **Graph Traversal** | Good | Excellent | Native (Neo4j) |
   | **Backup** | File copy | pg_dump | Varies |
   | **Replication** | Manual | Built-in | Varies |
   | **Encryption** | SQLCipher | pgcrypto | Varies |
   | **Cost** | Free | Free (self-host) | Varies |
   | **Typical Use Case** | Solo, Power User | Team, Org | Special needs |

5. **Migration Between Backends**

   **SQLite → PostgreSQL:**
   ```bash
   # Export from SQLite
   ctxt export --output backup.ctxt --format portable

   # Reconfigure for PostgreSQL
   ctxt config set storage.type postgres
   ctxt config set storage.connection.host localhost
   ctxt config set storage.connection.database ctxt

   # Initialize PostgreSQL schema
   ctxt migrate init

   # Import data
   ctxt import backup.ctxt --strategy merge

   # Rebuild indexes
   ctxt index rebuild
   ```

   **PostgreSQL → SQLite:**
   ```bash
   # Export from PostgreSQL
   ctxt export --output backup.ctxt

   # Reconfigure for SQLite
   ctxt config set storage.type sqlite
   ctxt config set storage.path ~/.local/share/ctxt/knowledge.db

   # Initialize SQLite schema
   ctxt migrate init

   # Import data
   ctxt import backup.ctxt
   ```

   **Migration Tools:**
   ```go
   type MigrationEngine struct {
       from Store
       to   Store
   }

   func (m *MigrationEngine) Migrate(
       ctx context.Context,
       options MigrationOptions,
   ) (*MigrationResult, error)

   type MigrationOptions struct {
       BatchSize       int
       SkipIndexes     bool  // Build indexes after
       Verify          bool  // Verify data integrity
       DryRun          bool  // Test without committing
   }
   ```

6. **Schema Management**

   **Version-Controlled Migrations:**
   ```
   dPKMS/storage/migrations/
     001_initial_schema.sql
     002_add_entities.sql
     003_add_mentions.sql
     004_add_graph_edges.sql
     005_add_job_system.sql
     006_add_fts_indexes.sql
     ...
   ```

   **Backend-Specific Migrations:**
   ```
   dPKMS/storage/migrations/
     sqlite/
       001_initial_schema.sql
       002_add_fts5_virtual_tables.sql
     postgres/
       001_initial_schema.sql
       002_add_gin_indexes.sql
   ```

   **Migration CLI:**
   ```bash
   # Check migration status
   ctxt migrate status

   # Run pending migrations
   ctxt migrate up

   # Rollback last migration
   ctxt migrate down

   # Create new migration
   ctxt migrate create add_new_feature
   ```

7. **Performance Optimization Per Backend**

   **SQLite Optimizations:**
   ```sql
   -- Indexes for common queries
   CREATE INDEX idx_objects_type ON objects(type);
   CREATE INDEX idx_objects_created_at ON objects(created_at);
   CREATE INDEX idx_objects_profile ON objects(profile);

   -- JSON indexes (SQLite 3.38+)
   CREATE INDEX idx_tags ON objects(json_extract(tags, '$[*].label'));

   -- FTS5 virtual table
   CREATE VIRTUAL TABLE objects_fts USING fts5(
       id, summary, content, tokenize='porter unicode61'
   );

   -- Graph indexes
   CREATE INDEX idx_edges_from ON edges(from_type, from_id);
   CREATE INDEX idx_edges_to ON edges(to_type, to_id);
   ```

   **PostgreSQL Optimizations:**
   ```sql
   -- Partial indexes
   CREATE INDEX idx_objects_active ON objects(created_at)
   WHERE deleted_at IS NULL;

   -- GIN index for JSONB
   CREATE INDEX idx_tags_gin ON objects USING gin(tags jsonb_path_ops);

   -- Full-text search
   CREATE INDEX idx_objects_fts ON objects
   USING gin(to_tsvector('english', content));

   -- Graph indexes (for recursive queries)
   CREATE INDEX idx_edges_from_composite ON edges(from_type, from_id, to_type);

   -- Partitioning for large datasets
   CREATE TABLE objects (
       id UUID,
       created_at TIMESTAMP,
       ...
   ) PARTITION BY RANGE (created_at);
   ```

8. **Custom Backend Support**

   **LEANN Integration (Vector-Optimized):**
   ```yaml
   storage:
     type: hybrid
     backends:
       metadata:
         type: sqlite
         path: ~/.local/share/ctxt/metadata.db
       vectors:
         type: leann
         path: ~/.local/share/ctxt/leann
         options:
           recompute: true
           graph_degree: 32
   ```

   **Graph Database (Neo4j, Dgraph):**
   ```yaml
   storage:
     type: plugin
     plugin: neo4j-storage
     connection:
       uri: bolt://localhost:7687
       user: neo4j
       password: ${NEO4J_PASSWORD}
   ```

   **Key-Value Store (Foundation for others):**
   ```yaml
   storage:
     type: plugin
     plugin: badger-storage
     path: ~/.local/share/ctxt/badger
     options:
       compression: zstd
       in_memory: false
   ```

---

## Rationale

### Alternatives Considered

#### 1. **Continue with JSON Filesystem (Rejected)**
Keep ADR-006 decision, optimize with in-memory indexes.

**Rejected because:**
- Cannot efficiently support graph traversal
- FTS requires external indexing
- Vector search impossible
- Concurrent writes problematic
- Performance unacceptable at scale
- Maintenance burden for edge cases

#### 2. **PostgreSQL as Default (Rejected)**
Make PostgreSQL the primary backend, SQLite optional.

**Rejected because:**
- Violates zero-install principle
- Requires server setup (high friction)
- Overkill for solo users (majority)
- Offline capability compromised
- Local-first philosophy undermined
- Increased operational complexity

#### 3. **Single Backend Only (Rejected)**
Choose one backend, remove pluggability.

**Rejected because:**
- No migration path for growth
- Teams excluded from architecture
- Cannot leverage specialized stores (LEANN, Neo4j)
- Vendor lock-in (violates sovereignty)
- Future-proofing sacrificed

#### 4. **Pure Key-Value Store (Rejected)**
Use embedded KV store (BadgerDB, LMDB) as default.

**Rejected because:**
- No SQL query capabilities
- FTS requires external implementation
- Graph queries complex
- Less mature tooling
- Migration from JSON harder
- Increased implementation burden

#### 5. **Hybrid Default (SQLite + LEANN) (Rejected)**
Default to split storage from start.

**Rejected because:**
- Increased complexity for solo users
- Two systems to maintain
- Sync issues between stores
- LEANN less mature
- Overkill for common case

### Benefits of Chosen Approach

**Progressive Complexity:**
- Solo users: SQLite (zero friction)
- Power users: SQLite optimized (same setup)
- Teams: PostgreSQL (migrate when needed)
- Special needs: Custom backends (plugin)

**Local-First Preserved:**
- Default (SQLite) fully local
- Offline capability guaranteed
- User owns data file
- Simple backup and restore
- No vendor dependency

**Performance Optimized:**
- SQLite excellent for < 50K objects
- PostgreSQL scales to millions
- Backend-specific optimizations
- No performance regression from pluggability

**Future-Proof:**
- Pluggable architecture
- LEANN for vector search (when mature)
- Graph DBs for complex traversal
- New backends without rewrite

**Migration Path Clear:**
- Start simple (SQLite)
- Grow to PostgreSQL (when needed)
- Export/import preserves data
- No lock-in at any stage

### Drawbacks / Risks

**Abstraction Overhead:**
- Interface adds indirection
- Backend-specific optimizations harder
- Lowest-common-denominator features
- Testing complexity (multiple backends)

**Migration Complexity:**
- Data migration between backends non-trivial
- Schema differences require translation
- Downtime during migration
- Verification required

**Maintenance Burden:**
- Multiple backends to test
- Different query optimization strategies
- Backend-specific bugs
- Documentation for each backend

**Feature Parity:**
- Not all backends support all features
- Capability detection required
- Graceful degradation needed
- User education about differences

---

## Consequences

### Positive

**Optimal Defaults:**
- Zero-install SQLite perfect for majority
- Local-first philosophy preserved
- Excellent performance for common case
- Simple setup and maintenance

**Growth Path:**
- Teams can migrate to PostgreSQL
- No rewrite required
- Export/import handles transition
- Clear documentation for migration

**Flexibility:**
- Custom backends possible
- LEANN for vector optimization
- Graph DBs for specialized needs
- No architectural lock-in

**Performance:**
- Backend-specific optimizations
- SQLite excellent for solo/power users
- PostgreSQL scales for teams
- Specialized backends for unique needs

### Negative

**Implementation Complexity:**
- Storage abstraction layer required
- Multiple backends to maintain
- Backend-specific code paths
- Testing matrix grows (SQLite × Postgres × ...)

**Migration Risk:**
- Data migration can fail
- Downtime during transition
- User education required
- Verification tooling needed

**Feature Fragmentation:**
- Not all features on all backends
- Capability detection everywhere
- Lowest-common-denominator limits
- Documentation complexity

**Support Burden:**
- Multiple backends to support
- Backend-specific issues
- Performance tuning per backend
- Different operational procedures

### Neutral / Considerations

**Default Backend Reassessment:**
- Monitor SQLite limits in practice
- LEANN maturity could shift recommendation
- DuckDB emergence as alternative
- Cloud-native options (Turso, Neon)

**Hybrid Storage:**
- Metadata in SQLite, vectors in LEANN
- Benefits of both worlds
- Increased complexity
- Clear use case for power users

**Backup Strategy:**
- SQLite: file copy
- PostgreSQL: pg_dump
- Hybrid: coordinate both
- Export bundle always works

**Cloud-Native Evolution:**
- LibSQL (SQLite fork for edge)
- Turso (distributed SQLite)
- Neon (serverless Postgres)
- Future ADR if adopted

---

## Implementation Notes

### Core Components

**Storage Provider Registry:**
```go
var providers = make(map[string]StorageProvider)

func RegisterStorageProvider(name string, provider StorageProvider) {
    providers[name] = provider
}

func OpenStorage(config Config) (Store, error) {
    provider, ok := providers[config.Type]
    if !ok {
        return nil, fmt.Errorf("unknown storage type: %s", config.Type)
    }
    return provider.Open(config)
}
```

**SQLite Implementation:**
```go
type SQLiteStore struct {
    db *sql.DB
}

func (s *SQLiteStore) Open(ctx context.Context) error {
    db, err := sql.Open("sqlite3", s.path)
    // Configure WAL, pragmas, etc.
    return err
}

func (s *SQLiteStore) QueryObjects(ctx context.Context, query *Query) ([]*Object, error) {
    // Build SQL from AST
    // Execute query
    // Return results
}
```

**PostgreSQL Implementation:**
```go
type PostgresStore struct {
    pool *pgxpool.Pool
}

func (p *PostgresStore) Open(ctx context.Context) error {
    pool, err := pgxpool.Connect(ctx, p.connString)
    // Configure pool, timeouts, etc.
    return err
}

func (p *PostgresStore) QueryObjects(ctx context.Context, query *Query) ([]*Object, error) {
    // Build SQL from AST (Postgres-specific optimizations)
    // Execute query
    // Return results
}
```

**Migration Engine:**
```go
type MigrationEngine struct {
    store Store
}

func (m *MigrationEngine) CurrentVersion() (int, error)
func (m *MigrationEngine) PendingMigrations() ([]Migration, error)
func (m *MigrationEngine) Up(ctx context.Context) error
func (m *MigrationEngine) Down(ctx context.Context) error
```

### CLI Commands

```bash
# Storage configuration
ctxt config get storage
ctxt config set storage.type sqlite
ctxt config set storage.type postgres

# Migration management
ctxt migrate status
ctxt migrate up
ctxt migrate down
ctxt migrate create <name>

# Backend migration
ctxt storage migrate --from sqlite --to postgres
ctxt storage verify
ctxt storage optimize
```

### Performance Benchmarks

**Target Metrics:**

| Operation | SQLite | PostgreSQL |
|-----------|--------|------------|
| Single object read | < 1ms | < 2ms |
| Filtered query (100 results) | < 10ms | < 15ms |
| FTS query (1000 results) | < 50ms | < 50ms |
| Graph traversal (depth 3) | < 30ms | < 25ms |
| Batch insert (100 objects) | < 100ms | < 50ms |
| Concurrent reads (10 threads) | < 10ms/query | < 10ms/query |
| Concurrent writes (10 threads) | Serialized | < 20ms/query |

### Testing Strategy

**Multi-Backend Test Suite:**
```go
func TestStore(t *testing.T, provider StorageProvider) {
    store := provider.Open(testConfig)

    t.Run("CRUD", func(t *testing.T) { ... })
    t.Run("Queries", func(t *testing.T) { ... })
    t.Run("Transactions", func(t *testing.T) { ... })
    t.Run("Concurrency", func(t *testing.T) { ... })
}

func TestSQLiteStore(t *testing.T) {
    TestStore(t, &SQLiteProvider{})
}

func TestPostgresStore(t *testing.T) {
    TestStore(t, &PostgresProvider{})
}
```

### Migration Strategy

**Phase 1: Storage Interface (Skeleton 1)**
- Define Store interface
- Implement SQLite backend
- Migrate from JSON (if ADR-006 implemented)
- Basic migration tooling

**Phase 2: PostgreSQL Support (Skeleton 7)**
- Implement PostgreSQL backend
- Backend-specific optimizations
- Migration tools between backends
- Performance benchmarking

**Phase 3: Plugin Architecture (Skeleton 9)**
- Pluggable storage providers
- LEANN integration
- Custom backend examples
- Advanced migration features

---

## References

- **dpkms/storage.md** – Storage layer specification
- **design.md:288-303** – Storage backend discussion
- **ROADMAP.md** – Skeleton 1: Durable Core (SQLite backend)
- ADR-006 – JSON Default Storage (superseded by this ADR)
- ADR-001 – Local-First and Decentralized (SQLite aligns perfectly)
- ADR-020 – Export/Import (enables backend migration)

**External References:**
- SQLite documentation: https://sqlite.org/docs.html
- SQLite Performance: https://www.sqlite.org/speed.html
- PostgreSQL documentation: https://www.postgresql.org/docs/
- LEANN: https://github.com/yichuan-w/LEANN
- pgvector: https://github.com/pgvector/pgvector

---
