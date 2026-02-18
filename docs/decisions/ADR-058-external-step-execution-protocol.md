# ADR-058 – External Step Execution Protocol

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Pipeline steps are executable code that process knowledge objects. Steps may be:

**Local steps** (built into dPKMS or installed via registry):
- Go code compiled into dPKMS binary
- Direct function calls with `*KnowledgeObject` in/out
- Execute in dPKMS process or container sandbox

**External steps** (future capability):
- Executables or scripts on local filesystem (Go, Python, shell, etc.)
- Standalone processes spawned by dPKMS
- Communicate via stdin/stdout or files
- Execute in sandbox for isolation

**Why external steps?**
- Plugin authors can write in any language (Python, Node.js, Rust, etc.)
- Steps can be developed and tested independently of dPKMS
- Steps can be versioned and updated without recompiling dPKMS
- Registry can host compiled binaries or scripts
- Lower barrier to entry for plugin developers

**Problem 1: No protocol defined for external step communication**
- How does dPKMS send input to external step?
- How does external step return output to dPKMS?
- What format should input/output be in?
- How are errors reported?

**Problem 2: Sandbox integration for external steps**
- Process isolation: how to enforce resource limits?
- Container isolation: how to mount inputs and collect outputs?
- How to pass sandbox configuration to external step?

**Problem 3: Step metadata for external steps**
- How to declare supported languages?
- How to declare required resources (memory, CPU)?
- How to declare input/output schema?
- How to declare minimum isolation level?

**The question:** What protocol should external pipeline steps follow to communicate with dPKMS, and how should sandboxing be integrated?

---

## Decision

**External steps communicate via stdin/stdout with JSON protocol. Input is `KnowledgeObject` (or partial subset), output is `StepResult` with updated `KnowledgeObject` or error. Sandbox configuration is passed via environment variables.**

### Communication Protocol

#### Input Format (stdin)

dPKMS sends JSON-serialized knowledge object to step's stdin:

```json
{
  "id": "ko-abc123",
  "type": "text",
  "subtype": "article",
  "raw_content": "The quick brown fox...",
  "content_type": "text/plain",
  "metadata": {"source": "https://example.com/article"},
  "summaries": ["A fox and a dog..."],
  "sections": [],
  "tags": [],
  "mentions": [],
  "pipeline": "text.short",
  "source": "cli",
  "plugins": {}
}
```

**Notes:**
- All fields from `KnowledgeObject` are included
- External step may read any field it needs
- External step should not modify `id`, `created_at`, `updated_at`
- External step should only modify fields it's responsible for (follow ADR-004 step separation principle)

#### Output Format (stdout)

External step writes JSON-serialized `StepResult` to stdout:

**Success:**
```json
{
  "object": {
    "type": "text",
    "subtype": "article",
    "raw_content": "The quick brown fox...",
    "summaries": ["A fox and a dog..."],
    "tags": ["animal", "dog", "fox"]
  },
  "error": null
}
```

**Error:**
```json
{
  "object": null,
  "error": {
    "code": "STEP_FAILED",
    "message": "Failed to process content: malformed input",
    "details": {"line": 42, "column": 10}
  }
}
```

**Schema:**
```go
type StepResult struct {
    Object *KnowledgeObject `json:"object,omitempty"`
    Error  *StepError      `json:"error,omitempty"`
}

type StepError struct {
    Code    string                 `json:"code"`
    Message string                 `json:"message"`
    Details map[string]any        `json:"details,omitempty"`
}
```

### Environment Variables

dPKMS passes sandbox configuration and execution context via environment variables:

```bash
# Sandbox configuration
SANDBOX_ENABLED=true
SANDBOX_ISOLATION_LEVEL=process
SANDBOX_MAX_MEMORY=512MB
SANDBOX_MAX_CPU=50%
SANDBOX_TIMEOUT=30s
SANDBOX_NETWORK=false

# Execution context
DPKMS_STEP_NAME=tagger
DPKMS_PIPELINE_NAME=custom-pipeline
DPKMS_STEP_CONFIG_PATH=/tmp/step-config.json
```

### Step Config File

Optional per-step configuration (passed via path in `DPKMS_STEP_CONFIG_PATH` env var):

```json
{
  "model": "legal-v2",
  "min_confidence": 0.85,
  "language": "en"
}
```

### Step Metadata (SKILL.md)

External steps must provide `SKILL.md` with frontmatter declaring execution requirements:

```markdown
---
name: legal-classifier
description: Classifies legal documents by document type
version: 1.2.0
author: Example Legal AI <legal@example.com>
license: MIT
supported_languages: ["python", "shell"]
execution:
  type: external
  executable: scripts/classify.py
  config_schema:
    type: object
    properties:
      model:
        type: string
        description: Model version to use
      min_confidence:
        type: number
        description: Minimum confidence threshold
  minimum_isolation: process
  resource_requirements:
    max_memory: 512MB
    max_cpu: 50%
    timeout: 30s
---
```

**Key fields:**
- `execution.type: external` – indicates external step
- `execution.executable` – path to script or binary (relative to step directory)
- `execution.config_schema` – JSON Schema for step config validation
- `execution.minimum_isolation` – required isolation level (`process` or `container`)
- `resource_requirements` – default resource limits (can be overridden by pipeline sandbox config)

### Execution Flow

**Process isolation (default):**

```
dPKMS (Go process)
    ↓
1. Read step metadata (SKILL.md)
2. Validate minimum_isolation <= pipeline isolation level
3. Merge resource_requirements with pipeline sandbox config
4. Spawn subprocess: exec.Command(step.Executable)
5. Set environment variables (SANDBOX_*, DPKMS_*)
6. Write JSON input to subprocess stdin
7. Apply resource limits (RLIMIT_*)
8. Read JSON output from subprocess stdout
9. Parse StepResult
10. Return object or error to pipeline
```

**Container isolation (Docker):**

```
dPKMS (Go process)
    ↓
1. Read step metadata (SKILL.md)
2. Validate minimum_isolation <= pipeline isolation level
3. Check Docker availability
4. Create container with:
   - Image: step metadata or default (e.g., alpine:3)
   - Cmd: step.Executable
   - Env: SANDBOX_*, DPKMS_*
   - Mounts: read-only bind mounts for step files, read-only for inputs, writeable for output
5. Start container
6. Write JSON input to container stdin (or mount as file)
7. Wait for container completion
8. Read JSON output from container stdout (or mounted output file)
9. Parse StepResult
10. Return object or error to pipeline
```

### Example: External Step in Python

**File: `steps/legal-classifier/scripts/classify.py`**

```python
#!/usr/bin/env python3

import json
import sys
import os

def main():
    # 1. Read input from stdin
    input_json = sys.stdin.read()
    input_obj = json.loads(input_json)

    # 2. Read step config (optional)
    config_path = os.environ.get('DPKMS_STEP_CONFIG_PATH')
    config = {}
    if config_path and os.path.exists(config_path):
        with open(config_path, 'r') as f:
            config = json.load(f)

    model = config.get('model', 'default-model')
    min_confidence = config.get('min_confidence', 0.8)

    # 3. Process content
    content = input_obj.get('raw_content', '')
    document_type = classify(content, model, min_confidence)

    # 4. Update knowledge object
    output_obj = input_obj.copy()
    output_obj['subtype'] = document_type

    # 5. Write output to stdout
    result = {
        'object': output_obj,
        'error': None
    }
    sys.stdout.write(json.dumps(result))

def classify(content, model, min_confidence):
    # Mock classification logic
    return 'contract' if 'contract' in content.lower() else 'other'

if __name__ == '__main__':
    main()
```

**SKILL.md:**
```markdown
---
name: legal-classifier
description: Classifies legal documents by document type
version: 1.2.0
supported_languages: ["python"]
execution:
  type: external
  executable: scripts/classify.py
  config_schema:
    type: object
    properties:
      model:
        type: string
        default: default-model
      min_confidence:
        type: number
        default: 0.8
        minimum: 0.0
        maximum: 1.0
  minimum_isolation: process
  resource_requirements:
    max_memory: 256MB
    max_cpu: 25%
    timeout: 30s
---
```

### Example: Containerized External Step

**Dockerfile:**
```dockerfile
FROM python:3.11-slim

WORKDIR /app
COPY scripts/classify.py .
RUN chmod +x classify.py

CMD ["python", "classify.py"]
```

**SKILL.md:**
```markdown
---
name: python-ner-extractor
description: Named entity recognition using Python spaCy
version: 2.0.0
supported_languages: ["python"]
execution:
  type: external
  executable: classify.py
  container_image: python-spacy:2.0.0
  minimum_isolation: container
  resource_requirements:
    max_memory: 1GB
    max_cpu: 100%
    timeout: 60s
---
```

---

## Rationale

### Alternatives Considered

#### 1. gRPC Protocol (Rejected)

Reject because:
- Requires step to implement gRPC server (complex for simple scripts)
- Requires network port (even for local execution)
- Overkill for single-request/response pattern
- Not accessible to shell scripts without gRPC library

#### 2. HTTP Protocol (Rejected)

Reject because:
- Requires step to implement HTTP server
- Requires network port (even for local execution)
- Overhead of HTTP for local IPC
- Not accessible to shell scripts without HTTP library

#### 3. File-Based Communication (stdin/out, but with temp files) (Rejected)

Reject because:
- More complex than direct stdin/stdout
- Requires cleanup of temp files
- Slower due to file I/O
- No clear advantage over stdin/stdout

#### 4. Custom Binary Protocol (Rejected)

Reject because:
- Requires parsing logic in dPKMS for each protocol version
- Not human-readable (difficult to debug)
- Language-specific (not cross-language compatible)
- JSON is more flexible and widely supported

### Benefits of Chosen Approach

- **Simple:** stdin/stdout is universal (all languages support it)
- **Flexible:** JSON is schema-optional, allows future fields
- **Debuggable:** Can pipe JSON to/from step manually for testing
- **Language-agnostic:** Works with any language that can read stdin and write stdout
- **Stateless:** Each invocation is independent (no persistent state)
- **Sandbox-compatible:** stdin/stdout works in both process and container isolation
- **Resource-limits compatible:** Environment variables pass sandbox config clearly

---

## Consequences

### Positive

- Plugin authors can write steps in any language
- Steps can be developed and tested independently
- Protocol is simple (stdin/stdout + JSON)
- Debugging is easy (pipe JSON manually)
- Sandbox integration via environment variables
- Step metadata declares requirements clearly

### Negative

- JSON serialization/deserialization overhead (negligible for typical object sizes)
- No streaming (entire object in memory)
- Step must read entire input before processing (not streaming)
- External step must be executable on host OS (or container must provide compatible OS)

### Neutral

- Environment variables are simple but not structured (could use JSON file instead)
- Step config is optional (some steps may not need config)
- Container image field in metadata is optional (defaults to base image)

---

## Implementation Notes

### Timeout Handling

If step exceeds timeout (from `SANDBOX_TIMEOUT` env var):
- dPKMS kills subprocess/container
- Returns error: "Step timeout exceeded: 30s"
- Step responsible for cleanup (killed, no graceful shutdown)

### Error Handling

If step writes invalid JSON to stdout:
- dPKMS returns error: "Step output is not valid JSON"
- Includes stdout/stderr in error details for debugging

If step exits with non-zero status:
- dPKMS reads stdout (may contain partial JSON)
- Returns error: "Step exited with status 1"

### Security Considerations

- External steps run in sandbox (process or container isolation)
- Steps cannot write directly to storage (only dPKMS ingestion manager commits)
- Steps have read-only filesystem access to step files
- Steps cannot access dPKMS internal state

### Validation

dPKMS validates before executing external step:
1. `execution.minimum_isolation` ≤ pipeline `sandbox.isolation_level`
2. `executable` file exists and is executable
3. `config_schema` (if provided) is valid JSON Schema
4. Step metadata is parseable (valid YAML frontmatter)

---

## References

- **ADR-004** – Step-Based Pipeline Architecture (defines pipeline step interface)
- **ADR-055** – Sandbox Isolation Levels (defines process and container isolation)
- **ADR-027** – Plugin Isolation and Sandboxing (general plugin security requirements)
- **US-0107** – Discover Local Steps (user story for step discovery)
- **US-0108** – Install Step from Registry (user story for step installation)

---
