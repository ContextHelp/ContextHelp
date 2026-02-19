# Tool Execution Memory Type Specification

**Status:** Draft  
**Version:** 0.1.0  
**Last Updated:** 2026-02-18  

---

## 1. Overview

### 1.1 Purpose

Define a memory type that captures agent tool execution traces as first-class knowledge objects. This enables:

- **Skill Extraction** - Learn reusable patterns from successful executions
- **Failure Mode Learning** - Capture and propagate error recovery knowledge
- **Cross-Agent Sharing** - Share execution wisdom between agents
- **Proactive Context** - Auto-load relevant patterns before tool invocation

### 1.2 Motivation

Enable agents to learn from their own tool executions:

- Treat tool execution as a **resource** that can be memorized
- Extract **memory items** (skills, patterns, failures) from traces
- Auto-categorize into a **file-system-like hierarchy**
- Enable **proactive retrieval** before similar executions

### 1.3 Scope

| In Scope | Out of Scope |
|----------|--------------|
| Tool invocation capture | Real-time monitoring dashboards |
| Execution trace storage | Billing/metering integration |
| Pattern extraction | Cross-platform tool standardization |
| Skill/failure learning | Automated remediation execution |

---

## 2. Data Model

### 2.1 Type Hierarchy

```
type
└── tool_execution
    ├── tool_execution.llm_call
    ├── tool_execution.code_exec
    ├── tool_execution.api_call
    ├── tool_execution.file_op
    ├── tool_execution.mcp_tool
    └── tool_execution.custom
```

### 2.2 Schema Definition

```yaml
ToolExecution:
  # Core Identification
  id: string (UUID v4)
  createdAt: string (ISO8601)
  lastMatchedAt: string (ISO8601)
  
  # Type Classification
  type: "tool_execution"
  subtype: string (from hierarchy above)
  raw: string (JSON-serialized input)
  source: string (agent-cli | rest-api | mcp-server | plugin:<name>)
  pipeline: "tool_execution.capture"
  
  # Deduplication
  contentHash: string (SHA-256)
  reinforcementCount: integer (default: 1)
  lastReinforcedAt: string (ISO8601 | null)
  
  # Content
  title: string
  summary: string
  
  # Tool-Specific Content
  content: ToolContent
  
  # Semantic Fields
  tags: Tag[]
  hints: string[]
  mentions: Mention[]
  decisions: Decision[]
  questions: Question[]
  
  # Extracted Knowledge
  extracted: ExtractedKnowledge
  
  # Execution Context
  context: ExecutionContext
  
  # Relations
  related: RelatedBookmark[]
  registry: RegistryAlignment
  metadata: object
  plugins: object
  debug: DebugInfo
```

### 2.3 ToolContent Structure

```yaml
ToolContent:
  # Tool Identity
  tool_name: string (required)
  tool_version: string
  tool_namespace: string (e.g., "mcp", "builtin", "plugin")
  
  # Invocation Identity
  invocation_id: string (UUID v4)
  parent_invocation_id: string (UUID v4 | null)
  child_invocation_ids: string[] (UUID v4)
  
  # Session Context
  session_id: string (UUID v4)
  agent_id: string
  conversation_id: string (UUID v4 | null)
  
  # Input
  input:
    parameters: object (tool-specific)
    context: object (ambient context at invocation time)
    intent: string | null (inferred user intent)
  
  # Output
  output:
    result: object (tool-specific)
    status: "success" | "failure" | "timeout" | "partial"
    error:
      code: string
      message: string
      stack_trace: string | null
      recoverable: boolean
  
  # Execution Metrics
  execution:
    duration_ms: integer
    tokens_used: integer | null
    retry_count: integer (default: 0)
    timestamp_start: string (ISO8601)
    timestamp_end: string (ISO8601)
    memory_bytes: integer | null
    rate_limited: boolean
```

### 2.4 ExtractedKnowledge Structure

```yaml
ExtractedKnowledge:
  # Skills extracted from successful patterns
  skills:
    - pattern_id: string (UUID v4)
      pattern: string (description of the pattern)
      preconditions: string[] (when this pattern applies)
      steps: string[] (the successful sequence)
      outcome: string (expected result)
      confidence: float (0.0-1.0)
      examples: string[] (concrete examples)
  
  # Failure modes identified
  failure_modes:
    - failure_id: string (UUID v4)
      symptom: string (what went wrong)
      root_cause: string (why it failed)
      detection: string (how to recognize this failure)
      fix: string (how to recover)
      prevention: string (how to avoid)
      confidence: float (0.0-1.0)
  
  # Optimizations discovered
  optimizations:
    - optimization_id: string (UUID v4)
      before: string (original approach)
      after: string (improved approach)
      improvement: string (quantified benefit)
      conditions: string[] (when to apply)
      confidence: float (0.0-1.0)
  
  # Dependencies discovered
  dependencies:
    - tool_name: string
      relation: "requires" | "enhances" | "conflicts"
      context: string
```

### 2.5 ExecutionContext Structure

```yaml
ExecutionContext:
  # Environment
  environment:
    os: string
    arch: string
    runtime: string
    runtime_version: string
  
  # Project Context
  project:
    root_path: string | null
    language: string | null
    framework: string | null
    git_branch: string | null
    git_commit: string | null
  
  # Agent State
  agent:
    model: string | null
    temperature: float | null
    system_prompt_hash: string | null
    tool_count: integer
  
  # User Context
  user:
    user_id: string | null
    preferences: object
```

### 2.6 DebugInfo Structure

```yaml
DebugInfo:
  # Trace Information
  trace:
    - timestamp: string (ISO8601)
      event: string
      data: object
  
  # Performance
  performance:
    queue_time_ms: integer
    preprocess_time_ms: integer
    postprocess_time_ms: integer
  
  # Raw Data (for debugging)
  raw_input: string (base64 encoded, optional)
  raw_output: string (base64 encoded, optional)
```

---

## 3. Memory Categories

### 3.1 Hierarchical Organization

Using a file-system-as-memory approach:

```
tool_memory/
├── by_tool/                          # Organize by tool type
│   ├── llm_call/
│   │   ├── success/                  # Successful invocations
│   │   ├── failure/                  # Failed invocations
│   │   ├── timeout/                  # Timed out invocations
│   │   └── extracted/                # Extracted patterns
│   │       ├── skills/
│   │       └── failure_modes/
│   ├── code_exec/
│   │   ├── success/
│   │   ├── failure/
│   │   └── extracted/
│   ├── api_call/
│   ├── file_op/
│   │   ├── read/
│   │   ├── write/
│   │   └── search/
│   ├── mcp_tool/
│   └── custom/
│
├── by_agent/                         # Organize by agent
│   ├── @agent.codex/
│   ├── @agent.opencode/
│   └── @agent.custom/
│
├── by_project/                       # Organize by project
│   └── @project.<slug>/
│
├── by_session/                       # Organize by session
│   └── <session_id>/
│
└── extracted/                        # Global extracted knowledge
    ├── skills/                       # Cross-tool skills
    ├── failure_modes/                # Cross-tool failure modes
    ├── optimizations/                # Performance patterns
    └── dependencies/                 # Tool dependency graph
```

### 3.2 Category Schema

```yaml
ToolMemoryCategory:
  id: string (UUID v4)
  path: string (e.g., "tool_memory/by_tool/llm_call/success")
  name: string
  description: string
  
  # Auto-generation rules
  auto_categorize:
    conditions:
      - field: string (jsonpath)
        operator: "eq" | "in" | "regex" | "exists"
        value: any
  
  # Aggregation
  aggregation:
    enabled: boolean
    max_items: integer (before compression)
    compression_strategy: "summarize" | "sample" | "cluster"
  
  # Access patterns
  access:
    query_priority: integer (1-10)
    cache_ttl_seconds: integer
```

---

## 4. Tags and Mentions

### 4.1 Standard Tags

#### Execution Status Tags

| Tag | Description | Auto-applied |
|-----|-------------|--------------|
| `#tool.success` | Execution completed successfully | Yes |
| `#tool.failure` | Execution failed | Yes |
| `#tool.timeout` | Execution timed out | Yes |
| `#tool.partial` | Partial success | Yes |
| `#tool.retry` | Required retry attempt | Yes |
| `#tool.cached` | Result from cache | Yes |

#### Pattern Tags

| Tag | Description | Auto-applied |
|-----|-------------|--------------|
| `#skill.extracted` | Skill pattern identified | Yes (post-extraction) |
| `#skill.reusable` | Highly reusable skill | No (manual/criteria) |
| `#error.pattern` | Recurring error pattern | Yes (post-analysis) |
| `#error.recoverable` | Error was recovered | Yes |
| `#optimization.found` | Performance improvement found | Yes (post-analysis) |

#### Domain Tags

| Tag | Description | Example Usage |
|-----|-------------|---------------|
| `#domain.<slug>` | Domain classification | `#domain.refactoring` |
| `#task.<slug>` | Task classification | `#task.code-generation` |
| `#complexity.<level>` | Execution complexity | `#complexity.high` |

### 4.2 Mention Conventions

Standard mentions for tool executions:

| Mention Pattern | Purpose | Example |
|-----------------|---------|---------|
| `@tool.<name>` | Reference tool | `@tool.read_file` |
| `@agent.<name>` | Reference agent | `@agent.codex` |
| `@project.<slug>` | Reference project | `@project.checkout-redesign` |
| `@session.<id>` | Reference session | `@session.abc123` |
| `@skill.<slug>` | Reference extracted skill | `@skill.file-pattern-match` |
| `@error.<slug>` | Reference error pattern | `@error.rate-limit-exceeded` |

### 4.3 Tag Schema Extension

```yaml
ToolExecutionTag:
  label: string (required)
  namespace: "tool_execution" (required)
  confidence: float (0.0-1.0, required)
  weight: float (0.0-1.0, optional)
  source: "auto" | "manual" | "inferred" (required)
  timestamp: string (ISO8601, required)
```

---

## 5. Pipeline Specification

### 5.1 Pipeline Stages

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        TOOL EXECUTION CAPTURE PIPELINE                       │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────┐   ┌─────────┐   ┌──────────┐   ┌────────────┐   ┌─────────────┐
│ CAPTURE │──►│ NORMALIZE│──►│ EXTRACT  │──►│ CATEGORIZE │──►│ PERSIST     │
└─────────┘   └─────────┘   └──────────┘   └────────────┘   └─────────────┘
     │             │              │              │                 │
     ▼             ▼              ▼              ▼                 ▼
┌─────────┐   ┌─────────┐   ┌──────────┐   ┌────────────┐   ┌─────────────┐
│ Hook    │   │ Schema  │   │ LLM-based│   │ Rule-based │   │ Store with  │
│ into    │   │ validate│   │ pattern  │   │ + ML       │   │ embeddings  │
│ tool    │   │ + hash  │   │ detection│   │ routing    │   │ + relations │
│ runtime │   │         │   │          │   │            │   │             │
└─────────┘   └─────────┘   └──────────┘   └────────────┘   └─────────────┘
```

### 5.2 Stage Details

#### Stage 1: Capture

```yaml
CaptureStage:
  name: "capture"
  
  inputs:
    - tool_invocation: ToolInvocationEvent
  
  outputs:
    - raw_trace: RawToolTrace
  
  processing:
    - Hook into tool execution runtime
    - Capture input parameters
    - Capture execution context
    - Record timestamps
    - Capture output/error
  
  error_handling:
    on_failure: "log and continue"
    timeout_ms: 5000
```

#### Stage 2: Normalize

```yaml
NormalizeStage:
  name: "normalize"
  
  inputs:
    - raw_trace: RawToolTrace
  
  outputs:
    - normalized_trace: ToolContent
    - content_hash: string
  
  processing:
    - Validate against schema
    - Compute content hash
    - Check for duplicates (by hash)
    - Apply field defaults
    - Sanitize sensitive data
  
  error_handling:
    on_validation_error: "reject with reason"
    on_duplicate: "emit reinforcement event"
```

#### Stage 3: Extract

```yaml
ExtractStage:
  name: "extract"
  
  inputs:
    - normalized_trace: ToolContent
    - historical_patterns: ExtractedKnowledge[]
  
  outputs:
    - extracted: ExtractedKnowledge
  
  processing:
    - Analyze execution pattern
    - Compare with historical successes
    - Extract skills (if success)
    - Extract failure modes (if failure)
    - Identify optimizations
    - Map dependencies
  
  llm_integration:
    provider: "configured_llm"
    model: "extraction-model"
    prompt_template: "tool_pattern_extraction"
    max_tokens: 2000
  
  error_handling:
    on_llm_failure: "store without extraction, retry async"
    timeout_ms: 30000
```

#### Stage 4: Categorize

```yaml
CategorizeStage:
  name: "categorize"
  
  inputs:
    - normalized_trace: ToolContent
    - extracted: ExtractedKnowledge
  
  outputs:
    - categories: string[] (category paths)
    - tags: ToolExecutionTag[]
    - mentions: Mention[]
  
  processing:
    - Apply categorization rules
    - Generate auto-tags
    - Infer domain/task tags
    - Extract mentions from content
    - Link to related executions
  
  error_handling:
    on_no_match: "assign to 'uncategorized'"
```

#### Stage 5: Persist

```yaml
PersistStage:
  name: "persist"
  
  inputs:
    - normalized_trace: ToolContent
    - extracted: ExtractedKnowledge
    - categories: string[]
    - tags: ToolExecutionTag[]
    - mentions: Mention[]
  
  outputs:
    - bookmark: Bookmark
    - embedding_id: string
  
  processing:
    - Construct final bookmark
    - Generate summary embedding
    - Store to database
    - Update category indexes
    - Emit events for subscribers
  
  error_handling:
    on_storage_failure: "retry with backoff, max 3 attempts"
```

### 5.3 Pipeline Configuration

```yaml
ToolExecutionPipeline:
  name: "tool_execution.capture"
  version: "1.0.0"
  
  stages:
    - capture
    - normalize
    - extract
    - categorize
    - persist
  
  settings:
    async_extraction: true
    batch_size: 10
    retry_attempts: 3
    timeout_total_ms: 60000
  
  hooks:
    pre_capture: []
    post_persist:
      - "emit_to_subscribers"
      - "update_agent_context"
```

---

## 6. API Specification

### 6.1 Capture API

```yaml
POST /api/v1/tool-execution/capture

Request:
  tool_name: string (required)
  tool_version: string
  invocation_id: string (required, UUID)
  parent_invocation_id: string (UUID)
  session_id: string (required, UUID)
  agent_id: string (required)
  
  input:
    parameters: object (required)
    context: object
  
  output:
    result: object
    status: "success" | "failure" | "timeout" | "partial" (required)
    error:
      code: string
      message: string
      recoverable: boolean
  
  execution:
    duration_ms: integer (required)
    tokens_used: integer
    retry_count: integer
    timestamp_start: string (ISO8601, required)
    timestamp_end: string (ISO8601, required)

Response:
  id: string (bookmark UUID)
  status: "created" | "reinforced"
  categories: string[]
  extracted_skills: integer
  extracted_failure_modes: integer

Errors:
  400: Invalid request body
  409: Duplicate invocation (idempotent, returns existing)
  500: Internal processing error
```

### 6.2 Query API

```yaml
GET /api/v1/tool-execution

Query Parameters:
  tool_name: string (filter by tool)
  agent_id: string (filter by agent)
  session_id: string (filter by session)
  status: string (filter by status)
  tag: string[] (filter by tags)
  mention: string[] (filter by mentions)
  after: string (ISO8601, filter by timestamp)
  before: string (ISO8601, filter by timestamp)
  limit: integer (default: 50, max: 200)
  offset: integer (default: 0)
  include_extracted: boolean (default: false)

Response:
  items: ToolExecution[]
  total: integer
  has_more: boolean
```

### 6.3 Skill Retrieval API

```yaml
POST /api/v1/tool-execution/skills/retrieve

Request:
  tool_name: string (optional, narrow to tool)
  context:
    task: string (description of current task)
    intent: string (inferred user intent)
    constraints: string[] (any constraints)
  limit: integer (default: 10)

Response:
  skills:
    - skill_id: string
      pattern: string
      preconditions: string[]
      steps: string[]
      outcome: string
      confidence: float
      source_executions: string[] (bookmark IDs)
      relevance_score: float
```

### 6.4 Failure Mode Retrieval API

```yaml
POST /api/v1/tool-execution/failures/retrieve

Request:
  tool_name: string (optional)
  error_context:
    symptom: string (what's happening)
    error_code: string (if known)
    attempted_fixes: string[]
  limit: integer (default: 10)

Response:
  failure_modes:
    - failure_id: string
      symptom: string
      root_cause: string
      detection: string
      fix: string
      prevention: string
      confidence: float
      match_score: float
```

---

## 7. CLI Specification

### 7.1 Capture Commands

```bash
# Capture a tool execution (usually called by agents, not users)
ctxt tool capture --tool read_file --input '{"path": "src/main.go"}' --output '{"content": "..."}' --status success --duration 150

# Capture with context
ctxt tool capture --tool llm_call --agent @agent.codex --session $SESSION_ID --input '{"prompt": "..."}' --output '{"response": "..."}' --status success --tokens 500
```

### 7.2 Query Commands

```bash
# List recent tool executions
ctxt tool list --after 2026-02-01 --limit 50

# Filter by tool and status
ctxt tool list --tool read_file --status failure

# Filter by tags
ctxt tool list --tag skill.extracted --tag tool.success

# Filter by mention
ctxt tool list --mention @agent.codex

# Get execution details
ctxt tool get <invocation-id>
```

### 7.3 Skill Commands

```bash
# List extracted skills
ctxt skill list --tool llm_call

# Search skills by context
ctxt skill search --context "refactoring Go code"

# Get skill details
ctxt skill get <skill-id>

# Export skills for sharing
ctxt skill export --agent @agent.codex --output skills.json
```

### 7.4 Failure Mode Commands

```bash
# List failure modes
ctxt failure list --tool code_exec

# Search failures by symptom
ctxt failure search --symptom "timeout"

# Get failure details
ctxt failure get <failure-id>
```

---

## 8. Integration Points

### 8.1 MCP Tool Integration

```yaml
MCPToolHook:
  trigger: "before_tool_call" | "after_tool_call"
  
  before_tool_call:
    actions:
      - Load relevant skills
      - Load relevant failure modes
      - Inject context into tool call
  
  after_tool_call:
    actions:
      - Capture execution trace
      - Run extraction pipeline
      - Update agent context
  
  configuration:
    enabled_tools: string[] | "*" (all)
    excluded_tools: string[]
    async_capture: boolean
```

### 8.2 Agent SDK Integration

```go
// Go SDK example
type ToolExecutionRecorder interface {
    // BeforeInvocation records pre-execution state and loads context
    BeforeInvocation(ctx context.Context, inv *ToolInvocation) (*ExecutionContext, error)
    
    // AfterInvocation records post-execution state
    AfterInvocation(ctx context.Context, inv *ToolInvocation, result *ToolResult) error
    
    // RetrieveSkills loads relevant skills for a task
    RetrieveSkills(ctx context.Context, toolName string, task string) ([]Skill, error)
    
    // RetrieveFailureModes loads relevant failure modes
    RetrieveFailureModes(ctx context.Context, toolName string, symptom string) ([]FailureMode, error)
}
```

### 8.3 Event Emission

```yaml
Events:
  tool_execution_captured:
    payload:
      invocation_id: string
      tool_name: string
      status: string
      categories: string[]
    subscribers:
      - "agent-context-updater"
      - "skill-notifier"
  
  skill_extracted:
    payload:
      skill_id: string
      tool_name: string
      pattern: string
      source_execution: string
    subscribers:
      - "skill-indexer"
      - "notification-service"
  
  failure_mode_detected:
    payload:
      failure_id: string
      tool_name: string
      symptom: string
    subscribers:
      - "alert-service"
      - "knowledge-base-updater"
```

---

## 9. Storage Schema

### 9.1 Database Tables

```sql
-- Tool executions (extends bookmarks)
CREATE TABLE tool_executions (
    id UUID PRIMARY KEY,
    bookmark_id UUID REFERENCES bookmarks(id),
    
    -- Tool identity
    tool_name VARCHAR(255) NOT NULL,
    tool_version VARCHAR(64),
    tool_namespace VARCHAR(64),
    
    -- Invocation
    invocation_id UUID NOT NULL UNIQUE,
    parent_invocation_id UUID,
    session_id UUID NOT NULL,
    agent_id VARCHAR(255) NOT NULL,
    conversation_id UUID,
    
    -- Status
    status VARCHAR(32) NOT NULL,
    
    -- Metrics
    duration_ms INTEGER,
    tokens_used INTEGER,
    retry_count INTEGER DEFAULT 0,
    
    -- Timestamps
    timestamp_start TIMESTAMPTZ NOT NULL,
    timestamp_end TIMESTAMPTZ NOT NULL,
    
    -- Content (JSONB for flexibility)
    input JSONB,
    output JSONB,
    error JSONB,
    context JSONB,
    
    -- Extracted knowledge
    extracted_skills JSONB,
    extracted_failure_modes JSONB,
    extracted_optimizations JSONB,
    
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_tool_executions_tool_name ON tool_executions(tool_name);
CREATE INDEX idx_tool_executions_agent_id ON tool_executions(agent_id);
CREATE INDEX idx_tool_executions_session_id ON tool_executions(session_id);
CREATE INDEX idx_tool_executions_status ON tool_executions(status);
CREATE INDEX idx_tool_executions_timestamp ON tool_executions(timestamp_start);
CREATE INDEX idx_tool_executions_parent ON tool_executions(parent_invocation_id);

-- Skills extracted from executions
CREATE TABLE extracted_skills (
    id UUID PRIMARY KEY,
    skill_id UUID NOT NULL UNIQUE,
    
    -- Classification
    tool_name VARCHAR(255),
    domain VARCHAR(255),
    
    -- Content
    pattern TEXT NOT NULL,
    preconditions TEXT[],
    steps TEXT[],
    outcome TEXT,
    
    -- Metadata
    confidence FLOAT,
    usage_count INTEGER DEFAULT 0,
    last_used_at TIMESTAMPTZ,
    
    -- Source
    source_execution_id UUID REFERENCES tool_executions(id),
    source_bookmark_id UUID REFERENCES bookmarks(id),
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Failure modes extracted from executions
CREATE TABLE extracted_failure_modes (
    id UUID PRIMARY KEY,
    failure_id UUID NOT NULL UNIQUE,
    
    -- Classification
    tool_name VARCHAR(255),
    error_code VARCHAR(255),
    
    -- Content
    symptom TEXT NOT NULL,
    root_cause TEXT,
    detection TEXT,
    fix TEXT,
    prevention TEXT,
    
    -- Metadata
    confidence FLOAT,
    occurrence_count INTEGER DEFAULT 1,
    last_occurred_at TIMESTAMPTZ,
    
    -- Source
    source_execution_id UUID REFERENCES tool_executions(id),
    source_bookmark_id UUID REFERENCES bookmarks(id),
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Tool execution hierarchy (parent-child relationships)
CREATE TABLE tool_execution_hierarchy (
    parent_id UUID REFERENCES tool_executions(id),
    child_id UUID REFERENCES tool_executions(id),
    depth INTEGER DEFAULT 1,
    PRIMARY KEY (parent_id, child_id)
);
```

### 9.2 Vector Embeddings

```sql
-- Embeddings for semantic search
CREATE TABLE tool_execution_embeddings (
    id UUID PRIMARY KEY,
    execution_id UUID REFERENCES tool_executions(id),
    
    -- Embedding type
    embedding_type VARCHAR(32), -- 'summary', 'input', 'output', 'skill'
    
    -- Vector
    embedding vector(1536), -- OpenAI ada-002 dimensions, adjust as needed
    
    -- Metadata
    model VARCHAR(255),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_tool_emb_vector ON tool_execution_embeddings 
    USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);
```

---

## 10. Implementation Phases

### Phase 1: Core Infrastructure (Week 1-2)

- [ ] Define Go structs for schema
- [ ] Create database migrations
- [ ] Implement storage layer
- [ ] Basic capture endpoint

### Phase 2: Pipeline Implementation (Week 3-4)

- [ ] Implement 5-stage pipeline
- [ ] Add LLM-based extraction
- [ ] Implement categorization rules
- [ ] Add embedding generation

### Phase 3: CLI & API (Week 5-6)

- [ ] CLI commands for capture/query
- [ ] REST API endpoints
- [ ] Skill/failure retrieval APIs
- [ ] Query filtering and pagination

### Phase 4: Integration (Week 7-8)

- [ ] MCP tool hooks
- [ ] Agent SDK integration
- [ ] Event emission
- [ ] Cross-referencing

### Phase 5: Optimization (Week 9-10)

- [ ] Query performance tuning
- [ ] Caching strategies
- [ ] Batch processing
- [ ] Compression for old traces

---

## 11. Success Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Capture latency | < 100ms | P95 of capture endpoint |
| Extraction accuracy | > 85% | Manual validation sample |
| Skill reuse rate | > 30% | Skills used in subsequent executions |
| Failure recovery rate | > 50% | Failures where recovery was found |
| Query latency | < 200ms | P95 of query endpoint |
| Storage efficiency | < 1KB/execution (compressed) | Average size after compression |

---

## 12. Future Considerations

### 12.1 Potential Extensions

- **Real-time Dashboard** - Live execution monitoring
- **Automated Remediation** - Auto-apply known fixes
- **Cross-Agent Learning** - Federated skill sharing
- **Compression Policies** - Automatic summarization of old traces
- **Privacy Controls** - Sensitive data handling

### 12.2 Known Limitations

- Large outputs may need truncation
- LLM extraction has cost implications
- Real-time requirements may need async processing
- Cross-session skill transfer needs validation

---

## Appendix A: Example Executions

### A.1 Successful LLM Call

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "type": "tool_execution",
  "subtype": "tool_execution.llm_call",
  "createdAt": "2026-02-18T10:30:00Z",
  
  "content": {
    "tool_name": "llm_call",
    "tool_namespace": "builtin",
    "invocation_id": "660e8400-e29b-41d4-a716-446655440001",
    "session_id": "770e8400-e29b-41d4-a716-446655440002",
    "agent_id": "opencode",
    
    "input": {
      "parameters": {
        "model": "claude-3-5-sonnet",
        "prompt": "Explain the difference between sync and async in Go"
      },
      "intent": "code explanation"
    },
    
    "output": {
      "result": {
        "response": "In Go, synchronous (sync) operations block..."
      },
      "status": "success"
    },
    
    "execution": {
      "duration_ms": 1234,
      "tokens_used": 450,
      "timestamp_start": "2026-02-18T10:30:00Z",
      "timestamp_end": "2026-02-18T10:30:01.234Z"
    }
  },
  
  "tags": [
    { "label": "tool.success", "namespace": "tool_execution", "confidence": 1.0, "source": "auto" },
    { "label": "domain.code-explanation", "namespace": "tool_execution", "confidence": 0.9, "source": "inferred" }
  ],
  
  "mentions": [
    { "text": "Go", "mention_type": "Concept", "namespace": "concept", "slug": "go-language", "confidence": 0.95 }
  ],
  
  "extracted": {
    "skills": [
      {
        "pattern_id": "880e8400-e29b-41d4-a716-446655440003",
        "pattern": "Explain programming concepts with examples",
        "preconditions": ["User asks for explanation", "Topic is programming-related"],
        "steps": ["Parse the concept", "Provide definition", "Give code example"],
        "outcome": "Clear explanation with runnable code",
        "confidence": 0.85
      }
    ]
  }
}
```

### A.2 Failed File Operation

```json
{
  "id": "990e8400-e29b-41d4-a716-446655440004",
  "type": "tool_execution",
  "subtype": "tool_execution.file_op",
  "createdAt": "2026-02-18T11:00:00Z",
  
  "content": {
    "tool_name": "write_file",
    "tool_namespace": "builtin",
    "invocation_id": "aa0e8400-e29b-41d4-a716-446655440005",
    "session_id": "770e8400-e29b-41d4-a716-446655440002",
    "agent_id": "opencode",
    
    "input": {
      "parameters": {
        "path": "/etc/hosts",
        "content": "127.0.0.1 localhost"
      },
      "intent": "write system file"
    },
    
    "output": {
      "result": null,
      "status": "failure",
      "error": {
        "code": "EACCES",
        "message": "permission denied",
        "stack_trace": null,
        "recoverable": true
      }
    },
    
    "execution": {
      "duration_ms": 5,
      "timestamp_start": "2026-02-18T11:00:00Z",
      "timestamp_end": "2026-02-18T11:00:00.005Z"
    }
  },
  
  "tags": [
    { "label": "tool.failure", "namespace": "tool_execution", "confidence": 1.0, "source": "auto" },
    { "label": "error.recoverable", "namespace": "tool_execution", "confidence": 1.0, "source": "auto" }
  ],
  
  "extracted": {
    "failure_modes": [
      {
        "failure_id": "bb0e8400-e29b-41d4-a716-446655440006",
        "symptom": "Permission denied when writing to system directories",
        "root_cause": "Insufficient privileges for system file modification",
        "detection": "Error code EACCES on write operations",
        "fix": "Use sudo, write to user directory, or request elevated permissions",
        "prevention": "Check file permissions before attempting write",
        "confidence": 0.95
      }
    ]
  }
}
```

---

## Appendix B: Configuration Reference

```yaml
tool_execution:
  enabled: true
  
  capture:
    async: true
    timeout_ms: 5000
    sensitive_fields:
      - "input.parameters.api_key"
      - "input.parameters.password"
      - "output.result.token"
  
  extraction:
    enabled: true
    provider: "openai"
    model: "gpt-4o-mini"
    max_tokens: 2000
    timeout_ms: 30000
  
  categorization:
    rules:
      - condition: "content.status == 'success'"
        category: "tool_memory/by_tool/{tool_name}/success"
      - condition: "content.status == 'failure'"
        category: "tool_memory/by_tool/{tool_name}/failure"
      - condition: "extracted.skills.length > 0"
        category: "tool_memory/extracted/skills"
  
  storage:
    retention_days: 365
    compression_after_days: 30
    max_trace_size_kb: 1024
  
  retrieval:
    default_limit: 50
    max_limit: 200
    embedding_model: "text-embedding-3-small"
```

---

**End of Specification**
