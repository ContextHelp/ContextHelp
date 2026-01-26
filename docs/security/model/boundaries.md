# Security Boundaries

Trust boundaries and isolation layers in ContextHelp.

---

## Overview

ContextHelp implements **five security boundaries** with different trust levels and protection mechanisms. Data crossing these boundaries is subject to strict validation and access controls.

---

## Boundary 1: Local Machine

**Trust Level**: Medium (Owner trusted, machine not fully trusted)

### Components
- CLI application
- Local storage (SQLite, JSON)
- Configuration files
- Log files

### Threat Model

The local machine is partially trusted:
- **Trusted**: The user running ContextHelp
- **Not Trusted**: Other processes, other users, malware

### Protection Mechanisms

#### File Permission Validation

```bash
# Sensitive files
~/.config/contexthelp/config.yaml          # 0600 (rw-------)
~/.local/share/contexthelp/contexthelp.db  # 0600 (rw-------)
~/.config/contexthelp/secrets/*            # 0600 (rw-------)

# Directories
~/.config/contexthelp/                     # 0700 (rwx------)
~/.local/share/contexthelp/                # 0700 (rwx------)
```

ContextHelp validates permissions on startup:

```go
func ValidateFilePermissions(path string) error {
    info, err := os.Stat(path)
    if err != nil {
        return err
    }
    
    mode := info.Mode().Perm()
    if mode != 0600 && mode != 0700 {
        return fmt.Errorf("insecure permissions: %o (should be 0600 or 0700)", mode)
    }
    
    return nil
}
```

#### User Ownership Checks

```go
func ValidateOwnership(path string) error {
    info, err := os.Stat(path)
    if err != nil {
        return err
    }
    
    stat := info.Sys().(*syscall.Stat_t)
    if stat.Uid != uint32(os.Getuid()) {
        return fmt.Errorf("file not owned by current user")
    }
    
    return nil
}
```

#### Encryption at Rest (Optional)

```yaml
security:
  encryption:
    enabled: true
    key_derivation: argon2
    # Passphrase from environment
```

Protects against:
- Disk theft
- Unauthorized file access
- Filesystem-level attacks

#### Log Sanitization

All log output automatically sanitized:

```go
log.Info("Processing request",
    "api_key", apiKey,           // Redacted
    "password", dbPassword)      // Redacted

// Output:
// "Processing request api_key=<REDACTED> password=<REDACTED>"
```

---

## Boundary 2: External Registries

**Trust Level**: Low (Completely untrusted)

### Components
- Registry endpoints (HTTP/HTTPS)
- Registry responses (taxonomy, pipelines)
- Registry authentication credentials
- Registry metadata

### Threat Model

External registries are **completely untrusted**:
- Registry server could be compromised
- Network traffic could be intercepted (MitM)
- Responses could contain malicious data

### Protection Mechanisms

#### Schema Validation

All registry responses validated against strict schemas:

```go
type RegistryResponse struct {
    Version    string      `json:"version" validate:"required,semver"`
    Pipelines  []Pipeline  `json:"pipelines" validate:"dive"`
    Taxonomies []Taxonomy  `json:"taxonomies" validate:"dive"`
}

type Pipeline struct {
    Name        string `json:"name" validate:"required,alphanum"`
    Description string `json:"description" validate:"max=500"`
    Steps       []Step `json:"steps" validate:"required,dive"`
}

type Step struct {
    Type   string                 `json:"type" validate:"oneof=parse extract transform"`
    Config map[string]interface{} `json:"config" validate:"required"`
}
```

#### Type Checking

No direct use of registry data without transformation:

```go
// BAD - Direct use
pipeline := registryResponse.Pipelines[0]
execute(pipeline)  // Dangerous

// GOOD - Validated transformation
pipeline, err := ValidateAndTransform(registryResponse.Pipelines[0])
if err != nil {
    return fmt.Errorf("invalid pipeline: %w", err)
}
execute(pipeline)
```

#### TLS Enforcement

```go
func FetchRegistry(url string) (*RegistryResponse, error) {
    // Enforce HTTPS
    if !strings.HasPrefix(url, "https://") {
        return nil, fmt.Errorf("registry must use HTTPS")
    }
    
    // Strict TLS configuration
    transport := &http.Transport{
        TLSClientConfig: &tls.Config{
            MinVersion: tls.VersionTLS12,
            CipherSuites: []uint16{
                tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
                tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
            },
        },
    }
    
    client := &http.Client{Transport: transport}
    // ... fetch and validate
}
```

#### Version Pinning Support

```yaml
registries:
  - name: trusted-registry
    url: https://registry.example.com
    version: "1.2.3"  # Pin to specific version
    verify_checksum: true
```

#### Signature Verification (Future)

```go
// Future enhancement
func VerifySignature(response []byte, signature []byte, publicKey []byte) error {
    // Cryptographic signature verification
    return verifyEd25519Signature(response, signature, publicKey)
}
```

---

## Boundary 3: AI Providers

**Trust Level**: Medium (Trusted service, but external)

### Components
- API endpoints (OpenAI, Anthropic, Ollama)
- API requests and responses
- Model outputs
- API credentials

### Threat Model

AI providers are trusted but external:
- Provider service is trusted (OpenAI, Anthropic)
- Network path to provider is not trusted
- Provider responses should be validated

### Protection Mechanisms

#### Explicit Opt-In Configuration

No AI provider enabled by default:

```yaml
# User must explicitly configure
providers:
  openai:
    api_key: ${OPENAI_API_KEY}  # Required
    enabled: true               # Explicit opt-in
```

#### TLS-Only Communication

```go
func CallOpenAI(prompt string) (string, error) {
    client := &http.Client{
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{
                MinVersion: tls.VersionTLS12,
            },
        },
    }
    
    // All requests over HTTPS
    req, _ := http.NewRequest("POST", "https://api.openai.com/v1/completions", ...)
    // ...
}
```

#### Credential Storage Security

```yaml
# GOOD - From environment
providers:
  openai:
    api_key: ${OPENAI_API_KEY}

# GOOD - From secret file
providers:
  openai:
    api_key_file: /etc/contexthelp/secrets/openai-key

# BAD - Hardcoded (rejected by validation)
providers:
  openai:
    api_key: "sk-proj-abc123"  # Validation error
```

#### Request/Response Sanitization

```go
// Sanitize before logging
log.Info("AI request",
    "provider", "openai",
    "model", model,
    "prompt_length", len(prompt),
    // NO api_key, NO full prompt
)

log.Info("AI response",
    "provider", "openai",
    "tokens", response.Usage.TotalTokens,
    // NO full response (may contain sensitive data)
)
```

#### Rate Limiting

```go
type RateLimiter struct {
    requests map[string]*rate.Limiter
}

func (rl *RateLimiter) Allow(provider string) bool {
    limiter := rl.requests[provider]
    if limiter == nil {
        // 10 requests per second
        limiter = rate.NewLimiter(rate.Limit(10), 20)
        rl.requests[provider] = limiter
    }
    return limiter.Allow()
}
```

#### Fallback to Local Models

```yaml
providers:
  # Primary: OpenAI
  openai:
    api_key: ${OPENAI_API_KEY}
    priority: 1
  
  # Fallback: Local Ollama
  ollama:
    host: http://localhost:11434
    model: mistral
    priority: 2  # Used if OpenAI fails
```

---

## Boundary 4: Plugins

**Trust Level**: Low (Untrusted code)

### Components
- Plugin code
- Plugin configuration
- Plugin data access
- Plugin network access

### Threat Model

Plugins are **completely untrusted**:
- Plugin code could be malicious
- Plugins should not access system resources
- Plugins should not exfiltrate data

### Protection Mechanisms

#### Capability-Based Permissions

```json
{
  "name": "my-plugin",
  "version": "1.0.0",
  "capabilities": [
    "read:objects",
    "write:tags",
    "network:https://api.example.com"
  ]
}
```

Capability enforcement:

```go
func (s *Sandbox) CheckCapability(plugin *Plugin, capability string) error {
    for _, allowed := range plugin.Manifest.Capabilities {
        if allowed == capability || allowed == "*" {
            return nil
        }
    }
    return fmt.Errorf("capability %q not granted to plugin %q", capability, plugin.Name)
}
```

#### Sandbox Execution Environment

```go
type Sandbox struct {
    MemoryLimit   int64         // 256 MB default
    CPULimit      time.Duration // 5 minutes default
    NetworkAccess []string      // Whitelisted domains
    FileAccess    []string      // Restricted to plugin directory
}

func (s *Sandbox) Execute(plugin *Plugin, input interface{}) (interface{}, error) {
    // Set resource limits
    setRlimit(RLIMIT_AS, s.MemoryLimit)
    setRlimit(RLIMIT_CPU, s.CPULimit)
    
    // Restrict file system
    chroot(fmt.Sprintf("/var/lib/contexthelp/plugins/%s", plugin.Name))
    
    // Execute with timeout
    ctx, cancel := context.WithTimeout(context.Background(), s.CPULimit)
    defer cancel()
    
    return plugin.Process(ctx, input)
}
```

#### Memory and CPU Limits

```yaml
plugins:
  - name: text-analyzer
    limits:
      memory: 256MB
      cpu_time: 5m
      disk_read: 10MB/s
      disk_write: 1MB/s
```

#### File System Restrictions

```go
// Plugin can only access own directory
pluginDir := filepath.Join(PluginBaseDir, plugin.Name)

// Attempted path traversal blocked
requestedPath := pluginDir + userInput  // user provides "../../etc/passwd"
cleanPath := filepath.Clean(requestedPath)
if !strings.HasPrefix(cleanPath, pluginDir) {
    return fmt.Errorf("access denied: path outside plugin directory")
}
```

#### Network Access Whitelisting

```go
type NetworkPolicy struct {
    AllowedDomains []string
    AllowedPorts   []int
}

func (np *NetworkPolicy) Allow(destination string) bool {
    for _, domain := range np.AllowedDomains {
        if strings.HasSuffix(destination, domain) {
            return true
        }
    }
    return false
}
```

#### Data Access Scoping

```go
// Plugin can only access objects it created
func (s *Sandbox) GetObjects(plugin *Plugin) ([]Object, error) {
    return db.Query(`
        SELECT * FROM objects 
        WHERE created_by = ? 
        OR visibility = 'public'
    `, plugin.Name)
}
```

---

## Boundary 5: REST/gRPC APIs

**Trust Level**: High (Localhost), Low (Remote)

### Components
- HTTP/gRPC endpoints
- API requests/responses
- Authentication tokens
- TLS certificates

### Threat Model

API trust varies by deployment:
- **Localhost binding**: High trust (same machine)
- **Remote binding**: Low trust (network access)

### Protection Mechanisms

#### Localhost Binding by Default

```yaml
server:
  http:
    host: localhost  # 127.0.0.1 only
    port: 7700
  grpc:
    host: localhost  # 127.0.0.1 only
    port: 7701
```

#### Optional TLS Termination

```yaml
server:
  http:
    host: 0.0.0.0      # Bind to all interfaces
    port: 8080
    tls:
      enabled: true
      cert_file: /etc/contexthelp/tls/server.crt
      key_file: /etc/contexthelp/tls/server.key
      min_version: "1.2"
```

#### Authentication/Authorization Checks

```go
func AuthMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Extract JWT token
        token := r.Header.Get("Authorization")
        if token == "" {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        
        // Validate token
        claims, err := ValidateJWT(token)
        if err != nil {
            http.Error(w, "invalid token", http.StatusUnauthorized)
            return
        }
        
        // Check permissions
        if !HasPermission(claims.UserID, r.URL.Path, r.Method) {
            http.Error(w, "forbidden", http.StatusForbidden)
            return
        }
        
        next.ServeHTTP(w, r)
    })
}
```

#### Input Validation

```go
func ValidateSearchRequest(req *SearchRequest) error {
    if len(req.Query) > MaxQueryLength {
        return fmt.Errorf("query too long (max %d)", MaxQueryLength)
    }
    
    if req.Limit < 0 || req.Limit > MaxResultLimit {
        return fmt.Errorf("invalid limit (0-%d)", MaxResultLimit)
    }
    
    if !IsValidSortField(req.SortBy) {
        return fmt.Errorf("invalid sort field: %s", req.SortBy)
    }
    
    return nil
}
```

#### Rate Limiting

```go
func RateLimitMiddleware(limiter *rate.Limiter) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if !limiter.Allow() {
                http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

#### Output Sanitization

```go
func SearchHandler(w http.ResponseWriter, r *http.Request) {
    results, err := Search(r.Context(), req.Query)
    if err != nil {
        // Sanitize error (don't expose internal details)
        log.Error("search error", "error", err)
        http.Error(w, "search failed", http.StatusInternalServerError)
        return
    }
    
    // Sanitize results
    for i := range results {
        results[i].InternalID = ""  // Don't expose internal IDs
        results[i].CreatedBy = ""   // Don't expose user info
    }
    
    json.NewEncoder(w).Encode(results)
}
```

#### CORS Restrictions

```yaml
server:
  http:
    cors:
      allowed_origins:
        - https://app.example.com
      allowed_methods:
        - GET
        - POST
      allowed_headers:
        - Content-Type
        - Authorization
      max_age: 3600
```

---

## Boundary Crossing Summary

| From → To | Trust Change | Validation Required | Examples |
|-----------|-------------|-------------------|----------|
| Local Machine → Registry | Medium → Low | Schema, TLS | Fetching pipelines |
| Local Machine → AI Provider | Medium → Medium | TLS, rate limit | AI API calls |
| Local Machine → Plugin | Medium → Low | Sandbox, capabilities | Plugin execution |
| Plugin → Local Machine | Low → Medium | Data scope, sanitization | Plugin writes objects |
| External → API | Low → High/Low | Auth, validation, rate limit | API requests |

---

## Boundary Violation Detection

ContextHelp logs boundary violations:

```go
func LogBoundaryViolation(boundary string, details string) {
    log.Warn("boundary violation",
        "boundary", boundary,
        "details", details,
        "timestamp", time.Now())
}
```

Examples:
```
boundary violation boundary=plugin details="attempted file access outside sandbox"
boundary violation boundary=registry details="schema validation failed"
boundary violation boundary=api details="rate limit exceeded"
```

---

## Related Documentation

- [threat.md](threat.md) — Threat model
- [controls.md](controls.md) — Security controls
- [../secret-management.md](../secret-management.md) — Secret lifecycle
- [../../plugins/plugin-isolation.md](../../plugins/plugin-isolation.md) — Plugin sandboxing

---

**Last Updated**: 2026-01-26
**Version**: 1.0
**Status**: Active
**Maintained by**: Security Team
