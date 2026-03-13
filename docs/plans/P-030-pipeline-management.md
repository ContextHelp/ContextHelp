# Pipeline Management — Implementation Plan

> **Status:** Proposed
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dpkms, ctxt
> **Supersedes:** None
> **Superseded by:** None

---

## Context

The current pipeline system has several limitations:
1. **No persistent storage** - Pipelines are only defined in code (`internal/pipeline/registry.go`) and loaded into memory at startup
2. **No custom pipelines** - Users cannot create specialized pipelines without modifying code
3. **Limited step discovery** - Only built-in steps are available; no external step loading
4. **No sandboxing support** - All steps run in the same process with no isolation
5. **Inconsistent enqueue** - `ctxt analyze` enqueues directly to job queue, bypassing pipeline configuration logic
6. **No registry support** - Cannot share or discover steps from external sources

This plan addresses these limitations by adding:
- Persistent pipeline storage in SQLite
- Agent Skills-compatible step discovery (local + registry)
- Configurable sandboxing (process/container isolation)
- External step execution with protocol enforcement
- Registry manifest caching with update notifications
- System reminder support for updates and alerts
- Unified enqueue API through `dpkms pipeline enqueue`

## Decision

**We will implement a persistent pipeline management system with Agent Skills-compatible step discovery, sandbox isolation, and registry integration.**

### Rationale

- **Extensibility**: Persistent storage enables custom pipelines without code changes. Users can install and manage pipelines declaratively.
- **Agent Skills integration**: Leverages existing open standard for step metadata, enabling ecosystem growth.
- **Safety**: Sandbox isolation prevents malicious or broken steps from affecting system stability.
- **User control**: Manual update mode for registries prevents unwanted automatic changes.
- **Unified enqueue**: Single endpoint simplifies client implementations and provides consistent behavior.

### Alternatives considered

- **Custom pipeline language**: Considered DSLs (e.g., custom YAML schema) — rejected as less standard and harder to support
- **Code-only pipelines**: Rejected as prevents extensibility for platform integrators
- **Only built-in steps**: Rejected as limits customization capabilities
- **No sandboxing**: Rejected as safety risk for production systems

---

## Consequences

### Positive

- **Flexible pipeline system** — Custom pipelines for domain-specific needs
- **Ecosystem growth** — Registry marketplace for shared steps
- **Safety isolation** — Container and process-level sandbox options
- **Better UX** — `dpkms pipeline list/show` commands for visibility
- **Single enqueue API** — Consistent behavior across clients
- **Update notifications** — Users know when new steps/versions available

### Negative

- **Complexity** — Multiple subsystems (storage, discovery, execution, HTTP, CLI) need coordination
- **Docker dependency** — Container isolation requires Docker, adding deployment complexity
- **Validation overhead** — Step config schemas and manifest parsing
- **API surface growth** — New endpoints add maintenance burden

### Neutral

- **Configuration complexity** — Sandbox settings per pipeline, auto-update flags per registry
- **Migration path** - Existing in-memory pipelines need migration strategy
- **Memory overhead** - Registry manifests and step metadata stored in database

---

## Implementation Notes

### Task Organization

This plan organizes work into 3 parallel tracks:

**Track A: Storage Layer**
- Database schema for pipelines, steps, registry cache, system reminders
- Storage implementations (SQLite)
- Migration script

**Track B: Step Discovery**
- Agent Skills SKILL.md parser
- Local filesystem discovery
- Registry manifest fetch and caching
- Update notification logic

**Track C: Execution Layer**
- Step executor with subprocess support
- Container isolation (Docker)
- Protocol enforcement (JSON I/O)

**Track D: Service Layer**
- Pipeline CRUD operations
- Step management operations
- Registry cache management
- System reminder operations
- Enqueue endpoint (sole entry point)

**Track E: HTTP Handlers**
- Pipeline endpoints (CRUD + enqueue)
- Step endpoints (list, install, uninstall)
- Registry endpoints (fetch, update, manifest)
- System reminder endpoints

**Track F: CLI Commands**
- `dpkms pipeline` command and subcommands
- Config file parsers (JSON, YAML, TOML)
- Integration with step subcommands
- `ctxt analyze` refactored as API client

**Track G: Documentation**
- User stories (US-0101 through US-0113)
- ADRs (ADR-054 through ADR-058)
- API protocol documentation
- Persona documentation updates

### Key Dependencies

- **New packages:**
  - `github.com/pelletier/go-toml/v2` — TOML parser
  - `github.com/docker/docker` — Docker client for container isolation

- **Existing packages used:**
  - `github.com/go-chi/chi/v5` — HTTP routing
  - `github.com/spf13/cobra` — CLI framework
  - `github.com/spf13/viper` — Config management
  - `gopkg.in/yaml.v3` — YAML parser (existing, used for config)
  - `modernc.org/sqlite` — SQLite driver (existing)

### API Endpoints

```http
POST   /api/v1/pipelines            # Create custom pipeline
GET    /api/v1/pipelines            # List pipelines (with filters)
GET    /api/v1/pipelines/{name}       # Get pipeline details
DELETE /api/v1/pipelines/{name}    # Delete custom pipeline
POST   /api/v1/pipelines/{name}/archive   # Archive pipeline
POST   /api/v1/pipelines/{name}/unarchive # Unarchive pipeline
POST   /api/v1/pipelines/enqueue   # Unified enqueue

GET    /api/v1/steps                   # List available steps
GET    /api/v1/steps/{name}           # Get step details
POST   /api/v1/steps/install           # Install step
DELETE /api/v1/steps/{name}         # Uninstall step

POST   /api/v1/steps/registries/fetch  # Fetch registry manifest
POST   /api/v1/steps/registries/{url}/update # Manual update trigger

GET    /api/v1/system/reminders     # List system reminders
POST   /api/v1/system/reminders/{id}/dismiss # Dismiss reminder
```

---

## Task List

### Track A: Storage Layer

- **A1: Define storage types**
  - Pipeline struct (ID, Name, Description, Steps, IsBuiltIn, Archived, Sandbox)
  - StepRef struct (Name, Config)
  - SandboxConfig struct (Enabled, ResourceLimits, Network, Filesystem, IsolationLevel)
  - ResourceLimitsConfig struct (MaxMemory, MaxCPU, Timeout)
  - FilesystemSandboxConfig struct (ReadOnly, WriteAllowed)
  - RegisteredStep struct (Name, Source, Path, Metadata, InstalledAt, UpdatedAt)
  - StepMetadata struct (Name, Description, License, Version, Author, ConfigSchema, SupportedLangs)
  - RegistryManifest struct (Name, Description, Version, Steps, Supports)
  - ManifestStep struct (Name, Path, License, Version)
  - RegistryCache struct (RegistryURL, Manifest, LastFetched, ETag, AutoUpdate)
  - SystemReminder struct (ID, Type, Title, Message, Source, ActionURL, Dismissed, CreatedAt, UpdatedAt)
  - PipelineFilter struct (Name, IncludeArchived, OnlyArchived)

- **A2: Add storage interfaces**
  - PipelineStore to StorageDriver interface
  - StepStore to StorageDriver interface
  - RegistryStore to StorageDriver interface
  - ReminderStore to StorageDriver interface
  - Create PipelineStore, StepStore, RegistryStore, ReminderStore implementations in SQLite package

- **A3: Create migration script**
  - `internal/storage/sqlite/migrations/002_add_pipelines.sql`
  - Tables: `pipelines`, `steps`, `registry_cache`, `system_reminders`
  - Indexes on archived, source
  - Include SQLite pragmas (journal_mode=WAL, foreign_keys=ON)

- **A4: Implement SQLite stores**
  - `internal/storage/sqlite/pipelines.go` — Create, Get, List, Update, Delete, Archive, Unarchive
  - `internal/storage/sqlite/steps.go` — Create, Get, List, Unregister, Update
- `internal/storage/sqlite/registry_cache.go` — Cache manifest for update detection
- `internal/storage/sqlite/system_reminders.go` — CRUD operations

- **A5: Update existing stores**
  - Add Pipelines(), Steps(), Registries(), Reminders() to StorageDriver interface
- - Update Driver struct to hold store instances

### Track B: Step Discovery

- **B1: Parse Agent Skills format**
  - Extract frontmatter from SKILL.md files
  - Validate required fields (name, description)
  - Extract metadata fields (license, version, author, supported_languages, config_schema)
  - Support optional custom manifest format fallback

- **B2: Implement local filesystem scanner**
  - Scan `~/.config/contexthelp/steps/` directory for SKILL.md directories
  - Parse step metadata from each SKILL.md
  - Register steps in StepStore with source="local:<path>"
  - Handle duplicates (newest wins or user prompt)

- **B3: Implement registry manifest fetching**
  - HTTP GET from registry URL to fetch MANIFEST.yaml or custom manifest
  - Parse manifest (name, description, version, steps array)
  - Cache manifest in RegistryStore with ETag for change detection
  - Compare cached vs fetched to detect updates

- **B4: Implement update notification logic**
  - Periodic check on schedule or manual trigger
  - Create SystemReminder for available updates (version, step count)
  - Only auto-update if registry has `auto_update: true` in config
  - Respect `auto_update` flag per registry

- **B5: Implement step download and installation**
  - Download step package from registry
  - Extract to `~/.config/contexthelp/steps/<step-name>/`
  - Validate SKILL.md or manifest structure
  - Register in StepStore
- - Add entry to SystemReminder if new version available

- **B6: Load steps into pipeline registry**
- **B7: Create StepDiscovery service**
  - Methods: DiscoverAll, LoadBuiltinSteps, ScanLocalSteps, CheckRegistryUpdates, FetchRegistryManifest, DownloadStep, InstallStep, NotifyUpdateAvailable

### Track C: Step Execution Layer

- **C1: Define execution protocol**
  - Input format (JSON with config and object fields)
- - Output format (JSON with object or error)
  - StepResult struct (Object, Error)
  - Standardized error reporting

- **C2: Implement step executor**
  - Map of built-in steps (pipeline.PipelineStep interface)
- - Load registered steps from StepStore
- - Execute built-in steps directly
- `executeExternalStep` for external steps

- **C3: Process-level isolation**
- Use syscall.Rlimit for resource limits (RLIMIT_AS, RLIMIT_CPU)
- Set environment variables for sandboxing
- exec.CommandContext for subprocess execution
  - Parse stdout JSON, return result or error
  - Handle timeouts with context cancellation

- **C4: Container-level isolation**
- Check Docker availability (`docker --version`)
  - Pull step container image from metadata or use default
- Run container with resource limits
  Mount workspace volume
- Apply filesystem restrictions (read-only mounts, no write)
- Disable network if sandbox.Network == false
- Capture logs and parse output
- Auto-remove container on completion

- **C5: Create StepExecutor service**
  - ExecuteStep(ctx, name, config, sandbox, draft) method
  - Select built-in or external execution based on source
  - Apply sandbox configuration based on pipeline's SandboxConfig

- **C6: Add to service layer**
  - Execute method to Pipeline struct (delegates to executor)

### Track D: Service Layer

- **D1: CreatePipeline**
  - Validate pipeline config format (JSON/YAML/TOML)
  - Validate step names against registered steps
  - Parse sandbox configuration
  - Create Pipeline struct
  - Call PipelineStore.Create()
  - Return pipeline ID

- **D2: GetPipeline**
  - Fetch from PipelineStore by name
- - Handle not found (404)

- **D3: ListPipelines**
  - Apply PipelineFilter (name, include_archived, only_archived)
  - Return list from PipelineStore.List()

- **D4: DeletePipeline**
- - Validate not built-in pipeline name (cannot delete `text.*`)
- - Call PipelineStore.Delete()
- - Cascade delete related steps from StepStore (optional)

- **D5: ArchivePipeline**
- - Set Archived=true
- - Prevents pipeline from being used (check in SelectPipeline)

- **D6: UnarchivePipeline**
- - Set Archived=false
- Makes pipeline available again

- **D7: Enqueue (sole entry point)**
  - Accepts AnalyzeRequest (content, type, pipeline, source)
- - Select pipeline (user-specified or SelectPipeline based on content)
  - Create job, enqueue via Queue
- Return job ID
- Replace Analyze in service with this unified Enqueue method

- **D8: Step management methods**
- ListSteps(source) - list from StepStore
- InstallStep(name, fromRegistry) - download and register step
- UninstallStep(name) - remove from StepStore
- FetchRegistry(url) - fetch and cache manifest
- UpdateRegistry(url) - manual update trigger
- ConfigureAutoUpdate(url, enabled) - set auto-update flag

- **D9: Registry cache management**
- UpdateRegistryCache(url, manifest, etag) - cache or invalidate

- **D10: System reminder methods**
- ListReminders(activeOnly) - list non-dismissed reminders
- DismissReminder(id) - mark reminder as dismissed
- CreateReminder(type, title, message, source, actionURL) - create reminder

- **D11: Update Service struct**
- Add Steps (*StepDiscovery), Executor (*StepExecutor) to Service
- Update PipelineRegistry to use step discovery

### Track E: HTTP Handlers

- **E1: Pipeline handlers**
- Create `internal/server/http/handlers_pipelines.go`
  - CreatePipeline(svc) — POST /api/v1/pipelines
  - ListPipelines(svc) — GET /api/v1/pipelines (with query params)
  - GetPipeline(svc) — GET /api/v1/pipelines/{name}
- - DeletePipeline(svc) — DELETE /api/v1/pipelines/{name}
- - ArchivePipeline(svc) — POST /api/v1/pipelines/{name}/archive
- - UnarchivePipeline(svc) — POST /api/v1/pipelines/{name}/unarchive
  - Enqueue(svc) — POST /api/v1/pipelines/enqueue

- **E2: Step handlers**
- Create `internal/server/http/handlers_steps.go`
  - ListSteps(svc) — GET /api/v1/steps
- - GetStep(svc) — GET /api/v1/steps/{name}
- - InstallStep(svc) — POST /api/v1/steps/install
- - UninstallStep(svc) - DELETE /api/v1/steps/{name}

- **E3: Registry handlers**
- FetchRegistry(svc) — POST /api/v1/steps/registries/fetch
- UpdateRegistry(svc) — POST /api/v1/steps/registries/{url}/update

- **E4: System reminder handlers**
- Create `internal/server/http/handlers_system.go`
- ListReminders(svc) — GET /api/v1/system/reminders
- DismissReminder(svc) — POST /api/v1/system/reminders/{id}/dismiss

- **E5: Update router**
- Add pipeline, step, registry, system routes to `internal/server/http/server.go`

### Track F: CLI Commands

- **F1: `dpkms pipeline` root command**
- Use: `pipeline`, Short: "Manage ingestion pipelines"

- **F2: Pipeline subcommands**
- `pipeline create` (alias: `add`) — Create pipeline from config file
- `pipeline remove` (alias: `del`) — Delete custom pipeline
- `pipeline list` (aliases: `ls`, `all`) — List pipelines with flags
- `pipeline show` (alias: `view`) — Show pipeline details
- `pipeline archive` (alias: `disable`) — Archive pipeline
- `pipeline unarchive` (alias: `enable`) — Unarchive pipeline
- `pipeline enqueue` (alias: `push`) — Sole enqueue method

- **F3: Step subcommands**
- `dpkms pipeline step` root command
- Use: `step`, Short: "Manage pipeline steps"
  - `step list` — List available steps
  - `step install <name> [--registry <url>]` — Install step
  - `step uninstall <name>` — Uninstall step
  `step registry` — Manage registries
  - `step registry add <name> <url>` — Add registry
  - `step registry list` — List registries
  - `step registry update <url>` — Manual update
- `step registry autoupdate <url> [--enable|--disable]` — Configure auto-update

- **F4: Config file parsers**
- JSON parser (encoding/json)
- YAML parser (`gopkg.in/yaml.v3`)
- TOML parser (`github.com/pelletier/go-toml/v2`)
- Unified PipelineConfig struct
  - Validation against StepMetadata.config_schema if available

- **F5: Update `ctxt analyze`**
- Remove direct Queue access
- Call `/api/v1/pipelines/enqueue` endpoint
- Read server URL from config (default: http://localhost:8080)
- - Keep same CLI interface (flags, stdin, file support)

### Track G: Documentation

- **G1: User stories (docs/stories/pipelines/)**
  - US-0101-create-custom-pipeline.md
  - US-0102-list-and-filter-pipelines.md
  - US-0103-show-pipeline-details.md
  - US-0104-delete-custom-pipeline.md
  - US-0105-archive-pipeline.md
  - US-0106-enqueue-content-via-dpkms.md
  - US-0107-discover-local-steps.md
  - US-0108-install-step-from-registry.md
  - US-0109-fetch-registry-manifest.md
  - US-0110-check-registry-updates.md
  - US-0111-configure-registry-autoupdate.md
  - US-0112-configure-sandbox-per-pipeline.md
  - US-0113-ctxt-analyze-api-client.md

- **G2: ADRs (docs/decisions/)**
  - ADR-054-agentskills-step-discovery.md
  - ADR-055-sandbox-isolation-levels.md
- - ADR-056-unified-enqueue-api.md
  - ADR-057-registry-update-notification-model.md
- - ADR-058-external-step-execution-protocol.md

- **G3: API protocol (docs/dpkms/)**
  - `ctxt-dpkms-api-protocol.md` — HTTP API contract between ctxt and dpkms

- **G4: Persona documentation**
- Update `docs/dpkms/personas/README.md` — Add Platform Integrator persona section with CLI/HTTP interaction focus
- Update `docs/personas/README.md` — Add reference to pipeline management

### Track H: Testing Strategy

- **H1: Unit tests**
- Storage layer: PipelineStore, StepStore, RegistryStore, ReminderStore
- Step discovery: manifest parsing, registry fetching
- Step executor: external step execution
- Service layer: pipeline and step management methods
- HTTP handlers: request/response validation

- **H2: Integration tests**
- End-to-end: `dpkms pipeline create` → verify persistence
- Step discovery → verify local steps and registry fetching
- External step execution → verify sandbox isolation
- ctxt → dpkms API integration
- Registry updates → verify notification flow

- **H3: Error handling tests**
- Invalid pipeline config → 400 errors
- Protected pipeline deletion → 403 errors
- Missing steps → validation error
- Sandbox validation → config schema errors

- **H4: System reminder tests**
- Create, list, dismiss operations
- Update reminder flows

- **H5: Manual test scenarios**
- Install and use custom pipeline
- Registry update notification flow
- Auto-update disabled when configured

---

## Dependencies

### New Dependencies
```go
// Storage
github.com/pelletier/go-toml/v2
gopkg.in/yaml.v3  // already used
```

### Configuration

### Environment Variables

```bash
# Step discovery path
DPKMS_STEPS_PATH=~/.config/contexthelp/steps/

# Sandbox defaults
DPKMS_DEFAULT_SANDBOX_ISOLATION_LEVEL=process
DPKMS_DEFAULT_SANDBOX_MAX_MEMORY=512MB
DPKMS_SANDBOX_DEFAULT_TIMEOUT=30s
```

### Risk Mitigation

1. **Docker requirement validation** - Check at startup, clear error if not available
2. **Step validation** - Validate step config schemas before execution
3. **Sandbox enforcement** - Verify isolation level matches Docker availability
4. **Resource limits** - Enforce via cgroups/resource limits
5. **Network isolation** - Verify disabled in containers
6. **Input validation** - Sanitize all inputs before passing to subprocess
7. **Output parsing** - Handle malformed output gracefully
8. **Built-in protection** - Cannot delete/archive built-in pipeline names (text.*)

---

## Success Criteria

- [ ] Custom pipelines can be created from config files (JSON/YAML/TOML)
- [ ] Pipelines are stored persistently in SQLite
- [ ] Built-in pipelines are protected from modification/deletion
- [ ] Steps can be discovered from local filesystem
- [ ] Registry manifests can be fetched and cached
- ] ] Steps can be installed from registries
- [ ] System reminders are created for available updates
- [ ] External steps execute with sandbox isolation (process or container)
- [ ] `dpkms pipeline enqueue` is sole enqueue method
- [ ] `ctxt analyze` calls dpkms API for enqueue
- [ ] All CLI commands work correctly
- [ ] Documentation is complete (stories + ADRs + API protocol)
- [ ] All tests pass with coverage target 90%+

## Related Stories

- [US-0028](../admin/US-0028-register-custom-pipeline.md) — Original custom pipeline story (superseded)
- [US-0101](./US-0101-create-custom-pipeline.md) — Create custom pipeline (new story)
- [US-0102](./US-0102-list-and-filter-pipelines.md) — List pipelines (new story)
- [US-0103](./US-0103-show-pipeline-details.md) - Show pipeline details (new story)
- [US-0104](./US-0104-delete-custom-pipeline.md) - Delete pipeline (new story)
- [US-0105](./US-0105-archive-pipeline.md) - Archive pipeline (new story)
- [US-0106](./US-0106-enqueue-content-via-dpkms.md) - Enqueue (new story)
- [US-0107](./US-0107-discover-local-steps.md) - Discover local steps (new story)
- [US-0108](./US-0108-install-step-from-registry.md) - Install from registry (new story)
- [US-0109](./US-0109-fetch-registry-manifest.md) - Fetch manifest (new story)
- [US-0110](./US-0110-check-registry-updates.md) - Check for updates (new story)
- [US-0111](./US-0111-configure-registry-autoupdate.md) - Configure auto-update (new story)
- [US-0112](./US-0112-configure-sandbox-per-pipeline.md) - Configure sandbox (new story)
- [US-0113](./US-0113-ctxt-analyze-api-client.md) - ctxt as API client (new story)

## Related ADRs

- [ADR-004](../../decisions/ADR-004-step-based-pipeline.md) — Step-based pipeline architecture (existing)
- [ADR-053](../../decisions/ADR-053-job-system.md) — Job system (existing)
- [ADR-054](../../decisions/ADR-054-edges-table-for-mentions.md) — Edges for mentions (existing)
- [ADR-051](../../decisions/ADR-051-chi-http-framework.md) — HTTP framework (existing)

---

## Notes

### Implementation Priority

**Critical Path:**
1. Storage layer (Track A) → foundation for all other tracks
2. Service layer (Track D) → depends on storage
3. HTTP handlers (Track E) → depends on service
4. CLI commands (Track F) → depends on service

**Estimated Effort:** 8-12 days

### Dependencies

This plan requires:
- No blocking external dependencies
- Docker for production (optional for container isolation, process-level isolation works without Docker)
