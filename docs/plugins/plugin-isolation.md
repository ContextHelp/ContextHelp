# Plugin Isolation Mechanism

This document provides detailed operational guidance on the plugin isolation and sandboxing mechanism for ContextHelp. It supplements [ADR-027](../decisions/ADR-027-plugin-isolation-sandboxing.md) with implementation-focused specifications for developers building and operating plugins.

---

## Overview

ContextHelp implements a **capability-based isolation model** that balances security with extensibility. Plugins run in-process but operate within strict permission boundaries enforced at runtime.

**Key Design Principles:**
- **Capability-scoped** - Explicit permissions declared in manifest
- **Runtime-enforced** - Not trust-based; every operation validated
- **Least-privilege** - Minimal scope granted by default
- **Auditable** - All privileged operations logged
- **User-approved** - Install-time consent required

---

## Isolation Mechanism Selection

### Evaluated Approaches

| Approach | Pros | Cons | Decision |
|----------|------|------|----------|
| **Capability-based (Chosen)** | Low overhead, granular control, in-process speed | Requires careful API design | **Selected** |
| WASM Sandboxing | Strong isolation, portable | Limited language support, no native libs, performance overhead | Rejected |
| Process Isolation | Complete isolation | High IPC overhead, complex debugging, resource intensive | Rejected |
| Container/namespace | OS-level isolation | Heavy infrastructure, overkill for plugins | Rejected |
| Trust-based | Zero overhead | No protection against malicious/buggy code | Rejected |

### Rationale for Capability-Based Approach

1. **Performance**: In-process calls avoid serialization and IPC overhead
2. **Developer Experience**: Native Go plugins with standard tooling
3. **Granularity**: Fine-grained permission control per resource type
4. **Auditability**: Every capability check logged for debugging and forensics
5. **Flexibility**: Can escalate to stricter isolation for specific plugins if needed

---

## Permission Model

### Capability Categories

```yaml
capabilities:
  # Filesystem - Where the plugin can read/write
  filesystem:
    read: ["~/.config/ctxt/plugins/my-plugin/"]
    write: ["~/.config/ctxt/plugins/my-plugin/cache/"]
    reason: "Cache processed data for faster retrieval"

  # Network - External connections allowed
  network:
    domains: ["api.example.com", "*.example.org"]
    protocols: ["https"]
    max_connections: 10
    timeout_seconds: 30
    reason: "Fetch external data from API"

  # Storage - dPKMS/ctxt data access
  storage:
    read: ["objects:type=bookmark", "entities:*"]
    write: ["objects:metadata.plugin_my_plugin"]
    reason: "Enrich bookmarks with external metadata"

  # API - Internal service access
  api:
    scopes: ["read:objects", "write:inbox"]
    reason: "Add enriched items to user's inbox"

  # Background Jobs - Scheduled execution
  cron:
    - schedule: "0 */6 * * *"
      job: "sync-external"
      reason: "Periodic sync with external API"

  # External Processes - Shell command execution
  exec:
    commands: ["/usr/bin/pandoc"]
    reason: "Convert document formats"

  # Registry - Knowledge registry access
  registries:
    allowed: ["public-registry"]
    reason: "Query public taxonomy"
```

### Permission Granularity

**Filesystem Permissions:**
- Paths are glob-matchable (`~/.config/ctxt/plugins/*/cache/**`)
- Symlinks resolved before matching (no traversal escape)
- Paths canonicalized to absolute form

**Network Permissions:**
- Domains support wildcards (`*.example.com`)
- Port restrictions optional (default: standard ports only)
- TLS verification always enforced
- Connection pooling shared but quota-tracked

**Storage Permissions:**
- Pattern format: `resource:filter`
- Resources: `objects`, `entities`, `edges`, `jobs`
- Filters: `type=X`, `metadata.field`, `*` (all)
- Write restrictions: Only metadata under `plugin_<name>` namespace

---

## Security Boundaries

### What Plugins CAN Do

- Register new pipeline steps and pipelines
- Define new knowledge object types
- Store metadata under `object.plugins.<pluginName>`
- Maintain isolated state in plugin directory
- Make approved network requests
- Execute approved shell commands
- Schedule background jobs
- Communicate with other plugins via exported APIs
- Extend CLI/REST/gRPC interfaces in namespaced routes

### What Plugins CANNOT Do

| Forbidden Action | Enforcement | Reason |
|-----------------|-------------|--------|
| Direct database access | No DB handle exposed | Schema integrity |
| Core schema modification | API contracts only | Stability |
| Override core types | Type registry locked | Predictability |
| Modify other plugins' storage | Namespace isolation | Plugin independence |
| Arbitrary filesystem access | Path validation | Security |
| Arbitrary network access | Domain allowlist | Privacy |
| Modify job state machine | Read-only job API | Reliability |
| Bypass user consent | Install-time approval | Trust |

### Runtime Enforcement

Every plugin API call passes through a capability checker:

```
Plugin Call
    |
    v
+------------------+
| Capability Check |
+------------------+
    |           |
    v           v
 Allowed     Denied
    |           |
    v           v
 Execute    Log + Error
    |
    v
 Audit Log
```

**Enforcement is non-bypassable**: The plugin SDK only exposes sandboxed APIs. Native Go code in plugins cannot access internals without going through the SDK.

---

## Plugin API Surface

### Exposed Interfaces

```go
// Plugin SDK - Only these interfaces are available to plugins
type PluginRuntime interface {
    // Filesystem (checked against capabilities)
    ReadFile(path string) ([]byte, error)
    WriteFile(path string, data []byte) error

    // Network (checked against capabilities)
    HTTPClient() *SandboxedHTTPClient

    // Storage (checked against capabilities)
    QueryObjects(filter string) ([]Object, error)
    CreateObject(obj Object) error
    UpdateObjectMetadata(id, key string, value any) error

    // Plugin-Namespaced KV Store (always allowed within namespace)
    KVGet(key string) ([]byte, error)
    KVSet(key string, value []byte) error
    KVDelete(key string) error

    // Job Queue (quota-limited)
    EnqueueJob(job Job) error

    // Logging (automatically prefixed)
    Log(level, message string, fields map[string]any)

    // Configuration (read-only, plugin section only)
    Config() PluginConfig
}
```

### Contracts and Guarantees

**Stability Guarantees:**
- Hook names remain stable or properly deprecated (2-version window)
- Plugin storage paths never change
- Metadata namespace `plugins.<name>` preserved through migrations
- API versioned with semver (breaking changes = major version)

**Performance Contracts:**
- Capability checks: < 1 microsecond
- KV operations: < 1 millisecond
- Network quotas enforced per-connection
- Job queue quotas enforced globally per plugin

---

## Threat Model

### Threats Addressed

| Threat | Mitigation |
|--------|------------|
| Malicious plugin data exfiltration | Network domain allowlist |
| Credential theft | No access to secrets outside plugin scope |
| Core data corruption | No direct DB access; namespace isolation |
| Denial of service | Resource quotas (CPU, memory, connections, jobs) |
| Supply-chain attack | Signature verification (optional) |
| Privilege escalation | Capability enforcement at runtime |
| Path traversal | Absolute path resolution; symlink following |
| Shell injection | Command allowlist; argument sanitization |

### Trust Levels

1. **Official Plugins**: Shipped with ctxt, full audit, elevated trust
2. **Verified Publishers**: Community-verified, signature-checked
3. **Third-Party**: User-approved only, full sandboxing
4. **Development**: Local plugins, relaxed for testing (flagged)

### Audit Trail

All privileged operations logged:

```sql
-- Audit log schema
plugin_audit_log (
    id UUID,
    plugin_id TEXT,
    operation TEXT,      -- filesystem.read, network.connect, storage.write
    resource TEXT,       -- path, url, object_id
    result TEXT,         -- allowed, denied
    error_message TEXT,
    metadata JSONB,
    created_at TIMESTAMP
)
```

Query audit trail:
```bash
ctxt plugin audit <name> --since "7 days ago"
ctxt plugin audit --result denied --since "24 hours ago"
```

---

## Resource Quotas

### Default Limits

```yaml
resource_limits:
  cpu_time_seconds: 300      # Per job
  memory_mb: 512             # Per plugin
  storage_mb: 1024           # Plugin directory
  max_concurrent_jobs: 5     # Simultaneous
  max_queued_jobs: 100       # Queue depth
  max_connections: 10        # Network
  max_query_results: 10000   # Per query
  max_query_time_ms: 5000    # Query timeout
```

### Enforcement Behavior

| Resource | Exceeded Action |
|----------|----------------|
| CPU time | Job terminated |
| Memory | Job terminated |
| Storage | Write rejected |
| Concurrent jobs | Job queued |
| Queued jobs | Enqueue rejected |
| Connections | Connection rejected |
| Query results | Results truncated |
| Query time | Query cancelled |

---

## User Approval Workflow

### Installation

```bash
$ ctxt plugin install rss-feed-monitor

Plugin: rss-feed-monitor v1.2.0
Author: @example
Homepage: https://github.com/example/ctxt-plugin-rss

This plugin requests the following permissions:

Filesystem Access:
  Read:  ~/.config/ctxt/plugins/rss-feed-monitor/
  Write: ~/.config/ctxt/plugins/rss-feed-monitor/cache/

Network Access:
  Domains: *.rss.example.com, feeds.news.com
  Reason: Fetch RSS feed content

Storage Access:
  Read:  objects with type=rss_feed
  Write: object metadata under plugin_rss

Background Jobs:
  Every 15 minutes - Check for new feed entries

Install this plugin? [y/N/details]:
```

### Permission Revocation

```bash
# Revoke specific capability
ctxt plugin permissions revoke rss-feed --capability network

# Revoke and disable
ctxt plugin disable rss-feed

# Full uninstall
ctxt plugin uninstall rss-feed
```

---

## Developer Guidelines

### Manifest Best Practices

1. **Request minimum permissions**: Only what's needed
2. **Provide clear reasons**: Help users understand why
3. **Use narrow patterns**: `*.example.com` not `*`
4. **Version your capabilities**: Update manifest when needs change
5. **Document data usage**: What's stored, for how long

### Testing Plugin Isolation

```go
// Test capability enforcement
func TestPluginCannotAccessUnauthorizedPath(t *testing.T) {
    plugin := loadTestPlugin(t, minimalManifest)

    _, err := plugin.ReadFile("/etc/passwd")
    if err == nil {
        t.Fatal("expected permission denied")
    }

    assertAuditLogContains(t, "filesystem.read", "/etc/passwd", "denied")
}
```

### Debugging Permission Issues

```bash
# View plugin capabilities
ctxt plugin permissions <name>

# View recent denials
ctxt plugin audit <name> --result denied

# Run with verbose logging
CTXT_PLUGIN_DEBUG=1 ctxt <command>
```

---

## See Also

- [ADR-027 - Plugin Isolation and Sandboxing](../decisions/ADR-027-plugin-isolation-sandboxing.md) - Architectural decision
- [Plugin API Specification](plugins-api.md) - Complete API reference
- [Plugin System Overview](plugins.md) - General plugin architecture
- [Security Model](../dpkms/security.md) - System-wide security

---
