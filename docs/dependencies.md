# Core Third-Party Dependencies

This document defines core third-party dependencies for both packages (dPKMS and ctxt) organized by Living Skeleton phase.

**Preference:** Wherever possible, use [Charmbracelet](https://github.com/charmbracelet) Go packages for CLI/TUI functionality.

---

## Dependency Selection Criteria

For each non-Charmbracelet dependency, we evaluate:

1. **Popular Option** - Most stars/downloads, widest adoption
2. **Complementary Option** - Best fit for package architecture/extensibility
3. **Lightweight Option** - Minimal footprint alternative

**Effort Estimate Format:** Days for integration assuming clean interfaces

---

## Phase 0: Shared Kernel

### dPKMS Core

#### Database: SQLite Driver

**Selected:** `modernc.org/sqlite` (CGo-free)

**Why:**
- Zero external dependencies (no CGo)
- Cross-platform without C compiler
- Full SQLite 3.x feature support
- 2-3 days integration (schema setup, WAL mode, migrations)

**Concurrency Strategy:**
- Use WAL mode (Write-Ahead Logging) for concurrent access
- Multiple connections via `database/sql` connection pool
- N concurrent readers + 1 writer (SQLite WAL guarantees)
- Connection pool tuning for optimal concurrency
- No blocking issues for local-first workloads
- Additional 1 day for connection pool configuration and testing

**Alternatives:**

1. **Popular:** `github.com/mattn/go-sqlite3` (~8k stars)
   - Industry standard, battle-tested
   - Requires CGo (complicates cross-compilation)

2. **Complementary:** `crawshaw.io/sqlite`
   - Connection pool built-in
   - Better concurrency primitives (this is the "SQLite non-blocking" solution)
   - Requires CGo

3. **Why modernc.org/sqlite wins:**
   - CGo-free = simpler builds
   - dPKMS must be portable
   - WAL mode provides adequate concurrency
   - Can achieve similar performance to crawshaw.io with proper pooling

#### Configuration: YAML/TOML Parser

**Selected:** `github.com/ilyakaznacheev/cleanenv` (~1.6k stars)

**Why:**
- Struct-tag based validation (declarative approach)
- Simple, minimal API
- Built-in env var mapping
- Perfect for dPKMS: one config struct, zero boilerplate
- 1 day integration

**Example:**
```go
type Config struct {
    DBPath string `yaml:"db_path" env:"DPKMS_DB_PATH" env-default:"./dpkms.db"`
    Port   int    `yaml:"port" env:"DPKMS_PORT" env-default:"8080"`
}
// Load with: cleanenv.ReadConfig("config.yaml", &cfg)
```

**Optional Addition:** `github.com/joho/godotenv` (~8k stars)
- Load `.env` files for profile-specific overrides
- 0.5 days integration
- Use with cleanenv for `.env` + YAML support

**Alternatives:**

1. **Popular:** `github.com/spf13/viper` (~27k stars)
   - Watch files, env vars, remote config
   - Overkill for local-first use case
   - 3-4 days (complex API)

2. **Complementary:** `github.com/knadh/koanf` (~2.7k stars)
   - More composable than viper
   - Clean separation of providers
   - 2 days integration
   - Choose if you need 3+ config sources or file watching

3. **Why cleanenv wins:**
   - Simplest for single YAML + env vars pattern
   - Validation built-in
   - Less code to maintain

#### Migrations

**Selected:** `github.com/pressly/goose` (~6.5k stars)

**Why:**
- Simpler API than golang-migrate
- Embedded migrations (better for compiled binaries)
- Excellent for plugin-extensible schemas
- Supports both SQL and Go migrations
- 2 days integration (define migration format, test up/down)

**Alternatives:**

1. **Popular:** `github.com/golang-migrate/migrate` (~15k stars)
   - Industry standard, more features
   - More complex API
   - 2 days integration
   - Choose if you need advanced features (multi-DB transactions, complex rollbacks)

2. **Lightweight:** Custom migration runner (100-200 LOC)
   - Apply SQL files in order
   - 1 day implementation
   - Loses versioning guarantees, not recommended

3. **Why goose wins:**
   - Simpler for our use case
   - Better embedded migration support
   - Plugin system will benefit from goose's flexibility

#### UUID Generation

**Selected (stdlib):** `github.com/google/uuid` (~5k stars)

**Why:**
- Canonical implementation
- Used everywhere
- 0.5 days (import + use)

**No alternatives needed** - this is the standard.

#### Caching: In-Memory Cache

**Selected:** `github.com/allegro/bigcache` (~7.5k stars)

**Why:**
- Zero GC overhead (off-heap storage)
- Fast concurrent access
- Perfect for caching: parsed documents, API responses, FTS results
- Simple API
- 2-3 days integration (cache policies, TTL, invalidation)

**Use Cases:**
- Cache OpenAI API responses (expensive)
- Cache parsed document content
- Cache FTS query results
- Cache embedding lookups

**Alternatives:**

1. **Complementary:** `github.com/dgraph-io/ristretto` (~5.5k stars)
   - Cost-based admission policy (more intelligent eviction)
   - Better for mixed workload sizes
   - 3-4 days (tuning admission/eviction policies)
   - Choose if you need fine-grained cache control

2. **Lightweight:** `github.com/jellydator/ttlcache` (~900 stars)
   - Simple TTL-based cache
   - 1-2 days
   - Less feature-rich

3. **Why bigcache wins:**
   - Zero GC impact (critical for Go performance)
   - Simple API for common use cases
   - Battle-tested at scale (Allegro production use)

---

## Phase 1: Echo Loop

### dPKMS

#### Job Queue: In-Process Queue

**Selected:** Custom implementation (transactional outbox pattern)

**Why:**
- Needs tight integration with SQLite transactions
- Simple polling loop sufficient for single-node
- 3-4 days (outbox table, worker loop, locking)

**Why not external queue:**
- Redis: Network dependency (violates local-first)
- NATS: Overkill for single-process
- BoltDB: Redundant with SQLite

**Future:** Plugin interface for distributed queues (Redis, NATS)

#### Logging

**Selected:** `github.com/charmbracelet/log` (~2.6k stars)

**Why:**
- Charmbracelet preference
- Beautiful terminal output
- Structured logging
- 1 day integration

**Alternatives:**

1. **Popular:** `github.com/sirupsen/logrus` (~24k stars)
   - JSON output
   - Many formatters
   - Aging, slower performance

2. **Complementary:** `go.uber.org/zap` (~21k stars)
   - Fastest structured logger
   - Production-grade
   - 2 days (more verbose API)

3. **Lightweight:** `log/slog` (stdlib, Go 1.21+)
   - Built-in structured logging
   - 0.5 days integration

**Recommendation:** Use `charmbracelet/log` for consistency.

**Why not `rs/zerolog`?** (~10k stars)
- zerolog is faster (zero-allocation)
- Better for high-throughput servers
- JSON output focused (less beautiful terminal output)
- Our use case: CLI tool prioritizes UX over raw performance
- Could reconsider for dPKMS server component if performance critical

#### Development Tool: Hot Reload

**Selected:** `github.com/cosmtrek/air` (~17k stars)

**Why:**
- Auto-rebuild on file changes
- Excellent developer experience
- Watch Go files, config files, templates
- 0.5 days (create `.air.toml` config)

**Note:** Development tool only, not a production dependency

**Configuration Example:**
```toml
[build]
  cmd = "go build -o ./tmp/dpkms ./cmd/dpkms"
  include_ext = ["go", "yaml"]
  exclude_dir = ["tmp", "vendor"]
```

### ctxt

#### CLI Framework

**Selected:** `github.com/spf13/cobra` (~38k stars)

**Why:**
- Industry standard for Go CLIs
- Subcommands, flags, help generation
- Command hierarchy (`ctxt analyze`, `ctxt list`, etc.)
- Integrates well with Charmbracelet ecosystem
- 2 days (command structure, flag binding)

**Why not `charmbracelet/gum`?**
- Gum is for **shell scripts**, not Go applications
- Gum is a standalone binary that Bash/Zsh scripts call
- Cobra provides **application structure** (routing, subcommands, flags)
- Gum provides **interactive elements** (prompts, spinners, inputs)
- **They solve different problems** - not alternatives

**Alternatives:**

1. **Popular:** (already selected)

2. **Complementary:** `github.com/urfave/cli` (~22k stars)
   - Simpler API
   - Less feature-rich
   - 1.5 days

3. **Lightweight:** `github.com/alexflint/go-arg` (~2k stars)
   - Struct-based flag parsing
   - Too minimal for ctxt's needs
   - 1 day

**Recommendation:** Use Cobra for structure, add Charmbracelet components for interaction.

#### Interactive Prompts & Forms

**Selected (Charmbracelet):** `github.com/charmbracelet/huh` (~4.5k stars)

**Why:**
- Charmbracelet's form/input library (Gum's Go equivalent)
- Beautiful prompts, confirmations, multi-select
- Use for interactive workflows
- 2 days (form definitions, validation)

**Use cases:**
```go
// Interactive analysis workflow
huh.NewForm(
  huh.NewInput().Title("Enter URL"),
  huh.NewConfirm().Title("Extract mentions?"),
).Run()
```

**Alternatives:**

1. **Shell-based:** `charmbracelet/gum` binary
   - Call from `exec.Command()`
   - Awkward from Go code
   - 2 days (command wrapping)

2. **Survey:** `github.com/AlecAivazis/survey` (~4k stars)
   - Similar to huh
   - Less modern design
   - 2 days

**Recommendation:** Use `huh` for interactive ctxt workflows (e.g., `ctxt add --interactive`).

#### Table Formatting

**Selected (Charmbracelet):** `github.com/charmbracelet/lipgloss` (~8k stars)

**Why:**
- Charmbracelet preference
- Rich terminal styling
- Layout primitives
- 2 days (learn layout model, style definitions)

**Use with:** `github.com/charmbracelet/bubbles/table` for interactive tables

**No alternatives needed** - this is the Charmbracelet solution.

#### Terminal Detection

**Selected (Charmbracelet):** `github.com/charmbracelet/x/term`

**Why:**
- Charmbracelet ecosystem
- Terminal capability detection
- Width/height, color support
- 0.5 days

---

## Phase 2: Real Data and Queries

### dPKMS

#### FTS (Full-Text Search)

**Selected:** SQLite FTS5 (built-in)

**Why:**
- Zero dependencies
- Integrated with storage
- Adequate performance for local workloads
- 2 days (schema design, query integration)

**Alternatives:**

1. **Popular:** `github.com/blevesearch/bleve` (~9.9k stars)
   - Pure Go FTS engine
   - Advanced features (facets, highlighting)
   - 5-7 days (separate index, sync logic)

2. **Complementary:** `github.com/blugelabs/bluge` (~1.7k stars)
   - Successor to Bleve
   - Better performance
   - 5-7 days

3. **Why SQLite FTS5 wins:**
   - Already have SQLite
   - Transactional consistency
   - Simpler architecture

#### RSQL Parser

**Selected:** Custom implementation

**Why:**
- RSQL is simple (BNF fits on one page)
- AST generation required for multi-backend dispatch
- 4-5 days (lexer, parser, AST nodes)

**Alternatives:**

1. **Popular:** `github.com/alecthomas/participle` (~3.5k stars)
   - Parser generator from structs
   - 3 days (grammar definition, AST mapping)

2. **Complementary:** `github.com/antlr/antlr4` (external tool)
   - Industry-standard parser generator
   - Overkill, requires Java tooling
   - 7+ days

3. **Lightweight:** `github.com/timtadh/lexmachine` (~525 stars)
   - Lexer only
   - Still need parser
   - 4 days total

**Recommendation:** Use `participle` if unfamiliar with parser construction, else custom.

#### HTTP Client (Registry Fetching)

**Selected:** `github.com/go-resty/resty` (~10k stars)

**Why:**
- Fluent API (cleaner than stdlib)
- Retry logic built-in (exponential backoff)
- Timeout handling
- Debug logging
- 1.5 days integration (configure retry, auth, caching)

**Saves 0.5 days vs. custom implementation**

**Example:**
```go
client := resty.New().
    SetRetryCount(3).
    SetTimeout(30 * time.Second)

resp, err := client.R().
    SetHeader("Authorization", token).
    Get("https://registry.example.com/packages")
```

**Alternatives:**

1. **Stdlib:** `net/http` + custom retry
   - More control, more code
   - 2 days (retry logic, auth headers, caching)
   - Choose if you need very specific retry behavior

2. **Complementary:** `github.com/hashicorp/go-retryablehttp` (~2k stars)
   - Minimal wrapper around stdlib
   - Exponential backoff
   - 1 day
   - More lightweight than resty

3. **Why resty wins:**
   - Time savings (0.5 days)
   - Cleaner code
   - Well-maintained, battle-tested
   - Easy to add later if starting with stdlib

### ctxt

#### LLM Client: OpenAI

**Selected:** `github.com/sashabaranov/go-openai` (~9k stars)

**Why:**
- Most popular Go OpenAI client
- Supports all API endpoints
- Streaming support
- 2-3 days (API key config, prompt templates, error handling)

**Alternatives:**

1. **Popular:** (already selected)

2. **Complementary:** `github.com/tmc/langchaingo` (~4.3k stars)
   - LangChain for Go
   - Multi-provider support (OpenAI, Anthropic, local)
   - 5-7 days (learn abstractions, adapt to dPKMS pipeline model)

3. **Lightweight:** Direct HTTP calls to OpenAI API
   - ~200 LOC
   - 2 days (JSON marshaling, streaming)

**Recommendation:** Start with `go-openai`, migrate to `langchaingo` in Phase 5 (multi-provider).

#### Markdown Processing

**Selected (Charmbracelet):** `github.com/charmbracelet/glamour` (~2.4k stars)

**Why:**
- Charmbracelet preference
- Terminal-rendered Markdown
- Integrates with lipgloss
- 1 day (output formatting)

**Alternatives:**

1. **Popular:** `github.com/yuin/goldmark` (~3.6k stars)
   - Pure Markdown parser
   - Extensible
   - No terminal rendering

2. **Complementary:** `github.com/gomarkdown/markdown` (~1.4k stars)
   - CommonMark compliant
   - AST-based

3. **Why glamour wins:**
   - Built for terminal display
   - ctxt outputs to terminals primarily

#### Fuzzy String Matching

**Selected:** `github.com/hbollon/go-edlib` (~480 stars)

**Why:**
- Edit distance algorithms (Levenshtein, Jaro-Winkler, etc.)
- Unicode-compatible string comparison
- Useful for: typo-tolerant search, fuzzy tag matching
- 2 days integration (integrate with FTS queries)

**Use Cases:**
- Typo tolerance in search: "dpkm" → "dpkms"
- Fuzzy tag matching: "machinelearnig" → "machinelearning"
- Similar document detection

**Alternatives:**

1. **Custom implementation:** Levenshtein algorithm (~50 LOC)
   - 1 day
   - Limited to one algorithm
   - go-edlib provides multiple algorithms

2. **Why go-edlib wins:**
   - Multiple algorithms for different use cases
   - Well-tested
   - Small dependency

**Note:** Optional enhancement, not critical for MVP

---

## Phase 3: Beyond Text

### dPKMS

#### Graph Database (Mentions/Backlinks)

**Selected:** SQLite tables + manual adjacency lists

**Why:**
- Simple graph queries (1-2 hops max)
- SQLite adequate for local-first scale
- 3-4 days (schema, backlink indexing, traversal queries)

**Alternatives:**

1. **Popular:** `github.com/dgraph-io/badger` (~13.8k stars)
   - Embedded KV store
   - Can build graph on top
   - 7-10 days (dual storage, sync logic)

2. **Complementary:** `github.com/cayleygraph/cayley` (~14.8k stars)
   - Full graph database
   - Overkill for local workloads
   - 10+ days (integration, query translation)

3. **Lightweight:** Manual SQL JOIN queries
   - (already selected)

**Future:** Plugin interface for graph backends (Neo4j connector, etc.)

### ctxt

#### URL Fetching & HTML Parsing

**Selected:** Dual-library approach

**1. Web Crawler:** `github.com/gocolly/colly` (~23k stars)

**Why:**
- Structured crawling framework
- Built-in rate limiting, robots.txt compliance
- Automatic link following
- Concurrent scraping
- 3-4 days (crawler rules, queue management)

**Use Cases:**
- Multi-page documentation ingestion
- Recursive website crawling
- Sitemap following

**Example:**
```go
c := colly.NewCollector(
    colly.AllowedDomains("docs.example.com"),
)
c.OnHTML("a[href]", func(e *colly.HTMLElement) {
    e.Request.Visit(e.Attr("href"))
})
```

**2. HTML Parser:** `github.com/PuerkitoBio/goquery` (~14k stars)

**Why:**
- jQuery-like selectors
- Precise content extraction
- Used internally by colly
- 2 days (selectors, content extraction)

**Use Cases:**
- Single-page content extraction
- Fine-grained DOM manipulation
- Custom parsing logic

**Total Integration:** 5-6 days for complete crawler + parser

**Alternatives:**

1. **Browser automation:** `github.com/playwright-community/playwright-go`
   - Full browser (handles JS rendering)
   - Overkill for static content
   - 5-7 days (browser management, resource overhead)
   - Choose if target sites require JavaScript execution

2. **Stdlib only:** `golang.org/x/net/html`
   - Manual tree walking
   - No crawler features
   - 3 days (more verbose)
   - Not recommended

**Why dual approach wins:**
- colly handles crawling concerns (rate limits, robots.txt)
- goquery handles parsing (clean, expressive selectors)
- colly uses goquery-compatible APIs internally
- Best of both worlds

#### HTML to Markdown Conversion

**Selected:** `github.com/JohannesKaufmann/html-to-markdown` (~2k stars)

**Why:**
- Clean Markdown output
- Configurable rules
- Handles tables, lists, code blocks
- 1.5 days (configure rules, handle edge cases)

**Alternatives:**

1. **Popular:** `github.com/lunny/html2md` (~125 stars)
   - Simpler, less configurable
   - 1 day

2. **Complementary:** Custom conversion (goquery → strings)
   - Full control
   - 3-4 days (implement Markdown spec)

3. **Why html-to-markdown wins:**
   - Battle-tested
   - Handles edge cases

#### OCR: Tesseract Bindings

**Selected:** `github.com/otiai10/gosseract` (~2.7k stars)

**Why:**
- Go bindings for Tesseract
- Most mature option
- 3-4 days (install Tesseract, language packs, image preprocessing)

**Alternatives:**

1. **Popular:** (already selected)

2. **Complementary:** Cloud OCR APIs (Google Vision, AWS Textract)
   - Network dependency (violates local-first)
   - Fallback option for plugin

3. **Lightweight:** `github.com/GeertJohan/go.tesseract` (~36 stars)
   - Unmaintained
   - Not recommended

**Note:** Tesseract must be installed separately (not Go-pure).

---

## Phase 4: Interfaces & Independence

### dPKMS

#### gRPC

**Selected:** `google.golang.org/grpc` (~21k stars)

**Why:**
- Official gRPC implementation
- Required for gRPC API
- 4-5 days (proto definitions, codegen, service impl)

**No alternatives** - this is the standard.

#### Protocol Buffers

**Selected:** `google.golang.org/protobuf` (official)

**Why:**
- Required for gRPC
- 2 days (schema design, codegen setup)

**No alternatives** - this is the standard.

#### REST Framework

**Selected:** `github.com/go-chi/chi` (~18k stars)

**Why:**
- Lightweight, composable router
- Stdlib-compatible middleware
- 2-3 days (route setup, middleware, JSON marshaling)

**Alternatives:**

1. **Popular:** `github.com/gin-gonic/gin` (~78k stars)
   - Fastest Go web framework
   - More opinionated
   - 3 days

2. **Complementary:** `github.com/labstack/echo` (~29k stars)
   - High performance
   - Built-in middleware
   - 3 days

3. **Lightweight:** `net/http` stdlib + `github.com/gorilla/mux` (~20k stars)
   - Manual composition
   - 2 days

**Recommendation:** Use `chi` for balance of simplicity and features.

### ctxt

#### Profile Management

**Selected:** Custom implementation (profile struct + config files)

**Why:**
- Profiles are ctxt-specific domain logic
- Simple YAML files per profile
- 2 days (YAML loading, validation, switching)

**No external dependency needed.**

---

## Phase 5: Semantics & Sidecars

### dPKMS

#### Vector Database Interface

**Selected:** `github.com/philippgille/chromem-go` (~1k stars, rapidly growing)

**Why:**
- Pure Go embeddable vector database (no CGo, no build complexity)
- Chroma-compatible API (familiar to AI engineers)
- Built-in persistence to disk
- HNSW index for similarity search
- 4-5 days integration (configure persistence, query interface)

**Example:**
```go
db := chromem.NewDB()
collection := db.CreateCollection("documents", nil, nil)
collection.Add(ctx, embeddings, documents, ids)
results := collection.Query(ctx, queryEmbedding, 10)
```

**Alternatives:**

1. **SQLite-based:** `github.com/asg017/sqlite-vec` (~4k stars)
   - SQLite extension for vectors
   - Requires CGo-free compilation tricks OR building C extension
   - Better for unified SQL + vector queries
   - 4-5 days (compile extension, query integration)
   - Choose if deep SQLite integration is priority

2. **Cloud:** `github.com/pinecone-io/go-pinecone` (~300 stars)
   - Pinecone cloud SDK
   - Network dependency (violates local-first)
   - 3 days (API integration)
   - Plugin option for cloud deployments

3. **Pure Go HNSW:** `github.com/chewxy/hnsw` (~480 stars)
   - Lower-level HNSW library
   - More manual persistence
   - 5-6 days (index management, persistence layer)

**Why chromem-go wins:**
- Zero CGo = simpler builds (aligns with modernc.org/sqlite choice)
- Batteries-included (persistence, collections, metadata)
- Easy to start, easy to deploy
- Plugin interface allows swapping to sqlite-vec later if needed

**Plugin Interface:** Still provide abstraction for alternative backends

#### Plugin System

**Selected:** Go plugin system OR compiled-in plugins

**Why:**
- Go plugins (`plugin` package) are platform-limited
- Recommend compiled-in plugins with interface registration
- 5-7 days (plugin interface, discovery, lifecycle)

**Alternatives:**

1. **Go stdlib plugins:** `plugin` package
   - Linux/macOS only
   - CGo required
   - Fragile across Go versions
   - 3-4 days (if it works)

2. **Hashicorp go-plugin:** `github.com/hashicorp/go-plugin` (~5k stars)
   - RPC-based plugins (subprocesses)
   - Cross-platform
   - More complex
   - 7-10 days

3. **Compiled-in + Registry:** (recommended)
   - Plugins are Go packages
   - Register via init() functions
   - Rebuild to add plugins
   - 5-7 days (interface design, registry, examples)

**Recommendation:** Start with compiled-in, consider hashicorp/go-plugin for v2.

### ctxt

#### Embedding Model Client

**Selected:** `github.com/sashabaranov/go-openai` (already used)

**Why:**
- OpenAI embeddings API
- 1 day (reuse existing client)

**Alternatives:**

1. **Local:** `github.com/go-skynet/go-llama.cpp` (~1k stars)
   - Local embeddings (llama.cpp bindings)
   - 5-7 days (model download, CGo, inference)

2. **Multi-provider:** `github.com/tmc/langchaingo`
   - Supports OpenAI, Cohere, HuggingFace
   - 3-4 days

**Recommendation:** Reuse OpenAI client, add local option in Phase 7.

---

## Phase 6: Polish & Multimedia

### ctxt

#### Audio Transcription

**Selected:** OpenAI Whisper API

**Why:**
- Cloud-based, high quality
- Use existing `go-openai` client
- 2 days (file upload, polling)

**Alternatives:**

1. **Local:** `github.com/ggerganov/whisper.cpp` bindings
   - No mature Go bindings yet
   - 7-10 days (CGo, model management)

2. **Cloud:** AssemblyAI, Deepgram
   - Separate SDKs
   - 3 days each

**Recommendation:** Start with OpenAI, add local whisper.cpp in Phase 7.

#### Video Processing

**Selected:** `github.com/giorgisio/goav` (~2.1k stars)

**Why:**
- FFmpeg Go bindings
- Frame extraction, metadata
- 5-7 days (FFmpeg install, video decoding, keyframe extraction)

**Alternatives:**

1. **Popular:** (already selected)

2. **Command-line FFmpeg:**
   - Shell out to `ffmpeg` binary
   - 2-3 days (command construction, parsing output)

3. **Why goav wins:**
   - Programmatic control
   - Better error handling
   - Faster for frame extraction

**Note:** FFmpeg must be installed separately.

#### PDF Processing

**Selected:** `github.com/unidoc/unipdf` (~1.3k stars)

**Why:**
- Pure Go PDF parsing
- Text extraction, metadata
- 3-4 days (handle various PDF formats)

**Alternatives:**

1. **Popular:** `github.com/ledongthuc/pdf` (~1.2k stars)
   - Simpler API
   - Less feature-rich
   - 2 days

2. **Commercial:** UniDoc commercial license
   - Advanced features (OCR, forms)
   - Paid license required

3. **Command-line:** `pdftotext` (poppler-utils)
   - Shell out
   - 1 day

**Recommendation:** Start with `ledongthuc/pdf`, upgrade to unipdf if needed.

#### Observability: Metrics

**Selected:** `github.com/prometheus/client_golang` (~5.5k stars)

**Why:**
- Industry standard for metrics
- Expose `/metrics` endpoint for monitoring
- Track: query latency, cache hits, document counts, ingestion rates
- Integrates with Prometheus/Grafana
- 3-4 days (instrumentation, metrics definition, endpoint)

**Key Metrics:**
```go
// Query performance
queryDuration := prometheus.NewHistogram(...)
cacheHitRate := prometheus.NewCounter(...)

// Storage metrics
documentCount := prometheus.NewGauge(...)
storageSize := prometheus.NewGauge(...)

// Pipeline metrics
ingestionRate := prometheus.NewCounter(...)
pipelineErrors := prometheus.NewCounter(...)
```

**Alternatives:**

1. **OpenTelemetry:** `go.opentelemetry.io/otel`
   - More complex, supports tracing + metrics
   - 5-7 days
   - Choose if you need distributed tracing

2. **Custom metrics:** Log-based metrics
   - Parse logs for metrics
   - 1-2 days
   - Not recommended (less tooling support)

**Why Prometheus wins:**
- Industry standard
- Rich ecosystem (Grafana dashboards)
- Simple HTTP endpoint
- Production-ready

**Note:** Optional for MVP, critical for production deployment

---

## Phase 7: Sovereign & Scalable

### ctxt

#### Local LLM Support (Ollama)

**Selected:** `github.com/ollama/ollama` HTTP API

**Why:**
- Ollama manages models locally
- HTTP API (no special Go client needed)
- 2-3 days (HTTP client, streaming, model selection)

**Alternatives:**

1. **llama.cpp:** `github.com/go-skynet/go-llama.cpp`
   - Direct bindings
   - More control, more complexity
   - 7-10 days (model management, CGo)

2. **LocalAI:** `github.com/mudler/LocalAI`
   - OpenAI-compatible API for local models
   - 2-3 days (same as Ollama)

**Recommendation:** Support both Ollama and llama.cpp via plugin interface.

---

## Phase 8: Trust & Automation

### dPKMS

#### Cryptography (Signatures, Encryption)

**Selected (stdlib):** `crypto/*` packages

**Why:**
- Ed25519 signatures: `crypto/ed25519`
- AES encryption: `crypto/aes`
- 4-5 days (key generation, signing, verification, encrypted storage)

**Alternatives:**

1. **Popular:** `github.com/ProtonMail/gopenpgp` (~1.1k stars)
   - PGP implementation
   - Overkill for our use case
   - 5-7 days

2. **Complementary:** `golang.org/x/crypto`
   - Extended crypto (Argon2, ChaCha20)
   - 4-5 days

**Recommendation:** Use stdlib, add `x/crypto` for password hashing (Argon2).

#### File Watching (Background Ingest)

**Selected:** `github.com/fsnotify/fsnotify` (~9.6k stars)

**Why:**
- Cross-platform file watching
- Stdlib-compatible
- 2-3 days (watch directories, debounce events, trigger ingestion)

**Alternatives:**

1. **Popular:** (already selected)

2. **Polling:** Manual directory scanning
   - 1 day (simple loop)
   - Less efficient

**No better alternative.**

---

## Phase 9: Proof of Platform

### ctxt

#### TUI Framework

**Selected (Charmbracelet):** `github.com/charmbracelet/bubbletea` (~27k stars)

**Why:**
- Charmbracelet preference
- Elm Architecture for Go
- 7-10 days (learn model-update-view pattern, build interactive UI)

**Use with:**
- `github.com/charmbracelet/bubbles` - Pre-built components (list, table, input)
- `github.com/charmbracelet/lipgloss` - Styling

**Alternatives:**

1. **Popular:** `github.com/rivo/tview` (~10k stars)
   - Widget-based TUI
   - Less modern architecture
   - 5-7 days

2. **Complementary:** `github.com/jroimartin/gocui` (~9.8k stars)
   - Low-level TUI library
   - More manual control
   - 7-10 days

**Recommendation:** Use Bubbletea - aligns with Charmbracelet ecosystem.

#### Browser Extension: Web Bridge

**Selected:** WebSocket server in `dpkms serve`

**Why:**
- Browser extensions can't directly access SQLite
- WebSocket = real-time updates
- 3-4 days (WebSocket handler, auth, message protocol)

**Alternatives:**

1. **Native Messaging:** Chrome/Firefox native messaging protocol
   - More complex setup
   - 5-7 days

2. **HTTP polling:** Extension polls REST API
   - Simpler but less efficient
   - 2 days

**Recommendation:** Use WebSocket for real-time, fallback to REST for simple cases.

---

## Summary: Charmbracelet Usage

| Package | Purpose | Phase |
|---------|---------|-------|
| `charmbracelet/log` | Structured logging | 1 |
| `charmbracelet/lipgloss` | Terminal styling | 1 |
| `charmbracelet/bubbles/table` | Table rendering | 1 |
| `charmbracelet/x/term` | Terminal detection | 1 |
| `charmbracelet/huh` | Interactive forms/prompts | 1 |
| `charmbracelet/glamour` | Markdown rendering | 2 |
| `charmbracelet/bubbletea` | TUI framework | 9 |
| `charmbracelet/bubbles` | TUI components (list, table, etc.) | 9 |

**Other Charmbracelet packages to consider:**
- `charmbracelet/x/editor` - Invoke $EDITOR from Go
- `charmbracelet/x/exp/ordered` - Ordered maps (if needed)
- `charmbracelet/x/ansi` - ANSI sequence parsing
- `charmbracelet/x/exp/golden` - Golden file testing

**Why not `charmbracelet/gum`?**
- Gum is a standalone binary for shell scripts, not a Go library
- Use `huh` instead for interactive Go applications
- Use `bubbletea` for full TUI applications
- Gum is great for Bash/Zsh scripts that call ctxt

---

## Dependency Management

### Go Modules

**Selected:** `go.mod` + `go.sum` (stdlib)

**Why:**
- Standard Go dependency management
- Vendoring optional (`go mod vendor`)

### Build Tags

Use build tags for optional dependencies:

```go
//go:build with_vectors
// +build with_vectors

package storage

import "github.com/asg017/sqlite-vec"
```

Allows building without heavy dependencies:
```bash
go build                    # Minimal build
go build -tags with_vectors # Full build
```

---

## Testing Dependencies

### Test Framework

**Selected (stdlib):** `testing` package

**Why:**
- Sufficient for most tests
- Table-driven tests
- Subtests

**Add:** `github.com/stretchr/testify` (~23k stars) for assertions
- 0.5 days (learn assert API)

### Mocking

**Selected:** `github.com/stretchr/testify/mock` (part of testify)

**Why:**
- Interface mocking
- Call verification
- 1-2 days (generate mocks, write tests)

**Alternative:** `github.com/golang/mock` (deprecated, use `go.uber.org/mock`)

### Integration Tests

**Selected:** `github.com/ory/dockertest` (~4k stars)

**Why:**
- Spin up Postgres, Redis in Docker for tests
- 2-3 days (write Docker-based integration tests)

---

## CI/CD Dependencies

**Not Go packages, but important:**

- **GitHub Actions** - CI pipeline
- **GoReleaser** - Binary releases
- **golangci-lint** - Linting
- **goreleaser/nfpm** - Package (.deb, .rpm)

---

## Total Effort Estimate by Phase

| Phase | dPKMS Days | ctxt Days | Total | Notes |
|-------|------------|-----------|-------|-------|
| 0: Shared Kernel | 9 | 0 | 9 | +2d (caching, cleanenv, WAL tuning) |
| 1: Echo Loop | 9 | 4 | 13 | +1d (air setup) |
| 2: Real Data | 9 | 10 | 19 | -0.5d (resty), +2d (fuzzy search) |
| 3: Beyond Text | 8 | 12 | 20 | +3d (colly integration) |
| 4: Interfaces | 12 | 4 | 16 | No change |
| 5: Semantics | 14 | 5 | 19 | No change (chromem-go same effort as sqlite-vec) |
| 6: Polish | 7 | 15 | 22 | +4d (prometheus metrics) |
| 7: Sovereign | 2 | 12 | 14 | No change |
| 8: Trust | 10 | 4 | 14 | No change |
| 9: Proof | 7 | 15 | 22 | No change |
| **Total** | **87** | **81** | **168** | +11 days (+7%) |

**Note:** These are integration days, not total implementation. Assumes clean interfaces and iterative development.

---

## Dependency License Audit

All listed dependencies use permissive licenses:
- MIT: Most Charmbracelet packages, many community packages
- BSD: Go stdlib, some parsers
- Apache 2.0: gRPC, Protocol Buffers, some cloud SDKs

**No GPL dependencies** - all compatible with proprietary forks if needed.

---

## Upgrade Strategy

### Semantic Versioning

Pin major versions in `go.mod`:
```
require (
    github.com/charmbracelet/lipgloss v0.13.0
    github.com/spf13/cobra v1.8.0
)
```

### Periodic Updates

Review dependencies quarterly:
```bash
go list -u -m all
go get -u ./...
go mod tidy
```

### Security Scanning

Use `govulncheck`:
```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

---

## Future Considerations

### Phase 10+ (Not in Living Skeleton)

- **Real-time Sync:** CRDTs (e.g., `github.com/ipfs/go-ipfs-ds-crdt`)
- **P2P:** libp2p for decentralized registry sync
- **Mobile:** Gomobile bindings
- **WASM:** Compile ctxt to WASM for browser usage

---

## Changelog

### Version 1.1 (2026-01-27)

**Major Changes:**

1. **SQLite Concurrency Clarification**
   - Added explicit WAL mode strategy
   - Documented connection pooling approach
   - Clarified that modernc.org/sqlite is non-blocking with proper configuration
   - Removed misleading bbolt suggestion

2. **Configuration: Switched to cleanenv**
   - Changed from koanf to cleanenv for simpler struct-tag based config
   - Added godotenv as optional companion for .env file support
   - Rationale: Simpler for single YAML + env vars pattern

3. **Migrations: Switched to goose**
   - Changed from golang-migrate to goose
   - Rationale: Simpler API, better embedded migrations

4. **Added Critical Missing Dependencies:**
   - **Caching:** Added bigcache (Phase 0) - was completely missing!
   - **Hot Reload:** Added air dev tool (Phase 1)
   - **HTTP Client:** Changed from stdlib to resty (saves 0.5 days)
   - **Fuzzy Search:** Added go-edlib (Phase 2)
   - **Web Crawler:** Added colly alongside goquery (Phase 3)
   - **Metrics:** Added prometheus client (Phase 6)

5. **Vector Database: Switched to chromem-go**
   - Changed from sqlite-vec to chromem-go as default
   - Rationale: Pure Go (no CGo), simpler builds, batteries-included
   - sqlite-vec remains as alternative for deep SQLite integration

6. **Effort Estimates Updated:**
   - Total increased from 157 to 168 days (+7%)
   - More realistic with additional tooling
   - Includes proper caching, observability, dev tools

7. **URI scheme registration: Added hop.top/hdl**
   - Self-owned package at `hop.top/hdl`
   - Replaces manual OS-specific code (LSSetDefaultHandlerForURLScheme, xdg-mime, registry)
   - Used by `ctxt uri register` to make `ctxt://` links OS-clickable
   - Phase 9 (T-0158)

**Comparison with awesome-go:**
- Validated choices against community standards
- Added missing categories (caching, metrics, dev tools)
- Prioritized CGo-free options where possible
- Maintained Charmbracelet preference for CLI/TUI

---

**Last Updated:** 2026-01-27
**Version:** 1.1
