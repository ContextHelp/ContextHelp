# ADR-055 – Sandbox Isolation Levels

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Pipeline steps are executable code (Go, Python, or shell scripts) that process knowledge objects. These steps may be:
- Written by third-party plugin developers
- Downloaded from untrusted registries
- Execute with access to system resources (filesystem, network, memory, CPU)

Security and stability risks include:
- **Malicious steps** accessing sensitive knowledge or exfiltrating data
- **Buggy steps** causing infinite loops, memory leaks, or crashes
- **Untrusted steps** interfering with core system functionality
- **Resource exhaustion** from runaway processes

ADR-027 defines plugin isolation requirements but does not specify isolation levels for pipeline step execution. Pipeline steps are a subset of plugin functionality and have specific execution needs:
- Steps are short-lived (seconds to minutes, not persistent services)
- Steps operate on bounded input (a single knowledge object)
- Steps must not write directly to storage (only ingestion manager commits)
- Steps may require filesystem access to referenced resources (e.g., local assets)

**The question:** What isolation levels should be available for pipeline step execution, and how should they be enforced?

---

## Decision

**Two isolation levels are supported: process isolation (default) and container isolation.**

### Isolation Levels

#### Level 1: Process Isolation (Default)

Uses OS-level process isolation with `syscall.Rlimit` resource limits.

**Characteristics:**
- Runs in same process tree as dPKMS
- Resource limits enforced via `RLIMIT_*` syscalls
- No filesystem sandboxing (access to same filesystem as dPKMS)
- Network access can be disabled per-pipeline
- Minimal overhead (~0ms startup)

**Use cases:**
- Trusted built-in steps
- Steps from trusted registries
- Performance-critical steps
- Steps requiring filesystem access to dPKMS assets

**Configuration:**
```yaml
sandbox:
  enabled: true
  isolation_level: "process"
  resource_limits:
    max_memory: "512MB"
    max_cpu: "50%"
    timeout: "30s"
  network: false
```

**Enforcement (Go):**
```go
import (
    "syscall"
    "os/exec"
)

type ProcessSandbox struct {
    limits *ResourceLimitsConfig
}

func (ps *ProcessSandbox) Execute(ctx context.Context, step *Step, input *KnowledgeObject) (*KnowledgeObject, error) {
    cmd := exec.CommandContext(ctx, step.ExecutablePath, step.Args...)

    // Apply resource limits
    cmd.SysProcAttr = &syscall.SysProcAttr{
        Setpgid: true,
        Pdeathsig: syscall.SIGTERM,
        RLimits: []syscall.Rlimit{
            {Type: syscall.RLIMIT_AS, Cur: ps.limits.MaxMemoryBytes()},
            {Type: syscall.RLIMIT_CPU, Cur: ps.limits.MaxCPUPercent()},
        },
    }

    // Restrict network if disabled
    if !ps.networkAllowed {
        cmd.Env = append(os.Environ(), "DISABLE_NETWORK=1")
    }

    output, err := cmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("step failed: %w", err)
    }

    var result KnowledgeObject
    if err := json.Unmarshal(output, &result); err != nil {
        return nil, fmt.Errorf("parse step output: %w", err)
    }

    return &result, nil
}
```

#### Level 2: Container Isolation (Optional)

Uses Docker containers for strong isolation. Requires Docker daemon.

**Characteristics:**
- Runs in isolated container namespace
- Full filesystem sandboxing (read-only rootfs, selected bind mounts)
- Network access controlled per-container
- Resource limits via Docker cgroups
- Higher overhead (~100-500ms startup)

**Use cases:**
- Untrusted steps from public registries
- Steps requiring specific OS environments
- Steps with conflicting dependencies
- Steps requiring complete filesystem isolation

**Configuration:**
```yaml
sandbox:
  enabled: true
  isolation_level: "container"
  resource_limits:
    max_memory: "1GB"
    max_cpu: "100%"
    timeout: "60s"
  network: false
  filesystem:
    read_only: ["assets/", "references/"]
    write_allowed: false
```

**Enforcement (Go with Docker SDK):**
```go
import (
    "github.com/docker/docker/client"
    "github.com/docker/docker/api/types"
    "github.com/docker/docker/api/types/container"
    "github.com/docker/docker/api/types/mount"
)

type ContainerSandbox struct {
    docker *docker.Client
    limits *ResourceLimitsConfig
}

func (cs *ContainerSandbox) Execute(ctx context.Context, step *Step, input *KnowledgeObject) (*KnowledgeObject, error) {
    // Parse resource limits
    memory, _ := parseMemory(cs.limits.MaxMemory)
    cpuQuota, cpuPeriod := parseCPU(cs.limits.MaxCPU)

    containerConfig := &container.Config{
        Image:        step.ContainerImage, // e.g., "alpine:3" or custom
        Cmd:          []string{step.ExecutablePath},
        Env:          buildEnv(step, input),
        ReadonlyRootfs: true,
        NetworkDisabled: !cs.networkAllowed,
    }

    hostConfig := &container.HostConfig{
        Memory:     memory,
        NanoCPUs:   cpuQuota,
        CpuPeriod:   cpuPeriod,
        Mounts:      buildReadOnlyBinds(cs.filesystem.ReadOnly),
        AutoRemove:  true,
    }

    resp, err := cs.docker.ContainerCreate(ctx, step.Name, containerConfig, hostConfig, nil, nil)
    if err != nil {
        return nil, fmt.Errorf("create container: %w", err)
    }
    defer cs.docker.ContainerRemove(ctx, resp.ID, types.ContainerRemoveOptions{})

    if err := cs.docker.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
        return nil, fmt.Errorf("start container: %w", err)
    }

    statusCh, errCh := cs.docker.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
    select {
    case status := <-statusCh:
        if status.StatusCode != 0 {
            return nil, fmt.Errorf("container exited with status %d", status.StatusCode)
        }
    case err := <-errCh:
        return nil, fmt.Errorf("container wait error: %w", err)
    case <-ctx.Done():
        cs.docker.ContainerKill(ctx, resp.ID, "SIGKILL")
        return nil, ctx.Err()
    }

    reader, err := cs.docker.ContainerLogs(ctx, resp.ID, types.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
    if err != nil {
        return nil, fmt.Errorf("read container logs: %w", err)
    }
    defer reader.Close()

    output, _ := io.ReadAll(reader)

    var result KnowledgeObject
    if err := json.Unmarshal(output, &result); err != nil {
        return nil, fmt.Errorf("parse step output: %w", err)
    }

    return &result, nil
}
```

### Sandbox Configuration Schema

Stored in `pipelines.sandbox` field as JSON:

```json
{
  "enabled": true,
  "isolation_level": "container",
  "resource_limits": {
    "max_memory": "1GB",
    "max_cpu": "100%",
    "timeout": "60s"
  },
  "network": false,
  "filesystem": {
    "read_only": ["assets/", "references/"],
    "write_allowed": false
  }
}
```

### Isolation Level Selection

**Default:** `process` isolation (no Docker required)

**User control:**
- Per-pipeline `sandbox.isolation_level` configuration
- dPKMS validates Docker availability on startup for container isolation
- Container isolation unavailable → fallback to process isolation with warning

**Step metadata override:**
Steps can specify `minimum_isolation_level` in metadata (e.g., `process` or `container`). Pipeline configuration must meet minimum requirement.

---

## Rationale

### Alternatives Considered

#### 1. Only Process Isolation (Rejected)

Reject because untrusted steps cannot be adequately isolated. `RLIMIT_*` provides resource limits but:
- No filesystem sandboxing (steps can read/write anywhere dPKMS process can)
- No network isolation (steps can open arbitrary network connections)
- Process isolation doesn't prevent privilege escalation if dPKMS runs with elevated permissions

#### 2. Only Container Isolation (Rejected)

Reject because:
- Docker adds dependency and complexity
- Overhead for trusted steps (100-500ms startup per step)
- Not available on all platforms (e.g., Windows desktop without WSL2)
- Overkill for built-in steps that are part of dPKMS codebase

#### 3. VM Isolation (e.g., Firecracker, gVisor) (Rejected)

Reject because:
- Significant overhead (seconds of startup time)
- Overkill for short-lived steps
- Complex dependency on hypervisor infrastructure
- Unnecessary for typical plugin use cases

#### 4. No Isolation (Unsandboxed Execution) (Rejected)

Reject because:
- Malicious steps can cause arbitrary harm
- No protection against buggy steps
- Violates ADR-027 plugin isolation requirements

### Benefits of Chosen Approach

- **Flexibility:** Choose appropriate isolation per pipeline
- **Performance:** Process isolation for trusted steps (near-zero overhead)
- **Security:** Container isolation for untrusted steps (strong isolation)
- **Simplicity:** No isolation for development/testing (can disable sandbox entirely)
- **Backward compatibility:** Process isolation has no new dependencies
- **Progressive adoption:** Users can enable container isolation when needed

---

## Consequences

### Positive

- Two-tier isolation meets diverse security/performance needs
- Default process isolation works out-of-the-box (no Docker required)
- Container isolation provides strong security when needed
- Per-pipeline configuration allows granular control
- Step metadata can enforce minimum isolation requirements

### Negative

- Container isolation requires Docker daemon (new dependency for users needing strong isolation)
- Process isolation provides limited security (no filesystem or network isolation)
- Two isolation levels increase complexity of sandbox implementation
- Users must understand trade-offs to choose appropriate isolation level

### Neutral

- Sandbox can be disabled entirely (`sandbox.enabled: false`) for development or trusted environments
- Resource limit parsing adds validation complexity (e.g., "1GB" → bytes)
- Docker SDK integration requires additional Go dependencies

---

## Implementation Notes

### Validation

1. **Docker availability:** Check on startup if container isolation configured
2. **Isolation level validation:** Only `process` or `container` allowed
3. **Resource limit parsing:** Validate format (e.g., "1GB", "50%", "60s")
4. **Step metadata compatibility:** Pipeline isolation level must meet step minimum

### Fallback Behavior

- Container isolation unavailable but configured → fallback to process isolation with warning log
- Sandbox configuration invalid → reject pipeline creation with validation error

### Error Handling

- Docker daemon not reachable → clear error message: "Container isolation requires Docker daemon"
- Container creation fails → detailed error from Docker API
- Resource limit parsing fails → validation error with expected format

---

## References

- **ADR-004** – Step-Based Pipeline Architecture (defines step execution interface)
- **ADR-027** – Plugin Isolation and Sandboxing (general plugin isolation requirements)
- **US-0112** – Configure Sandbox per Pipeline (user story for sandbox configuration)
- **Docker API Documentation** – ContainerCreate, ContainerStart, ContainerWait, ContainerLogs

---
