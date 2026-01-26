# ADR-027 – Plugin Isolation and Sandboxing

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

ContextHelp's plugin architecture enables extensibility at every layer—pipelines, storage, queries, registries, and background tasks. While this openness enables a rich ecosystem, it introduces significant security and stability risks:

**Security Risks:**
- Malicious plugins accessing sensitive knowledge
- Plugins exfiltrating data to external services
- Plugins tampering with core data structures
- Plugins executing arbitrary system commands
- Plugins reading credentials or API keys
- Supply-chain attacks via compromised dependencies

**Stability Risks:**
- Buggy plugins crashing the engine
- Plugins causing deadlocks or infinite loops
- Plugins consuming excessive memory/CPU
- Plugins interfering with other plugins
- Plugins breaking core functionality

**Privacy Risks:**
- Plugins accessing data outside their scope
- Plugins logging sensitive information
- Plugins sharing data without consent
- Plugins violating data retention policies

**User Experience Risks:**
- Unclear what permissions a plugin requires
- No way to revoke plugin access after installation
- Difficult to debug plugin misbehavior
- No audit trail for plugin actions
- Users unknowingly installing malicious plugins

**Design Constraints:**
- Plugins must remain powerful (not hobbled by restrictions)
- Capability system must be granular yet understandable
- Enforcement must happen at runtime (not trust-based)
- Plugins from untrusted sources must be sandboxed
- Performance overhead must be minimal
- Official plugins may have elevated permissions
- User must approve permissions explicitly
- Audit trail required for all privileged operations

**Affected Subsystems:**
- Plugin runtime (capability enforcement)
- Storage layer (plugin-owned namespaces)
- Network layer (outbound connection restrictions)
- Filesystem (restricted read/write paths)
- Job queue (resource quotas)
- API layer (scope enforcement)
- Configuration (secure credential access)

**Goals:**
- Prevent malicious plugins from harming users
- Enable granular capability declarations
- Enforce least-privilege principle
- Provide clear permission prompts to users
- Audit all privileged plugin operations
- Preserve plugin ecosystem openness
- Maintain backward compatibility

---

## Decision

**ContextHelp will implement a mandatory capability-based plugin isolation and sandboxing model with explicit permission declarations, runtime enforcement, user approval workflows, resource quotas, isolated storage namespaces, and comprehensive audit logging, ensuring plugins operate within defined boundaries while preserving extensibility.**

The plugin isolation system provides:

1. **Capability Model:**

   **Core Principles:**
   - Explicit declaration (manifest-based)
   - User approval required (install-time)
   - Runtime enforcement (not trust-based)
   - Least privilege (minimal scopes)
   - Composable (multiple capabilities)
   - Auditable (all operations logged)

   **Capability Categories:**
   ```yaml
   capabilities:
     # Filesystem access
     filesystem:
       read: ["~/.config/ctxt/plugins/my-plugin/"]
       write: ["~/.config/ctxt/plugins/my-plugin/cache/"]
       reason: "Cache processed data for faster retrieval"

     # Network access
     network:
       domains: ["api.example.com", "cdn.example.com"]
       protocols: ["https"]
       reason: "Fetch external data from API"

     # Database/Storage access
     storage:
       read: ["objects:type=bookmark", "entities:*"]
       write: ["objects:metadata.plugin_my_plugin"]
       reason: "Enrich bookmarks with external metadata"

     # API scopes
     api:
       scopes: ["read:objects", "write:inbox"]
       reason: "Add enriched items to user's inbox"

     # Background jobs
     cron:
       - schedule: "0 */6 * * *"
         reason: "Periodic sync with external API"

     # External processes
     exec:
       commands: ["/usr/bin/pandoc", "/usr/local/bin/ffmpeg"]
       reason: "Convert document formats"

     # Registry access
     registries:
       allowed: ["public-registry", "plugin-specific-registry"]
       reason: "Query plugin-specific taxonomy"
   ```

2. **Plugin Manifest Schema:**

   **Complete Manifest Example:**
   ```yaml
   # plugin.yaml
   name: rss-feed-monitor
   version: 1.2.0
   author: "@example"
   license: MIT
   homepage: "https://github.com/example/ctxt-plugin-rss"

   # Plugin description
   description: "Monitors RSS feeds and adds new entries to inbox"

   # Required ctxt version
   requires:
     ctxt: ">=1.0.0"
     dpkms: ">=1.0.0"

   # Capability declarations
   capabilities:
     filesystem:
       read:
         - path: "~/.config/ctxt/plugins/rss-feed-monitor/"
           reason: "Read feed configuration"
       write:
         - path: "~/.config/ctxt/plugins/rss-feed-monitor/cache/"
           reason: "Cache feed items to detect new entries"

     network:
       domains:
         - "*.example.com"
         - "feeds.news.com"
       protocols: ["https", "http"]
       max_connections: 10
       timeout_seconds: 30
       reason: "Fetch RSS feed content"

     storage:
       read:
         - "objects:type=rss_feed"
         - "objects:metadata.rss_url"
       write:
         - "objects:metadata.plugin_rss"
         - "objects:type=rss_feed"
       reason: "Store and retrieve RSS feed items"

     api:
       scopes:
         - "read:objects"
         - "write:inbox"
       reason: "Add new feed entries to inbox"

     cron:
       - schedule: "*/15 * * * *"
         job: "check-feeds"
         reason: "Check for new feed entries every 15 minutes"

   # Extension points
   extends:
     pipelines:
       - name: "rss.feed"
         triggers: ["url:pattern=*.xml", "url:pattern=*/feed"]

     object_types:
       - name: "rss_feed"
         schema: "./schemas/rss_feed.json"

     cli:
       - command: "feed"
         subcommands: ["add", "list", "remove", "check"]

   # Dependencies (other plugins)
   dependencies:
     optional:
       - name: "notification-plugin"
         reason: "Send notifications for new feed items"

   # Configuration schema
   config_schema: "./config.schema.json"
   ```

3. **Runtime Capability Enforcement:**

   **Filesystem Sandbox:**
   ```go
   type FilesystemSandbox struct {
       pluginID     string
       allowedReads []PathPattern
       allowedWrites []PathPattern
   }

   func (fs *FilesystemSandbox) CheckRead(path string) error {
       // Resolve path to absolute
       absPath, err := filepath.Abs(path)
       if err != nil {
           return err
       }

       // Check against allowed read paths
       for _, pattern := range fs.allowedReads {
           if pattern.Matches(absPath) {
               logAudit(fs.pluginID, "filesystem.read", absPath)
               return nil
           }
       }

       return fmt.Errorf("plugin %s not authorized to read %s", fs.pluginID, absPath)
   }

   func (fs *FilesystemSandbox) CheckWrite(path string) error {
       absPath, err := filepath.Abs(path)
       if err != nil {
           return err
       }

       // Prevent path traversal
       if strings.Contains(absPath, "..") {
           return fmt.Errorf("path traversal not allowed")
       }

       // Check against allowed write paths
       for _, pattern := range fs.allowedWrites {
           if pattern.Matches(absPath) {
               logAudit(fs.pluginID, "filesystem.write", absPath)
               return nil
           }
       }

       return fmt.Errorf("plugin %s not authorized to write %s", fs.pluginID, absPath)
   }
   ```

   **Network Sandbox:**
   ```go
   type NetworkSandbox struct {
       pluginID         string
       allowedDomains   []DomainPattern
       allowedProtocols []string
       maxConnections   int
       timeout          time.Duration
       activeConns      int32  // atomic counter
   }

   func (ns *NetworkSandbox) CheckConnection(url string) error {
       // Parse URL
       u, err := url.Parse(url)
       if err != nil {
           return err
       }

       // Check protocol
       if !contains(ns.allowedProtocols, u.Scheme) {
           return fmt.Errorf("protocol %s not allowed", u.Scheme)
       }

       // Check domain
       allowed := false
       for _, pattern := range ns.allowedDomains {
           if pattern.Matches(u.Host) {
               allowed = true
               break
           }
       }

       if !allowed {
           return fmt.Errorf("domain %s not allowed", u.Host)
       }

       // Check connection limit
       if atomic.LoadInt32(&ns.activeConns) >= int32(ns.maxConnections) {
           return fmt.Errorf("max connections (%d) reached", ns.maxConnections)
       }

       logAudit(ns.pluginID, "network.connect", url)
       return nil
   }
   ```

   **Storage Sandbox:**
   ```go
   type StorageSandbox struct {
       pluginID      string
       allowedReads  []StoragePattern
       allowedWrites []StoragePattern
   }

   type StoragePattern struct {
       Resource string  // objects, entities, etc.
       Filter   string  // type=bookmark, metadata.key=*
   }

   func (ss *StorageSandbox) CheckRead(resource string, filter string) error {
       for _, pattern := range ss.allowedReads {
           if pattern.Matches(resource, filter) {
               logAudit(ss.pluginID, "storage.read", resource+":"+filter)
               return nil
           }
       }

       return fmt.Errorf("plugin %s not authorized to read %s:%s",
           ss.pluginID, resource, filter)
   }

   func (ss *StorageSandbox) CheckWrite(resource string, field string) error {
       for _, pattern := range ss.allowedWrites {
           if pattern.Matches(resource, field) {
               logAudit(ss.pluginID, "storage.write", resource+":"+field)
               return nil
           }
       }

       return fmt.Errorf("plugin %s not authorized to write %s:%s",
           ss.pluginID, resource, field)
   }
   ```

   **Process Execution Sandbox:**
   ```go
   type ExecSandbox struct {
       pluginID        string
       allowedCommands []string
   }

   func (es *ExecSandbox) CheckExec(command string, args []string) error {
       // Resolve command to absolute path
       cmdPath, err := exec.LookPath(command)
       if err != nil {
           return err
       }

       // Check if command is allowed
       allowed := false
       for _, allowedCmd := range es.allowedCommands {
           if cmdPath == allowedCmd {
               allowed = true
               break
           }
       }

       if !allowed {
           return fmt.Errorf("command %s not allowed", command)
       }

       // Prevent shell injection
       for _, arg := range args {
           if containsShellMetachars(arg) {
               return fmt.Errorf("shell metacharacters not allowed in arguments")
           }
       }

       logAudit(es.pluginID, "exec", cmdPath+" "+strings.Join(args, " "))
       return nil
   }
   ```

4. **Resource Quotas:**

   **Plugin Resource Limits:**
   ```yaml
   resource_limits:
     # CPU time limit per job
     cpu_time_seconds: 300  # 5 minutes max

     # Memory limit
     memory_mb: 512

     # Disk storage limit
     storage_mb: 1024

     # Job queue limits
     max_concurrent_jobs: 5
     max_queued_jobs: 100

     # Network limits
     max_connections: 10
     bandwidth_mbps: 10

     # Database query limits
     max_query_results: 10000
     max_query_time_ms: 5000
   ```

   **Enforcement:**
   ```go
   type ResourceMonitor struct {
       pluginID string
       limits   ResourceLimits
       usage    ResourceUsage
   }

   func (rm *ResourceMonitor) CheckJobExecution(jobID string) error {
       // Check concurrent job limit
       if rm.usage.ConcurrentJobs >= rm.limits.MaxConcurrentJobs {
           return ErrTooManyConcurrentJobs
       }

       // Check queued job limit
       if rm.usage.QueuedJobs >= rm.limits.MaxQueuedJobs {
           return ErrQueueFull
       }

       // Start resource tracking
       rm.trackJob(jobID)

       return nil
   }

   func (rm *ResourceMonitor) TrackMemory(jobID string) error {
       currentUsage := getMemoryUsage(jobID)

       if currentUsage > rm.limits.MemoryMB*1024*1024 {
           killJob(jobID)
           return fmt.Errorf("memory limit exceeded: %d MB", currentUsage/(1024*1024))
       }

       return nil
   }
   ```

5. **Plugin Storage Isolation:**

   **Storage Namespaces:**
   ```
   Plugin storage structure:
   ~/.config/ctxt/plugins/<plugin-name>/
     config.yaml          # Plugin configuration
     cache/               # Plugin-managed cache
     data/                # Plugin persistent data
     temp/                # Temporary files (cleaned periodically)
     logs/                # Plugin-specific logs
   ```

   **Isolated KV Store:**
   ```go
   // Plugin-scoped key-value store
   type PluginKVStore struct {
       pluginID string
       backend  KVBackend
   }

   func (kv *PluginKVStore) Get(key string) ([]byte, error) {
       // Automatically namespace key by plugin ID
       nsKey := fmt.Sprintf("plugin:%s:%s", kv.pluginID, key)
       return kv.backend.Get(nsKey)
   }

   func (kv *PluginKVStore) Set(key string, value []byte) error {
       nsKey := fmt.Sprintf("plugin:%s:%s", kv.pluginID, key)
       logAudit(kv.pluginID, "kv.set", key)
       return kv.backend.Set(nsKey, value)
   }
   ```

6. **User Approval Workflow:**

   **Installation Prompt:**
   ```bash
   ctxt plugin install rss-feed-monitor

   # Output:
   Plugin: rss-feed-monitor v1.2.0
   Author: @example
   Homepage: https://github.com/example/ctxt-plugin-rss

   This plugin requests the following permissions:

   Filesystem Access:
     Read:
       • ~/.config/ctxt/plugins/rss-feed-monitor/
         Reason: Read feed configuration

     Write:
       • ~/.config/ctxt/plugins/rss-feed-monitor/cache/
         Reason: Cache feed items to detect new entries

   Network Access:
     • *.example.com, feeds.news.com (https, http)
       Reason: Fetch RSS feed content
     • Max connections: 10, Timeout: 30s

   Storage Access:
     Read:
       • Objects with type=rss_feed
       • Object metadata: rss_url

     Write:
       • Object metadata: plugin_rss
       • Create objects with type=rss_feed

     Reason: Store and retrieve RSS feed items

   API Access:
     • read:objects (Query your knowledge base)
     • write:inbox (Add new items to inbox)

   Background Jobs:
     • Every 15 minutes
       Reason: Check for new feed entries

   Resource Limits:
     • CPU: 5 minutes per job
     • Memory: 512 MB
     • Storage: 1 GB
     • Max concurrent jobs: 5

   Install this plugin? [y/N/show-details]:
   ```

   **Permission Updates:**
   ```bash
   # Plugin update requests new permission
   ctxt plugin update rss-feed-monitor

   # Output:
   Plugin rss-feed-monitor v1.3.0 requests new permissions:

   NEW PERMISSION:
   Execute Commands:
     • /usr/bin/pandoc
       Reason: Convert feed content to Markdown

   Previously approved permissions remain unchanged.

   Approve new permission? [y/N]:
   ```

7. **Audit Trail:**

   **Audit Log Schema:**
   ```sql
   CREATE TABLE plugin_audit_log (
       id UUID PRIMARY KEY,
       plugin_id TEXT NOT NULL,
       plugin_name TEXT NOT NULL,
       operation TEXT NOT NULL,  -- filesystem.read, network.connect, etc.
       resource TEXT NOT NULL,   -- path, url, object_id, etc.
       result TEXT NOT NULL,     -- allowed, denied, error
       error_message TEXT,
       metadata JSONB,
       created_at TIMESTAMP NOT NULL
   );

   CREATE INDEX idx_plugin_audit_plugin ON plugin_audit_log(plugin_id, created_at DESC);
   CREATE INDEX idx_plugin_audit_operation ON plugin_audit_log(operation, created_at DESC);
   ```

   **Audit Query:**
   ```bash
   # View plugin audit trail
   ctxt plugin audit rss-feed-monitor --since "7 days ago"

   # Output:
   2026-01-26 10:00:00 | filesystem.read    | ~/.config/.../config.yaml | allowed
   2026-01-26 10:00:01 | network.connect    | https://feeds.news.com    | allowed
   2026-01-26 10:00:02 | storage.write      | objects:metadata.plugin_rss | allowed
   2026-01-26 10:15:00 | network.connect    | https://malicious.com     | denied
   2026-01-26 10:15:01 | filesystem.write   | /etc/passwd               | denied

   # View denied operations across all plugins
   ctxt plugin audit --result denied --since "24 hours ago"
   ```

8. **Plugin Verification:**

   **Signature Verification:**
   ```bash
   # Install plugin with signature verification
   ctxt plugin install rss-feed-monitor --verify

   # Output:
   Verifying plugin signature...
   Publisher: ContextHelp Community
   Signature: valid ✓

   Plugin 'rss-feed-monitor' is verified by a trusted publisher.

   [Permission prompt follows...]
   ```

   **Trusted Publishers:**
   ```yaml
   trusted_publishers:
     - key_id: ed25519-ctxt-official
       name: "ContextHelp Official"
       trust_level: full
       auto_approve: false

     - key_id: ed25519-community-verified
       name: "Community Verified"
       trust_level: partial
       auto_approve: false
   ```

9. **CLI Plugin Management:**
   ```bash
   # Plugin installation
   ctxt plugin install <name>
   ctxt plugin install <name> --verify
   ctxt plugin install <path/to/plugin.tar.gz>

   # Plugin management
   ctxt plugin list
   ctxt plugin info <name>
   ctxt plugin enable <name>
   ctxt plugin disable <name>
   ctxt plugin uninstall <name>
   ctxt plugin update <name>

   # Permission management
   ctxt plugin permissions <name>
   ctxt plugin permissions revoke <name> --capability network
   ctxt plugin permissions grant <name> --capability network --domain "new.example.com"

   # Audit and monitoring
   ctxt plugin audit <name>
   ctxt plugin audit --result denied
   ctxt plugin stats <name>  # Resource usage

   # Security
   ctxt plugin verify <name>
   ctxt plugin trust list
   ctxt plugin trust add <pubkey-file> --name <publisher>
   ```

10. **Developer SDK:**

    **Plugin SDK Interface:**
    ```go
    package plugin

    // Plugin runtime provides sandboxed APIs
    type Runtime interface {
        // Filesystem (checked against capabilities)
        ReadFile(path string) ([]byte, error)
        WriteFile(path string, data []byte) error

        // Network (checked against capabilities)
        HTTPGet(url string) (*http.Response, error)
        HTTPPost(url string, body []byte) (*http.Response, error)

        // Storage (checked against capabilities)
        QueryObjects(filter string) ([]Object, error)
        CreateObject(obj Object) error
        UpdateObjectMetadata(id string, key string, value interface{}) error

        // KV store (plugin-namespaced)
        KVGet(key string) ([]byte, error)
        KVSet(key string, value []byte) error
        KVDelete(key string) error

        // Job queue
        EnqueueJob(job Job) error

        // Logging (automatically plugin-prefixed)
        Log(level string, message string, fields map[string]interface{})
    }

    // Plugin must implement this interface
    type Plugin interface {
        Name() string
        Initialize(runtime Runtime) error
        HandleEvent(event Event) error
        Shutdown() error
    }
    ```

---

## Rationale

### Alternatives Considered

#### 1. **Trust-Based Model (No Enforcement) (Rejected)**
Trust plugins to behave correctly without runtime enforcement.

**Rejected because:**
- Malicious plugins can harm users
- Buggy plugins can crash system
- No protection against supply-chain attacks
- Users have no visibility into plugin behavior
- Defeats security goals

#### 2. **Process-Level Sandboxing (Linux namespaces, Docker) (Rejected)**
Run each plugin in a separate process or container.

**Rejected because:**
- Excessive overhead for lightweight plugins
- Complex inter-process communication
- Difficult to debug
- Overkill for majority of plugins
- Performance impact unacceptable

#### 3. **WASM-Based Plugins (Rejected)**
Require all plugins to be WebAssembly modules.

**Rejected because:**
- Too restrictive (no native libraries)
- Limited language support
- WASM runtime overhead
- Ecosystem immaturity for this use case
- Defeats native Go plugin benefits

#### 4. **Coarse-Grained Permissions (Rejected)**
Simple "trusted" vs "untrusted" classification.

**Rejected because:**
- Too coarse (all or nothing)
- Doesn't follow least privilege
- Users can't make informed decisions
- No granular audit trail

#### 5. **Manual Code Review Only (Rejected)**
Rely on human code review to verify plugin safety.

**Rejected because:**
- Doesn't scale
- Users can't review code
- Dynamic behavior hard to detect
- Supply-chain attacks bypass review
- Runtime enforcement essential

### Benefits of Chosen Approach

**Security:**
- Malicious plugins contained
- Least privilege enforced
- Audit trail for accountability
- User approval required
- Signature verification supported

**Clarity:**
- Explicit capability declarations
- Clear permission prompts
- Understandable to non-technical users
- Reasoning provided for each capability

**Flexibility:**
- Granular capability system
- Multiple capability types
- Composable permissions
- Per-resource patterns supported

**Performance:**
- Minimal overhead (capability checks are fast)
- No process boundaries (in-process enforcement)
- Native Go plugins supported
- Resource quotas prevent abuse

**Ecosystem:**
- Powerful plugin model preserved
- Official plugins not hobbled
- Third-party plugins supported
- Clear security boundaries

### Drawbacks / Risks

**Developer Friction:**
- Manifest complexity
- Capability declarations required
- Testing sandbox constraints
- Permission rejections during development

**User Approval Fatigue:**
- Many permission prompts
- Users may auto-approve without reading
- Updates requiring new permissions interrupt flow

**Enforcement Overhead:**
- Every API call checks capabilities
- Audit logging I/O overhead
- Resource monitoring overhead

**Ecosystem Fragmentation:**
- Plugins may request excessive permissions
- Users hesitant to install plugins
- Trusted publisher ecosystem needed

---

## Consequences

### Positive

**Enhanced Security:**
- Malicious plugins contained effectively
- Least privilege enforced
- Audit trail complete
- User sovereignty preserved

**User Trust:**
- Clear visibility into plugin behavior
- Informed consent for permissions
- Ability to revoke access
- Signature verification available

**Ecosystem Health:**
- Encourages well-scoped plugins
- Reputation system possible (verified publishers)
- Clear security expectations
- Plugin marketplace viability

**Debugging:**
- Audit logs aid troubleshooting
- Resource usage visible
- Permission denials logged
- Clear error messages

### Negative

**Developer Experience:**
- Manifest complexity
- Capability declaration burden
- Testing sandbox behavior
- Permission errors during development

**User Experience:**
- Permission prompts interrupt flow
- Updates may request new permissions
- Complexity for non-technical users

**Performance:**
- Capability check overhead (~microseconds/call)
- Audit logging I/O
- Resource monitoring overhead

### Neutral / Considerations

**Official vs Third-Party Plugins:**
- Official plugins may ship with elevated permissions
- Clear distinction needed
- Trust establishment required
- Community verification process

**Plugin Marketplace:**
- Signature verification required
- Reputation system valuable
- Curation vs openness balance
- Discoverability challenges

**Permission Evolution:**
- Plugins may need new permissions over time
- Update approval flow essential
- Backward compatibility considerations

**Enforcement Gaps:**
- Native code can bypass sandbox
- Malicious plugins could exploit bugs
- Runtime verification essential
- Security audits needed

---

## Implementation Notes

### Core Components

**Plugin Manager (`dPKMS/plugins/`):**
```go
type PluginManager struct {
    plugins       map[string]*LoadedPlugin
    sandboxes     map[string]*PluginSandbox
    auditLog      AuditLogger
    resourceMon   ResourceMonitor
}

type LoadedPlugin struct {
    Manifest     PluginManifest
    Instance     plugin.Plugin
    Capabilities Capabilities
    Sandbox      *PluginSandbox
    Enabled      bool
}

func (pm *PluginManager) InstallPlugin(path string) error
func (pm *PluginManager) LoadPlugin(name string) error
func (pm *PluginManager) UninstallPlugin(name string) error
```

**Plugin Sandbox (`dPKMS/plugins/sandbox/`):**
```go
type PluginSandbox struct {
    PluginID   string
    Filesystem *FilesystemSandbox
    Network    *NetworkSandbox
    Storage    *StorageSandbox
    Exec       *ExecSandbox
    Resources  *ResourceMonitor
}

func (ps *PluginSandbox) CheckOperation(op Operation) error
```

**Audit Logger (`dPKMS/audit/`):**
```go
type AuditLogger interface {
    LogPluginOperation(
        pluginID string,
        operation string,
        resource string,
        result string,
        err error,
    )
    QueryAuditLog(filter AuditFilter) ([]AuditEntry, error)
}
```

### Storage Schema
```sql
-- Plugin metadata
CREATE TABLE plugins (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    version TEXT NOT NULL,
    author TEXT,
    installed_at TIMESTAMP NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    manifest JSONB NOT NULL,
    signature_verified BOOLEAN DEFAULT FALSE,
    publisher_key_id TEXT
);

-- Plugin capabilities (extracted from manifest)
CREATE TABLE plugin_capabilities (
    id UUID PRIMARY KEY,
    plugin_id UUID NOT NULL,
    category TEXT NOT NULL,  -- filesystem, network, storage, etc.
    resource TEXT NOT NULL,
    permission TEXT NOT NULL,
    reason TEXT,
    approved_at TIMESTAMP NOT NULL,
    approved_by TEXT,
    FOREIGN KEY(plugin_id) REFERENCES plugins(id)
);

CREATE INDEX idx_capabilities_plugin ON plugin_capabilities(plugin_id);
```

### Integration Points

**With Plugin System:**
1. Manifest parsing and validation
2. Capability extraction
3. User approval workflow
4. Runtime sandbox injection

**With Storage Layer:**
1. Plugin-namespaced KV store
2. Storage pattern matching
3. Quota enforcement
4. Isolated plugin directories

**With Job Queue:**
1. Job attribution to plugin
2. Resource quota enforcement
3. Concurrent job limits
4. Job audit trail

**With API Layer:**
1. Scope validation for plugin API calls
2. Rate limiting per plugin
3. Authentication context propagation

### Migration Strategy

**Phase 1: Capability Framework (Skeleton 7)**
- Capability schema and manifest parsing
- User approval workflow (CLI prompts)
- Basic audit logging
- Filesystem sandbox

**Phase 2: Network and Storage Sandboxing (Skeleton 8)**
- Network sandbox implementation
- Storage pattern enforcement
- Resource quota system
- Audit log queries

**Phase 3: Advanced Sandboxing (Skeleton 9)**
- Process execution sandbox
- Plugin signature verification
- Trusted publisher system
- Enhanced audit analytics

**Phase 4: Ecosystem Features (Skeleton 10+)**
- Plugin marketplace integration
- Reputation system
- Automated security scanning
- Community verification

**Backward Compatibility:**
- Existing plugins without manifests rejected
- Migration guide for plugin developers
- Grace period for manifest adoption
- Official plugins updated first

### Testing Requirements

**Unit Tests:**
- Capability matching logic
- Sandbox enforcement rules
- Resource quota tracking
- Audit log generation

**Integration Tests:**
- Plugin installation workflow
- Permission prompt flows
- Runtime capability enforcement
- Audit trail completeness

**Security Tests:**
- Capability bypass attempts
- Path traversal prevention
- Shell injection prevention
- Resource exhaustion attacks

**Performance Tests:**
- Capability check overhead
- Audit logging throughput
- Resource monitoring overhead
- Concurrent plugin execution

---

## References

- **architecture.md:1285-1292** – Plugin sandboxing overview
- **plugins/plugins.md:54-57** – Isolation principle
- **dpkms/security.md** – Security model
- ADR-012 – Plugins Extend Any Layer (plugin architecture)
- ADR-023 – Authentication and Authorization (capability model)
- ADR-024 – Self-Authenticating Knowledge (plugin signatures)

**Related Documents:**
- `dPKMS/plugins/sandbox/` – Sandbox implementation (to be created)
- `dPKMS/plugins/manifest.md` – Manifest specification (to be created)
- `dPKMS/audit/` – Audit logging (to be created)
- `docs/plugins/developer-guide.md` – Plugin development guide

**External References:**
- Linux Capabilities: https://man7.org/linux/man-pages/man7/capabilities.7.html
- Go Plugin Package: https://pkg.go.dev/plugin
- WASM Security Model: https://webassembly.github.io/spec/core/intro/introduction.html#security

---
