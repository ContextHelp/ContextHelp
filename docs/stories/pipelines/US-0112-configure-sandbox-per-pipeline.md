---
status: shipped
---

# US-0112: Configure Sandbox per Pipeline

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md), [Plugin Developers](../../personas/README.md), [Registry Operators](../../personas/README.md)

---

## User Goal

As a pipeline creator, I want to configure sandbox settings for pipelines to control step execution:
- **Isolation level**: process or container
- **Resource limits**: Memory, CPU, timeout
- **Network access**: Allow or block
- **Filesystem constraints**: Read-only paths, write permissions

Sandboxing ensures system stability by isolating potentially untrusted or buggy steps.

## Context

Pipelines support sandbox configuration in their definition:
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
    read_only: ["assets/"]
    write_allowed: false
}
```

Sandbox isolation can be set to:
- **process** - Run in process with resource limits (via syscall rlimits)
- **container** - Run in Docker container with full isolation

## Acceptance Criteria

- **Sandbox configuration is stored** in pipeline record
- **Sandbox configuration applies only when enabled**
- **Step execution enforces sandbox constraints**
- **Validation** prevents pipelines with incompatible sandbox settings from being created
- **Default sandbox is no sandboxing** (unconstrained execution)**
- **Docker is required for container-level isolation**

## Implementation Notes

### Sandbox Config Storage

Add to Pipeline table (see migration 002_add_pipelines.sql):
```sql
sandbox TEXT DEFAULT NULL  -- JSON blob of SandboxConfig
```

### Sandbox Configuration Parsing

```go
type SandboxConfig struct {
    Enabled        bool     `json:"enabled,omitempty"`
    IsolationLevel string     `json:"isolation_level,omitempty"` // "process" or "container"
    ResourceLimits ResourceLimitsConfig `json:"resource_limits,omitempty"`
    Network        bool     `json:"network,omitempty"`
    Filesystem     FilesystemSandboxConfig `json:"filesystem,omitempty"`
}

type ResourceLimitsConfig struct {
    MaxMemory string `json:"max_memory,omitempty"`
    MaxCPU    string `json:"max_cpu,omitempty"`
    Timeout   string `json:"timeout,omitempty"`
}

type FilesystemSandboxConfig struct {
    ReadOnly      []string `json:"read_only,omitempty"`
    WriteAllowed bool    `json:"write_allowed,omitempty"`
}
}
```

### Process-Level Isolation

Uses `syscall.Rlimit`:
```go
import (
    "syscall"
    "time"
)

func applyResourceLimits(rlimits *ResourceLimitsConfig) map[uint64]syscall.Rlimit) {
    var rlimit syscall.Rlimit
    rlimit.Memory = 0 // RLIMIT_AS
    rlimit.CPU = 0       // RLIMIT_CPU
    // rlimit.NoFile = 1024   // RLIMIT_NOFILE
    rlimit.Fs = nil       // RLIMIT_FSIZE
}

    // Apply to subprocess
    cmd.SysProcAttr = &syscall.SysProcAttr{
        RLimits: []syscall.Rlimit{rlimits},
    CpuPercentages: &rlimit.CPU},
        Env: sandboxedEnv,
    },
}
}
```

### Container-Level Isolation

Uses Docker SDK:
```go
import (
    "context"
    "github.com/docker/docker/client"
)

type SandboxDocker struct {
    DockerClient *docker.Client
    ContainerConfig *docker.ContainerConfig
}

func (sd *SandboxDocker) Apply(
    sandbox *SandboxConfig,
    draft *SandboxDocker,
    draft *storage.KnowledgeObject,
) -> (*storage.KnowledgeObject, error)
) {
    // Check Docker availability
    if _, err := exec.LookPath("docker"); err != nil {
        return nil, fmt.Errorf("docker required for container isolation: %w", err)
    }
    
    // Pull or use default image
    image := "alpine:3" // or step metadata.ContainerImage
    
    containerConfig := &docker.ContainerConfig{
        Image:        image,
        Cmd:          stepScriptPath,
        Env:          sandboxedEnv,
        WorkingDir:    "/workspace",
        ReadonlyRootfs: true,
        NetworkDisabled: !sandbox.Network,
        Mounts: []mount.Mount{
            {
                Source: workspaceVolume,
                Target: "/workspace",
                Type:   "volume",
                Options: []mount.Option{Type: "bind" },
        },
    }
        HostConfig: &docker.HostConfig{
            Memory:   sandbox.ResourceLimits.MaxMemory,
            NanoCPUs: sandbox.ResourceLimits.MaxCPU,
        },
        Binds: buildReadOnlyBinds(sandbox.Filesystem.ReadOnly),
    }
    }
    
    resp, err := sd.Docker.ContainerCreate(ctx, step.Name, containerConfig, hostConfig)
    defer sd.Docker.ContainerRemove(ctx, resp.ID, types.ContainerRemoveOptions{})
    if err != nil {
        return nil, fmt.Errorf("create container: %w", err)
    }
    
    if err := sd.Docker.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
        return nil, fmt.Errorf("start container: %w", err)
    }
    
    // Wait for completion
    statusCh, errCh := sd.Docker.ContainerWait(ctx, resp.ID, types.ContainerWaitConditionNotRunning)
    select {
    case status := <-statusCh:
        default:
            return nil, fmt.Errorf("container exited with status %d", status)
    }
    case err := <-errCh:
        return nil, fmt.Errorf("container wait error: %w", err)
    }
    
    // Read output
    reader, err := sd.Docker.ContainerLogs(ctx, resp.ID, types.ContainerLogsOptions{ShowStdout: true})
    if err != nil {
        return nil, fmt.Errorf("read container logs error: %w", err)
    }
    defer reader.Close()
    
    output, _ := io.ReadAll(reader)
    
    // Parse output
    var result StepResult
    if err := json.Unmarshal(output, &result); err != nil {
        return nil, fmt.Errorf("parse step output: %w", err)
    }
    
    if result.Error != nil {
        return nil, fmt.Errorf("step failed: %s: result.Error)
    }
    
    return result.Object, nil
}
}
```

### Validation

- Docker availability checked at service startup
- Sandbox configuration requires Docker for container-level
- Isolation level validates Docker dependency
- Process isolation uses syscall rlimits as fallback

## E2E Test Checklist

- [ ] Create pipeline with sandbox config → request payload contains `sandbox` object with all
  specified fields: `enabled`, `isolation_level`, `resource_limits`, `network`, `filesystem`
- [ ] Verify `resource_limits.max_memory` value in request payload matches CLI/config input
- [ ] Verify `resource_limits.max_cpu` value in request payload matches CLI/config input
- [ ] Verify `resource_limits.timeout` value in request payload matches CLI/config input
- [ ] Verify `filesystem.read_only` array in request payload matches CLI/config input
- [ ] Verify `filesystem.write_allowed` value in request payload matches CLI/config input
- [ ] Create pipeline with sandbox enabled=true, isolation_level=container → validates Docker
  dependency; sandbox config stored as JSON blob in `pipelines.sandbox` column
- [ ] Create pipeline with sandbox enabled=false, isolation_level=process → validates process
  isolation; `sandbox` JSON blob stored in DB with `enabled=false`
- [ ] Create pipeline with sandbox enabled=false → validates no sandbox isolation (same as
  default); `sandbox` field null or `{enabled:false}` in DB
- [ ] Create pipeline with invalid resource limits → validation error; no record created in DB
- [ ] Create pipeline with network=true → validation error (network not allowed in sandboxed
  pipeline); no record created in DB
- [ ] Verify sandbox configuration is persisted correctly in DB:
  `json_extract(sandbox, '$.isolation_level')`, `json_extract(sandbox, '$.network')`, etc.
  all match input values
- [ ] `dpkms pipeline show <name>` → response includes complete `sandbox` object; all fields
  match DB stored value
- [ ] `dpkms pipeline archive <name>` → `archived=1` in DB; sandbox config unchanged
- [ ] Auto-update flag is respected (default false, can be enabled with config or via CLI)

## Related Stories

- [US-0101](./US-0101-create-custom-pipeline.md) - Create pipelines (allows sandbox config specification)
- [US-0102](./US-0102-list-and-filter-pipelines.md) - List pipelines (shows sandbox status)
- [US-0103](./US-0103-show-pipeline-details.md) - Show pipeline configuration (includes sandbox)
- [US-0106](./US-0106-enqueue-content-via-dpkms.md) - Enqueue (only method, enforces sandbox)
- [US-0107](./US-0107-discover-local-steps.md) - Discover available steps
- [US-0108](./US-0108-install-step-from-registry.md) - Install from registry
- [US-0109](./US-0109-fetch-registry-manifest.md) - Fetch manifest (get available steps)
- [US-0110](./US-0110-check-registry-updates.md) - Check for updates
- [US-0111](./US-0111-configure-registry-autoupdate.md) - Configure auto-update
- [US-0113](./US-0113-ctxt-analyze-api-client.md) - ctxt as API client

## Related ADRs

- [ADR-055](../../decisions/ADR-055-sandbox-isolation-levels.md) - Sandbox isolation levels (this ADR)
- [ADR-056](../../decisions/ADR-056-unified-enqueue-api.md) - Unified enqueue API (this ADR defines)
- [ADR-057](../../decisions/ADR-057-registry-update-notification-model.md) - Registry update notification model (this ADR defines)
- [ADR-058](../../decisions/ADR-058-external-step-execution-protocol.md) - External step execution protocol

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Integrators](../../personas/platform-integrators.md)
- Automation Builder

---

## E2E Tests

- `test/integration/us0112_sandbox_test.go::TestUS0112_NonSandboxedStep_CanMakeOutboundCall`
- `test/integration/us0112_sandbox_test.go::TestUS0112_SandboxConfig_NoNetworkFlag`
- `test/integration/us0112_sandbox_test.go::TestUS0112_SandboxConfig_NetworkAllowed`
- `test/integration/us0112_sandbox_test.go::TestUS0112_SandboxConfig_RoundTrip_ViaCreateAPI`
- `test/integration/us0112_sandbox_test.go::TestUS0112_Sandbox_UnsupportedPlatformSkip`
