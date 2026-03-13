# Phase 6: Business Logic — Design Document

> **Date:** 2026-02-18
> **Status:** Draft
> **Applies to:** dPKMS, ctxt

---

## Goal

Replace the stub implementations in `dpkms serve` and all `ctxt` commands with working business logic. When Phase 6 is done, a user can:

1. Run `dpkms serve` and get a real HTTP + gRPC server
2. Run `ctxt analyze "some text"` and have it ingested into SQLite through the pipeline
3. Run `ctxt find "type==article"` and get results back via RSQL
4. Run `ctxt list`, `ctxt open`, `ctxt delete`, `ctxt edit` against real data

---

## Subsystems and Build Order

Each subsystem depends on the one above it. Build in this order:

```
1. Storage Layer      (no dependencies — foundation)
2. Job System         (depends on: Storage)
3. Pipeline Runtime   (depends on: Storage, Jobs)
4. Search Engine      (depends on: Storage)
5. HTTP Server        (depends on: all above)
6. gRPC Server        (depends on: all above)
```

---

## 1. Storage Layer

### What It Does

Persists knowledge objects, entities, edges, and jobs to SQLite (default) or Postgres.

### Package Structure

```
internal/
  storage/
    storage.go          # Store interface + factory
    sqlite/
      driver.go         # SQLite driver (implements StorageDriver)
      objects.go        # ObjectStore implementation
      entities.go       # EntityStore implementation
      edges.go          # EdgeStore implementation
      jobs.go           # JobStore implementation
      fts.go            # FTS5 virtual table management
      migrations.go     # Schema versioning
      migrations/
        001_initial.sql  # Tables + indexes
    postgres/
      driver.go         # Postgres driver (stub for Phase 6, implement later)
```

### Core Interfaces

```go
// storage.go

type StorageDriver interface {
    Init(ctx context.Context) error
    Close(ctx context.Context) error
    Objects() ObjectStore
    Entities() EntityStore
    Edges() EdgeStore
    Jobs() JobStore
    Health(ctx context.Context) error
}

type ObjectStore interface {
    Create(ctx context.Context, obj *KnowledgeObject) error
    Get(ctx context.Context, id string) (*KnowledgeObject, error)
    List(ctx context.Context, filter ObjectFilter) ([]*KnowledgeObject, int, error)
    Update(ctx context.Context, obj *KnowledgeObject) error
    Delete(ctx context.Context, id string) error
}

type EntityStore interface {
    Upsert(ctx context.Context, entity *Entity) error
    Get(ctx context.Context, slug string) (*Entity, error)
    List(ctx context.Context, filter EntityFilter) ([]*Entity, error)
    Resolve(ctx context.Context, mention string) (*Entity, error)
}

type EdgeStore interface {
    Create(ctx context.Context, edge *Edge) error
    ListFrom(ctx context.Context, fromType, fromID string) ([]*Edge, error)
    ListTo(ctx context.Context, toType, toID string) ([]*Edge, error)
    Delete(ctx context.Context, id string) error
    DeleteByObject(ctx context.Context, objectID string) error
}
```

### Schema (001_initial.sql)

Per ADR-049 (edges table for mentions), ADR-053 (KnowledgeObject struct):

```sql
-- Knowledge objects
CREATE TABLE IF NOT EXISTS objects (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    subtype TEXT DEFAULT '',
    raw_content TEXT DEFAULT '',
    content_type TEXT DEFAULT '',
    metadata JSON DEFAULT '{}',
    summaries JSON DEFAULT '[]',
    sections JSON DEFAULT '[]',
    tags JSON DEFAULT '[]',
    mentions JSON DEFAULT '[]',
    decisions JSON DEFAULT '[]',
    tasks JSON DEFAULT '[]',
    embeddings BLOB,
    pipeline TEXT DEFAULT '',
    source TEXT DEFAULT '',
    registry_influences JSON DEFAULT '[]',
    plugins JSON DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    fts_indexed INTEGER DEFAULT 0,
    vector_indexed INTEGER DEFAULT 0
);

-- Full-text search
CREATE VIRTUAL TABLE IF NOT EXISTS objects_fts USING fts5(
    id UNINDEXED,
    summaries,
    raw_content,
    content='objects',
    content_rowid='rowid'
);

-- Entities
CREATE TABLE IF NOT EXISTS entities (
    slug TEXT PRIMARY KEY,
    title TEXT DEFAULT '',
    description TEXT DEFAULT '',
    namespace TEXT DEFAULT '',
    aliases JSON DEFAULT '[]',
    metadata JSON DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Edges (source of truth for all relationships — ADR-049)
CREATE TABLE IF NOT EXISTS edges (
    id TEXT PRIMARY KEY,
    from_type TEXT NOT NULL,
    from_id TEXT NOT NULL,
    to_type TEXT NOT NULL,
    to_id TEXT NOT NULL,
    edge_type TEXT NOT NULL,
    weight REAL DEFAULT 1.0,
    metadata JSON DEFAULT '{}',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_type, from_id);
CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_type, to_id);
CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(edge_type);
CREATE INDEX IF NOT EXISTS idx_edges_from_type ON edges(from_type, from_id, edge_type);

-- Jobs
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    payload TEXT DEFAULT '',
    pipeline TEXT DEFAULT '',
    source TEXT DEFAULT '',
    result_id TEXT DEFAULT '',
    error TEXT DEFAULT '',
    retry_count INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_type ON jobs(type);

-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

-- SQLite pragmas (applied at connection time, not in migration)
-- PRAGMA journal_mode=WAL;
-- PRAGMA synchronous=NORMAL;
-- PRAGMA foreign_keys=ON;
-- PRAGMA cache_size=-64000;
```

### Domain Types

```go
// types.go (in internal/storage/)

type KnowledgeObject struct {
    ID                 string         `json:"id"`
    Type               string         `json:"type"`
    Subtype            string         `json:"subtype,omitempty"`
    RawContent         string         `json:"raw_content"`
    ContentType        string         `json:"content_type,omitempty"`
    Metadata           map[string]any `json:"metadata,omitempty"`
    Summaries          []string       `json:"summaries,omitempty"`
    Sections           []Section      `json:"sections,omitempty"`
    Tags               []Tag          `json:"tags,omitempty"`
    Mentions           []string       `json:"mentions,omitempty"`
    Decisions          []Decision     `json:"decisions,omitempty"`
    Tasks              []Task         `json:"tasks,omitempty"`
    Embeddings         []float32      `json:"embeddings,omitempty"`
    Pipeline           string         `json:"pipeline,omitempty"`
    Source             string         `json:"source,omitempty"`
    RegistryInfluences []string       `json:"registry_influences,omitempty"`
    Plugins            map[string]any `json:"plugins,omitempty"`
    CreatedAt          time.Time      `json:"created_at"`
    UpdatedAt          time.Time      `json:"updated_at"`
    FTSIndexed         bool           `json:"fts_indexed"`
    VectorIndexed      bool           `json:"vector_indexed"`
}

// Draft is a KnowledgeObject being progressively enriched (ADR-053).
type Draft = KnowledgeObject

type Section struct {
    Title   string `json:"title"`
    Content string `json:"content"`
    Order   int    `json:"order"`
}

type Tag struct {
    Label  string  `json:"label"`
    Weight float64 `json:"weight,omitempty"`
    Source string  `json:"source,omitempty"`
}

type Decision struct {
    Title  string `json:"title"`
    Status string `json:"status"`
    Impact string `json:"impact"`
}

type Task struct {
    Title  string `json:"title"`
    Status string `json:"status"`
}

type Entity struct {
    Slug        string         `json:"slug"`
    Title       string         `json:"title"`
    Description string         `json:"description,omitempty"`
    Namespace   string         `json:"namespace,omitempty"`
    Aliases     []string       `json:"aliases,omitempty"`
    Metadata    map[string]any `json:"metadata,omitempty"`
    CreatedAt   time.Time      `json:"created_at"`
    UpdatedAt   time.Time      `json:"updated_at"`
}

type Edge struct {
    ID        string         `json:"id"`
    FromType  string         `json:"from_type"`
    FromID    string         `json:"from_id"`
    ToType    string         `json:"to_type"`
    ToID      string         `json:"to_id"`
    EdgeType  string         `json:"edge_type"`
    Weight    float64        `json:"weight"`
    Metadata  map[string]any `json:"metadata,omitempty"`
    CreatedAt time.Time      `json:"created_at"`
}

type ObjectFilter struct {
    Type     string
    Subtype  string
    Tag      string
    Mention  string
    Pipeline string
    After    *time.Time
    Before   *time.Time
    Limit    int
    Offset   int
    Sort     string // "created_at", "updated_at"
    Dir      string // "asc", "desc"
}

type EntityFilter struct {
    Namespace string
    Limit     int
    Offset    int
}
```

### Factory

```go
func NewDriver(cfg config.StorageConfig) (StorageDriver, error) {
    switch cfg.Type {
    case "sqlite":
        return sqlite.New(cfg.Path)
    case "postgres":
        return nil, fmt.Errorf("postgres backend not yet implemented")
    default:
        return nil, fmt.Errorf("unknown storage type: %s", cfg.Type)
    }
}
```

### Migration System

Simple, forward-only migrations:

```go
type Migration struct {
    Version int
    SQL     string
}

func (d *Driver) Migrate(ctx context.Context) error {
    // 1. Read current version from schema_version
    // 2. Apply any migration with version > current
    // 3. Update schema_version
}
```

Postgres driver is stubbed — returns an error explaining it's not yet implemented. This keeps the interface honest without blocking Phase 6.

---

## 2. Job System

### What It Does

Queues ingestion work, tracks progress, retries failures, recovers from crashes.

### Package Structure

```
internal/
  jobs/
    queue.go        # JobQueue interface + in-process implementation
    worker.go       # WorkerPool — pulls jobs, runs pipelines
    types.go        # Job, JobStatus, JobStep types
```

### Job Lifecycle

```
Pending → Running → Completed
                  → Failed (retry_count < max_retries → back to Pending)
                  → Failed (retry_count >= max_retries → stays Failed)
```

Stale detection: a job stuck in `Running` for longer than `stale_timeout` (default 30m) gets reset to `Pending`.

### Core Types

```go
type JobStatus string

const (
    JobPending   JobStatus = "pending"
    JobRunning   JobStatus = "running"
    JobCompleted JobStatus = "completed"
    JobFailed    JobStatus = "failed"
)

type Job struct {
    ID          string    `json:"id"`
    Type        string    `json:"type"`       // "ingest:text", "ingest:url"
    Status      JobStatus `json:"status"`
    Payload     string    `json:"payload"`    // raw content or URL
    Pipeline    string    `json:"pipeline"`   // pipeline name
    Source      string    `json:"source"`
    ResultID    string    `json:"result_id"`  // KnowledgeObject ID on success
    Error       string    `json:"error"`
    RetryCount  int       `json:"retry_count"`
    MaxRetries  int       `json:"max_retries"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
    StartedAt   *time.Time `json:"started_at,omitempty"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type JobFilter struct {
    Status   JobStatus
    Type     string
    Limit    int
    Offset   int
}
```

### Queue Interface

The queue is backed by the `jobs` table in storage. No separate message broker needed for Phase 6 — SQLite with WAL handles concurrent readers and a single writer.

```go
type JobQueue interface {
    Enqueue(ctx context.Context, job *Job) error
    AcquireNext(ctx context.Context) (*Job, error)  // atomic: sets status=running
    Complete(ctx context.Context, id string, resultID string) error
    Fail(ctx context.Context, id string, errMsg string) error
    Retry(ctx context.Context, id string) error
    Get(ctx context.Context, id string) (*Job, error)
    List(ctx context.Context, filter JobFilter) ([]*Job, int, error)
    RecoverStale(ctx context.Context, timeout time.Duration) (int, error)
}
```

`AcquireNext` uses `UPDATE ... WHERE status='pending' ORDER BY created_at LIMIT 1 RETURNING *` to atomically claim a job.

### Worker Pool

```go
type WorkerPool struct {
    queue     JobQueue
    pipelines pipeline.Registry
    store     storage.StorageDriver
    workers   int
    staleTimeout time.Duration
}

func (p *WorkerPool) Start(ctx context.Context) error {
    g, ctx := errgroup.WithContext(ctx)

    // Stale job recovery goroutine
    g.Go(func() error { return p.recoverStaleLoop(ctx) })

    // Worker goroutines
    for i := 0; i < p.workers; i++ {
        g.Go(func() error { return p.workerLoop(ctx) })
    }

    return g.Wait()
}

func (p *WorkerPool) workerLoop(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return nil
        default:
            job, err := p.queue.AcquireNext(ctx)
            if err != nil || job == nil {
                time.Sleep(500 * time.Millisecond) // poll interval
                continue
            }
            p.process(ctx, job)
        }
    }
}
```

### Processing a Job

```go
func (p *WorkerPool) process(ctx context.Context, job *Job) {
    pipe, err := p.pipelines.Get(job.Pipeline)
    if err != nil {
        p.queue.Fail(ctx, job.ID, err.Error())
        return
    }

    draft := &storage.KnowledgeObject{
        ID:         uuid.New().String(),
        RawContent: job.Payload,
        Pipeline:   job.Pipeline,
        Source:      job.Source,
        CreatedAt:  time.Now(),
    }

    for _, step := range pipe.Steps {
        draft, err = step.Run(ctx, draft)
        if err != nil {
            p.queue.Fail(ctx, job.ID, fmt.Sprintf("step %s: %s", step.Name(), err))
            return
        }
    }

    draft.UpdatedAt = time.Now()

    if err := p.store.Objects().Create(ctx, draft); err != nil {
        p.queue.Fail(ctx, job.ID, err.Error())
        return
    }

    // Write edges for mentions (ADR-049)
    for _, mention := range draft.Mentions {
        edge := &storage.Edge{
            ID:        uuid.New().String(),
            FromType:  "object",
            FromID:    draft.ID,
            ToType:    "entity",
            ToID:      mention,
            EdgeType:  "mentions",
            Weight:    1.0,
            CreatedAt: time.Now(),
        }
        p.store.Edges().Create(ctx, edge)
    }

    p.queue.Complete(ctx, job.ID, draft.ID)
}
```

---

## 3. Pipeline Runtime

### What It Does

Executes ordered sequences of steps that progressively enrich a KnowledgeObject (ADR-053).

### Package Structure

```
internal/
  pipeline/
    pipeline.go     # Pipeline, PipelineStep interface, Registry
    steps/
      typedetect.go  # Detect content type and subtype
      sectioner.go   # Decompose into sections
      tagger.go      # Extract tags
      noop.go        # Pass-through step (for testing)
```

### Interfaces

```go
// pipeline.go

type PipelineStep interface {
    Name() string
    Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error)
}

type Pipeline struct {
    Name        string
    Description string
    Steps       []PipelineStep
}

type Registry interface {
    Register(name string, p *Pipeline) error
    Get(name string) (*Pipeline, error)
    List() []string
}
```

### Built-in Steps (Phase 6 Scope)

Phase 6 ships with deterministic steps only. AI-backed enrichment steps (summarization, entity extraction, embedding generation) require AI provider integration and ship later.

| Step | What it does | AI required? |
|------|-------------|-------------|
| `typedetect` | Sets `Type` and `Subtype` from content heuristics | No |
| `sectioner` | Splits `RawContent` into `Sections` by headings/paragraphs | No |
| `tagger` | Extracts basic tags from content (keyword frequency) | No |
| `noop` | Returns draft unchanged (testing) | No |

### Built-in Pipelines

```go
func DefaultRegistry() Registry {
    r := NewRegistry()

    r.Register("text.short", &Pipeline{
        Name: "text.short",
        Steps: []PipelineStep{
            steps.NewTypeDetector(),
            steps.NewTagger(),
        },
    })

    r.Register("text.long", &Pipeline{
        Name: "text.long",
        Steps: []PipelineStep{
            steps.NewTypeDetector(),
            steps.NewSectioner(),
            steps.NewTagger(),
        },
    })

    return r
}
```

Pipeline selection: if `ctxt analyze` doesn't specify `--pipeline`, the system picks one based on content length. Short text (< 500 chars) gets `text.short`, longer text gets `text.long`.

---

## 4. Search Engine

### What It Does

Parses RSQL queries (ADR-050), compiles them to SQL, executes against storage, returns ranked results.

### Package Structure

```
internal/
  search/
    parser.go       # RSQL lexer + recursive descent parser
    ast.go          # AST node types
    compiler.go     # AST → SQL compiler
    engine.go       # QueryEngine ties it all together
    fts.go          # FTS5 query builder
```

### RSQL Grammar (ADR-050)

```
query       = or_expr
or_expr     = and_expr ( "," and_expr )*
and_expr    = constraint ( ";" constraint )*
constraint  = group | comparison
group       = "(" query ")"
comparison  = field operator value
field       = IDENTIFIER
operator    = "==" | "!=" | ">=" | "<=" | ">" | "<" | "=in=" | "=out="
value       = STRING | "(" STRING ("," STRING)* ")"
```

Operator precedence: `,` (OR) binds weaker than `;` (AND).

### AST Nodes

```go
type Node interface{ node() }

type ComparisonNode struct {
    Field    string
    Operator Operator
    Value    any       // string or []string for IN/OUT
}

type AndNode struct {
    Children []Node
}

type OrNode struct {
    Children []Node
}

type Operator int

const (
    OpEq  Operator = iota // ==
    OpNeq                 // !=
    OpGt                  // >
    OpGte                 // >=
    OpLt                  // <
    OpLte                 // <=
    OpIn                  // =in=
    OpOut                 // =out=
)
```

### Compiler

The compiler turns AST nodes into SQL WHERE clauses. Special cases:

| Field | Compilation |
|-------|-------------|
| `type`, `subtype`, `pipeline`, `source` | Direct column: `WHERE type = ?` |
| `tag` | JSON: `EXISTS (SELECT 1 FROM json_each(tags) WHERE json_each.value->>'label' = ?)` |
| `mention` | Edge join: `JOIN edges ON ... WHERE edges.to_id = ?` (ADR-049) |
| `created_at`, `updated_at` | Date comparison: `WHERE created_at >= ?` |
| `similar` | FTS5 match: `WHERE objects_fts MATCH ?` (fallback, no vector search in Phase 6) |

### Engine

```go
type Engine struct {
    store storage.StorageDriver
}

func (e *Engine) Search(ctx context.Context, query string, limit, offset int) ([]*storage.KnowledgeObject, int, error) {
    ast, err := Parse(query)
    if err != nil {
        return nil, 0, fmt.Errorf("parse error: %w", err)
    }

    sql, args, err := Compile(ast)
    if err != nil {
        return nil, 0, fmt.Errorf("compile error: %w", err)
    }

    return e.store.Objects().ListBySQL(ctx, sql, args, limit, offset)
}
```

`ListBySQL` is a storage method that executes compiled SQL. It supplements the filter-based `List` method for when the query engine needs full SQL control.

### FTS Integration

FTS5 queries use the `MATCH` syntax:

```go
func BuildFTSQuery(text string) string {
    // Escape special FTS5 characters, wrap in quotes
    return fmt.Sprintf(`"%s"`, escapeFTS(text))
}
```

Vector search is deferred — when `similar==` is used in Phase 6, it falls back to FTS5.

---

## 5. HTTP Server

### What It Does

Serves the REST API on port 8080 (ADR-051: chi, ADR-052: separate listeners).

### Package Structure

```
internal/
  server/
    http/
      server.go      # chi router setup + lifecycle
      middleware.go   # Request ID, logging, recovery, auth stub
      handlers/
        health.go     # GET /health
        analyze.go    # POST /api/v1/analyze
        objects.go    # CRUD /api/v1/objects
        jobs.go       # /api/v1/jobs
        entities.go   # /api/v1/entities
        search.go     # GET /api/v1/search (RSQL)
      errors.go       # Error envelope {error: {code, message, details}}
```

### Route Layout

```go
func NewRouter(svc *service.Service) chi.Router {
    r := chi.NewRouter()

    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)

    r.Get("/health", handlers.Health(svc))

    r.Route("/api/v1", func(r chi.Router) {
        // Objects
        r.Get("/objects", handlers.ListObjects(svc))
        r.Get("/objects/{id}", handlers.GetObject(svc))
        r.Patch("/objects/{id}", handlers.UpdateObject(svc))
        r.Delete("/objects/{id}", handlers.DeleteObject(svc))

        // Analyze (ingestion)
        r.Post("/analyze", handlers.Analyze(svc))

        // Jobs
        r.Get("/jobs", handlers.ListJobs(svc))
        r.Get("/jobs/{id}", handlers.GetJob(svc))
        r.Post("/jobs/{id}/retry", handlers.RetryJob(svc))

        // Search
        r.Get("/search", handlers.Search(svc))

        // Entities
        r.Get("/entities", handlers.ListEntities(svc))
        r.Get("/entities/{slug}", handlers.GetEntity(svc))
        r.Get("/entities/{slug}/backlinks", handlers.EntityBacklinks(svc))
    })

    return r
}
```

### Error Envelope

All errors return:

```json
{
    "error": {
        "code": "NOT_FOUND",
        "message": "object abc123 not found",
        "details": {}
    }
}
```

### Service Layer (shared with gRPC)

Both HTTP and gRPC handlers call the same service layer:

```go
// internal/service/service.go

type Service struct {
    Store    storage.StorageDriver
    Queue    jobs.JobQueue
    Pipes    pipeline.Registry
    Search   *search.Engine
}

func (s *Service) Analyze(ctx context.Context, req AnalyzeRequest) (string, error) {
    job := &jobs.Job{
        ID:         uuid.New().String(),
        Type:       "ingest:" + req.Type,
        Payload:    req.Content,
        Pipeline:   req.Pipeline,
        Source:      req.Source,
        MaxRetries: 3,
        CreatedAt:  time.Now(),
        UpdatedAt:  time.Now(),
    }

    if job.Pipeline == "" {
        job.Pipeline = s.Pipes.SelectPipeline(req.Content)
    }

    if err := s.Queue.Enqueue(ctx, job); err != nil {
        return "", err
    }
    return job.ID, nil
}

func (s *Service) GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
    return s.Store.Objects().Get(ctx, id)
}

func (s *Service) SearchObjects(ctx context.Context, query string, limit, offset int) ([]*storage.KnowledgeObject, int, error) {
    return s.Search.Search(ctx, query, limit, offset)
}

// ... remaining methods follow the same pattern
```

---

## 6. gRPC Server

### What It Does

Serves the gRPC API on port 9090 (ADR-052: separate listener).

### Package Structure

```
internal/
  server/
    grpc/
      server.go       # gRPC server setup + lifecycle
      interceptors.go # Logging, recovery interceptors
      services/
        analyze.go    # AnalyzeService
        objects.go    # ObjectService
        jobs.go       # JobService
        entities.go   # EntityService
api/
  proto/
    dpkms/v1/
      analyze.proto
      objects.proto
      jobs.proto
      entities.proto
```

### Phase 6 Scope

Proto files and gRPC service stubs. The service implementations call the same `service.Service` layer as HTTP handlers. Full gRPC streaming and advanced features ship later.

### Server Lifecycle

```go
func NewServer(svc *service.Service) *grpc.Server {
    s := grpc.NewServer(
        grpc.ChainUnaryInterceptor(
            recoveryInterceptor,
            loggingInterceptor,
        ),
    )

    pb.RegisterAnalyzeServiceServer(s, &analyzeServer{svc: svc})
    pb.RegisterObjectServiceServer(s, &objectServer{svc: svc})
    pb.RegisterJobServiceServer(s, &jobServer{svc: svc})
    pb.RegisterEntityServiceServer(s, &entityServer{svc: svc})

    return s
}
```

---

## 7. Wiring It Together

### Updated `dpkms serve`

```go
func runServe(cmd *cobra.Command, args []string) error {
    ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    // 1. Storage
    driver, err := storage.NewDriver(cfg.Storage)
    if err != nil { return err }
    defer driver.Close(ctx)

    if err := driver.Init(ctx); err != nil { return err }

    // 2. Job queue (backed by storage)
    queue := jobs.NewQueue(driver.Jobs())

    // 3. Pipeline registry
    pipes := pipeline.DefaultRegistry()

    // 4. Search engine
    searchEngine := search.NewEngine(driver)

    // 5. Service layer
    svc := &service.Service{
        Store:  driver,
        Queue:  queue,
        Pipes:  pipes,
        Search: searchEngine,
    }

    // 6. Servers + workers
    g, ctx := errgroup.WithContext(ctx)

    // HTTP
    httpRouter := httpserver.NewRouter(svc)
    httpSrv := &http.Server{Addr: fmt.Sprintf(":%d", cfg.Server.Port), Handler: httpRouter}
    g.Go(func() error { return httpSrv.ListenAndServe() })

    // gRPC
    grpcSrv := grpcserver.NewServer(svc)
    lis, _ := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Server.GRPCPort))
    g.Go(func() error { return grpcSrv.Serve(lis) })

    // Workers
    pool := jobs.NewWorkerPool(queue, pipes, driver, cfg.Server.Workers)
    g.Go(func() error { return pool.Start(ctx) })

    // Shutdown
    g.Go(func() error {
        <-ctx.Done()
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        httpSrv.Shutdown(shutdownCtx)
        grpcSrv.GracefulStop()
        return nil
    })

    return g.Wait()
}
```

### Updated `ctxt analyze`

The `ctxt` CLI talks to `dpkms` via HTTP (or gRPC). In Phase 6, it calls `POST /api/v1/analyze`:

```go
func runAnalyze(cmd *cobra.Command, args []string) error {
    // Build request from flags + args
    req := AnalyzeRequest{
        Type:    viper.GetString("type"),
        Content: getContent(args),
        Pipeline: viper.GetString("pipeline"),
        // ...
    }

    // POST to dpkms
    resp, err := httpClient.Post(dpkmsURL+"/api/v1/analyze", "application/json", marshal(req))
    // ...

    fmt.Printf("Job %s queued\n", resp.JobID)
    return nil
}
```

---

## 8. What's NOT in Phase 6

These are explicitly deferred:

| Feature | Why deferred | When |
|---------|-------------|------|
| NLQ (natural language queries) | Needs AI providers (ADR-050) | Phase 7+ |
| AI enrichment steps | Needs AI providers | Phase 7+ |
| Vector search | Needs embedding generation | Phase 7+ |
| Postgres backend | SQLite covers local-first MVP | Phase 7+ |
| Authentication middleware | ADR-023 design exists, not blocking | Phase 7+ |
| Rate limiting | Not needed for local-first | Phase 7+ |
| Composition endpoints | Needs AI providers | Phase 7+ |
| Registry sync | Needs remote registry protocol | Phase 7+ |
| Plugin system | Interfaces defined, loading deferred | Phase 8+ |
| Profiles (ranking/filtering) | Needs search + AI to be meaningful | Phase 7+ |

---

## 9. Testing Strategy

### Unit Tests

| Subsystem | What to test |
|-----------|-------------|
| Storage/SQLite | CRUD operations, migrations, edge queries, FTS indexing |
| Jobs | Enqueue, acquire, complete, fail, retry, stale recovery |
| Pipeline | Step execution, registry lookup, draft enrichment |
| Search/Parser | Valid RSQL parsing, operator precedence, error messages |
| Search/Compiler | SQL generation for each field type, AND/OR composition |
| HTTP handlers | Request parsing, response format, error envelope |

### Integration Tests

| Scenario | What it covers |
|----------|---------------|
| Analyze → Job → Pipeline → Object | Full write path |
| Search → Parser → Compiler → Results | Full read path |
| Job failure → Retry → Success | Error recovery |
| HTTP `POST /analyze` → `GET /objects` | API round-trip |

### Test Helpers

```go
// internal/storage/testutil.go
func NewTestDriver(t *testing.T) storage.StorageDriver {
    t.Helper()
    dir := t.TempDir()
    driver, err := sqlite.New(filepath.Join(dir, "test.db"))
    require.NoError(t, err)
    require.NoError(t, driver.Init(context.Background()))
    t.Cleanup(func() { driver.Close(context.Background()) })
    return driver
}
```

---

## 10. New Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/go-chi/chi/v5` | HTTP router (ADR-051) |
| `github.com/mattn/go-sqlite3` | SQLite driver (CGo) |
| `google.golang.org/grpc` | gRPC server |
| `google.golang.org/protobuf` | Protobuf codegen |
| `github.com/google/uuid` | UUID generation |
| `golang.org/x/sync/errgroup` | Concurrent goroutine management |

### CGo Note

`go-sqlite3` requires CGo. If CGo is unavailable, `modernc.org/sqlite` is a pure-Go alternative (slower, but no C compiler needed). Decision: use `go-sqlite3` by default, document the pure-Go fallback.

---

## References

- ADR-049: Edges table for mentions
- ADR-050: RSQL canonical grammar
- ADR-051: chi HTTP framework
- ADR-052: Separate HTTP/gRPC listeners
- ADR-053: KnowledgeObject as pipeline draft type
- ADR-004: Step-based pipeline architecture
- ADR-021: Multi-backend storage strategy
- ADR-023: Authentication model (deferred for Phase 6)
