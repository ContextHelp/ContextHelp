# Testing

This document outlines the testing strategy for ContextHelp. The goal is to ensure that the engine, registries, pipelines, storage backends, ingestion workflows, and APIs behave predictably, safely, and consistently across decentralized and local-first environments.

---

## Philosophy

Testing in ContextHelp follows four principles:

- **Determinism:** Given the same input and configuration, results must remain stable.
- **Isolation:** Tests should run without external registry access unless explicitly enabled.
- **Mockability:** Pipelines, registries, storage backends, and ingestion jobs must expose interfaces enabling full mocking.
- **Reproducibility:** All tests must run identically in local, CI, and offline conditions.

---

## Test Categories

### Unit Tests

Small, fast tests targeting individual components:

- Pipeline steps (input parsing, normalization, tagging logic)
- Registry client interfaces (mocked responses)
- Storage engines (SQLite, file-based providers, Postgres)
- Configuration loader (YAML/JSON polymorphic config, plugin config, overrides)
- Query language parser → AST → visitor execution
- Queue primitives (job creation, transitions, retry logic)
- Utility functions (hint parsing, taxonomy resolution, merging behavior)

Unit tests **must not require** network or external registry access.

---

### Integration Tests

Integration tests verify the behavior of full subsystems:

- Pipeline execution using mock LLM/embedding providers
- Bookmark creation and retrieval through the ingestion **jobs system**
- Multi-step pipeline orchestration (short/long text, URL, image, audio, video)
- Local + remote registry merging logic
- Queue execution with retry semantics and crash recovery
- Query execution via AST → SQL → in-memory merge
- CLI command behavior using isolated temporary directories

Integration tests may use **file-based registry fixtures** to simulate external providers.

---

### End-to-End Tests

End-to-end tests simulate real usage of ContextHelp:

- Running `ch analyze` to enqueue jobs and verifying worker completion
- Worker crash-and-resume scenarios that ensure job recovery
- Starting the HTTP server and issuing REST/gRPC calls
- Validating correct bookmark creation, retrieval, translation, and ranking
- Full scatter–gather search across local storage + registry providers
- Consistency between CLI, REST, and gRPC responses
- Using mock external registries served through ephemeral HTTP servers

E2E tests run more slowly but validate complete engine behavior.

---

## Test Fixtures

Fixtures must exist inside `testdata/` and include:

- Example text, URL, image, audio, and video inputs
- Sample ingestion jobs (pending, running, completed)
- Bookmark JSON examples before/after processing
- Registry fixture files (taxonomy, bookmarks, weights)
- Pipeline configuration examples
- Expected output samples (golden files)

Golden files must be:

- Human-readable
- Stable over time
- Version-controlled

---

## Mocking Strategy

All major subsystems expose interfaces enabling mocking and deterministic testing.

### Mock Registry Providers

Simulate:

- Taxonomy registries
- Bookmark registries
- Tag / label translation registries
- Agent-specific worldview registries

Used to test:

- Tag validation
- Registry merging
- Localization override logic
- Multi-source search paths

### Mock Pipeline Steps

Replace AI calls with:

- Static canned responses
- Deterministic test doubles

Used to test:

- Pipeline branching
- Error propagation
- Multi-step workflow semantics
- I18N plugin behavior (summary translation, label mapping)

### Mock Storage Engines

Used to test:

- CRUD behavior
- Job recording and status transitions
- Transactional outbox characteristics
- Bookmark indexing and retrieval

---

## Queue & Ingestion Job Testing

The ingestion system uses a **transactional outbox pattern**, so the queue subsystem requires deep testing.

Tests must cover:

- Creation of new jobs in `Pending` state
- Pipeline execution in **worker mode**
- Behavior of `Running` jobs when the worker crashes
- Job retry logic with exponential backoff
- Job failure classification (fatal vs retryable)
- Idempotent job processing (no duplicate bookmarks)
- `job_steps` tracing correctness

Use in-memory queue implementations or SQLite WAL mode for deterministic results.

---

## Query Language & Search Testing

Testing must ensure the correctness of the query language, AST parsing, and search plan generation.

Tests include:

- AST parsing of nested Boolean expressions
- Visitor transformation into SQL filters
- Visitor transformation into semantic search queries
- Exclusion logic (`NOT`) correctly pushed down to storage when possible
- Pagination correctness (no in-memory overfetching unless necessary)
- Deduplication across multiple result sources
- Ranking and reranking logic

---

## API Testing

### CLI Testing

- Use temporary isolated directories for storage/config
- Snapshot testing for CLI output formatting
- Tests for:
  - `analyze` (job creation)
  - `jobs` (status, retry, list)
  - `list` (sorting, filters, pagination)
  - `delete`, `edit`, `alias` plugin commands (when applicable)

### REST Testing

Tests validate:

- Request handling and error responses
- Query parameters including AST-based unified search
- Pagination and streaming endpoints
- Content negotiation for language preferences
- Authentication / authorization (if enabled)

### gRPC Testing

Validate:

- Message contracts
- Unary + streaming functionality
- Interoperability with worker job APIs
- Correct mapping of query AST into RPC fields

Use in-process gRPC servers for performance and isolation.

---

## Cross-Platform Testing

ContextHelp must run reliably on:

- macOS
- Linux
- Windows (CLI, worker, server)

Platform-specific tests include:

- File permission handling
- Lock file behavior (when enabled)
- Path resolution in configuration files

---

## Continuous Integration

CI should:

- Run unit + integration tests on every PR
- Run E2E tests on main branch merges
- Enforce formatters, linters, static analysis
- Validate builds for all supported OS targets
- Run Go race detector (`-race`) to ensure concurrency safety

CI artifacts should include:

- Coverage reports
- Golden file diffs
- Build binaries (optional)
- Search performance metrics (optional)

---

## Performance Testing

Recommended tests include:

- Bookmark storage write throughput
- Query execution speed (AST → SQL → ranking)
- Pipeline throughput (with mock LLMs)
- Registry resolution under load
- Multi-source scatter–gather merging performance

Performance tests ensure predictable scaling as registries, pipelines, and plugins grow.

---

## Security Testing

Security-related tests include:

- Registry response sanitization
- Input validation and safe parsing (HTML, markdown, OCR output)
- Reserved namespace validation for tags/taxonomy
- API-level injection protections
- Permissions model for plugins (screen access, clipboard access, file access)

Fuzz testing is recommended for:

- Configuration parsers
- Registry loader logic
- Query language parser
- Pipeline routing logic

---

## Offline Mode Testing

Since ContextHelp is local-first, tests must validate:

- Correct behavior with **zero network access**
- Correct fallback to cached registry data
- Correct handling of stale or missing registries
- Graceful degradation of search and tagging

Offline mode must be tested with and without I18N plugins.

---

## Summary

Testing in ContextHelp is designed to:

- Guarantee reliability in a decentralized ecosystem
- Support deterministic behavior for multi-agent workflows
- Enable safe plugin and registry development
- Validate ingestion durability through the transactional outbox model
- Ensure consistent ranking and retrieval across storage backends
- Protect users from regressions, data loss, and inconsistent context resolution

As the engine grows, additional specialized tests may be added for ingestion jobs, registries, localization, vector search, and complex plugin behaviors.