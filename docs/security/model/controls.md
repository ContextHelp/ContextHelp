# Security Controls

Security controls implementation in ContextHelp.

---

## Overview

ContextHelp implements comprehensive security controls across multiple layers:

- **Authentication & Authorization** — Identity verification and access control
- **Encryption** — Data protection at rest and in transit
- **Data Validation** — Input sanitization and schema enforcement
- **Process Isolation** — Plugin and pipeline sandboxing
- **Audit Logging** — Security event tracking

---

## Authentication & Authorization

### REST API Authentication

**JWT Token Authentication**:

```go
type JWTClaims struct {
    UserID    string   `json:"user_id"`
    Email     string   `json:"email"`
    Scopes    []string `json:"scopes"`
    ExpiresAt int64    `json:"exp"`
}

func ValidateJWT(tokenString string) (*JWTClaims, error) {
    token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
        return []byte(os.Getenv("JWT_SECRET")), nil
    })
    
    if err != nil {
        return nil, err
    }
    
    if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
        return claims, nil
    }
    
    return nil, fmt.Errorf("invalid token")
}
```

**Configuration**:

```yaml
security:
  authentication:
    enabled: true
    jwt:
      secret: ${JWT_SECRET}
      expiration: 24h
      issuer: "contexthelp"
```

**Session-Based Authentication**:

```go
type Session struct {
    ID        string
    UserID    string
    CreatedAt time.Time
    ExpiresAt time.Time
}

func CreateSession(userID string) (*Session, error) {
    session := &Session{
        ID:        generateSecureID(),
        UserID:    userID,
        CreatedAt: time.Now(),
        ExpiresAt: time.Now().Add(24 * time.Hour),
    }
    
    return session, sessionStore.Save(session)
}
```

### gRPC API Authentication

**Per-Connection Authentication**:

```go
func (s *Server) Authenticate(ctx context.Context) error {
    md, ok := metadata.FromIncomingContext(ctx)
    if !ok {
        return status.Error(codes.Unauthenticated, "missing metadata")
    }
    
    tokens := md.Get("authorization")
    if len(tokens) == 0 {
        return status.Error(codes.Unauthenticated, "missing token")
    }
    
    claims, err := ValidateJWT(tokens[0])
    if err != nil {
        return status.Error(codes.Unauthenticated, "invalid token")
    }
    
    // Attach claims to context
    return nil
}
```

**Stream-Level Authorization**:

```go
func (s *Server) StreamObjects(req *StreamRequest, stream pb.ObjectService_StreamObjectsServer) error {
    // Authenticate connection
    if err := s.Authenticate(stream.Context()); err != nil {
        return err
    }
    
    // Check authorization for each object
    for {
        obj := getNextObject()
        if !HasPermission(userID, "read", obj.ID) {
            continue  // Skip unauthorized objects
        }
        stream.Send(obj)
    }
}
```

### CLI Authentication

**Local User Authentication**:

```go
func ValidateLocalUser() error {
    // Check process owner matches file owner
    configOwner := getFileOwner(configPath)
    currentUser := os.Getuid()
    
    if configOwner != currentUser {
        return fmt.Errorf("configuration file not owned by current user")
    }
    
    return nil
}
```

**No Network Authentication Needed**:

```bash
# CLI runs locally, authenticates via file permissions
ctxt analyze "input"  # No token needed
```

---

## Encryption

### At Rest Encryption

**AES-256-GCM Encryption**:

```go
func Encrypt(plaintext []byte, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }
    
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    
    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, err
    }
    
    ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
    return ciphertext, nil
}
```

**Key Derivation (PBKDF2)**:

```go
func DeriveKey(passphrase string, salt []byte) []byte {
    return pbkdf2.Key([]byte(passphrase), salt, 100000, 32, sha256.New)
}
```

**Key Derivation (Argon2)**:

```go
func DeriveKeyArgon2(passphrase string, salt []byte) []byte {
    return argon2.IDKey([]byte(passphrase), salt, 1, 64*1024, 4, 32)
}
```

**Configuration**:

```yaml
security:
  encryption:
    enabled: true
    key_derivation: argon2  # or pbkdf2
    # Passphrase from environment
```

**Transparent Encryption/Decryption**:

```go
type EncryptedStorage struct {
    backend Storage
    cipher  *Cipher
}

func (es *EncryptedStorage) Save(obj Object) error {
    // Encrypt before saving
    encrypted, err := es.cipher.Encrypt(obj.Data)
    if err != nil {
        return err
    }
    obj.Data = encrypted
    return es.backend.Save(obj)
}

func (es *EncryptedStorage) Load(id string) (Object, error) {
    obj, err := es.backend.Load(id)
    if err != nil {
        return Object{}, err
    }
    
    // Decrypt after loading
    decrypted, err := es.cipher.Decrypt(obj.Data)
    if err != nil {
        return Object{}, err
    }
    obj.Data = decrypted
    return obj, nil
}
```

### In Transit Encryption

**Mandatory TLS 1.2+ for External Connections**:

```go
func NewTLSConfig() *tls.Config {
    return &tls.Config{
        MinVersion: tls.VersionTLS12,
        CipherSuites: []uint16{
            tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
            tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
            tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
            tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
        },
        PreferServerCipherSuites: true,
    }
}
```

**Certificate Validation**:

```go
func ValidateCertificate(conn *tls.Conn) error {
    state := conn.ConnectionState()
    
    // Check certificate validity
    for _, cert := range state.PeerCertificates {
        if time.Now().After(cert.NotAfter) {
            return fmt.Errorf("certificate expired")
        }
        
        if time.Now().Before(cert.NotBefore) {
            return fmt.Errorf("certificate not yet valid")
        }
    }
    
    return nil
}
```

**HSTS Headers for HTTP APIs**:

```go
func HSTSMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
        next.ServeHTTP(w, r)
    })
}
```

---

## Data Validation

### Input Validation

**JSON Schema Validation**:

```go
type Validator struct {
    schemas map[string]*jsonschema.Schema
}

func (v *Validator) Validate(input interface{}, schemaName string) error {
    schema := v.schemas[schemaName]
    if schema == nil {
        return fmt.Errorf("unknown schema: %s", schemaName)
    }
    
    return schema.Validate(input)
}
```

**Size Limits**:

```go
const (
    MaxRequestSize  = 10 * MB
    MaxQueryLength  = 1000
    MaxResultLimit  = 100
    MaxBatchSize    = 50
)

func ValidateSize(data []byte) error {
    if len(data) > MaxRequestSize {
        return fmt.Errorf("request too large (max %d bytes)", MaxRequestSize)
    }
    return nil
}
```

**Type Checking**:

```go
type SearchRequest struct {
    Query  string `json:"query" validate:"required,max=1000"`
    Limit  int    `json:"limit" validate:"min=1,max=100"`
    Offset int    `json:"offset" validate:"min=0"`
    SortBy string `json:"sort_by" validate:"oneof=relevance date title"`
}

func ValidateRequest(req *SearchRequest) error {
    validate := validator.New()
    return validate.Struct(req)
}
```

**Whitelist-Based Validation**:

```go
var AllowedSortFields = []string{"relevance", "date", "title", "size"}

func ValidateSortField(field string) error {
    for _, allowed := range AllowedSortFields {
        if field == allowed {
            return nil
        }
    }
    return fmt.Errorf("invalid sort field: %s", field)
}
```

### Configuration Validation

**YAML/JSON Schema Validation**:

```go
func ValidateConfig(config *Config) error {
    // Type checking
    if config.Storage.Type != "sqlite" && config.Storage.Type != "postgres" {
        return fmt.Errorf("invalid storage type: %s", config.Storage.Type)
    }
    
    // Enum validation
    if config.Logging.Level != "debug" && config.Logging.Level != "info" &&
       config.Logging.Level != "warn" && config.Logging.Level != "error" {
        return fmt.Errorf("invalid log level: %s", config.Logging.Level)
    }
    
    // URL format validation
    for _, registry := range config.Registries {
        if _, err := url.Parse(registry.URL); err != nil {
            return fmt.Errorf("invalid registry URL: %s", registry.URL)
        }
    }
    
    return nil
}
```

**Path Traversal Prevention**:

```go
func ValidatePath(path string, baseDir string) error {
    cleanPath := filepath.Clean(path)
    absPath, err := filepath.Abs(cleanPath)
    if err != nil {
        return err
    }
    
    absBase, err := filepath.Abs(baseDir)
    if err != nil {
        return err
    }
    
    if !strings.HasPrefix(absPath, absBase) {
        return fmt.Errorf("path traversal detected: %s", path)
    }
    
    return nil
}
```

---

## Process Isolation

### Plugin Isolation

**Memory Limits**:

```go
func SetMemoryLimit(limit int64) error {
    return syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{
        Cur: uint64(limit),
        Max: uint64(limit),
    })
}

// Default: 256 MB
SetMemoryLimit(256 * 1024 * 1024)
```

**CPU Time Limits**:

```go
func ExecuteWithTimeout(ctx context.Context, fn func() error, timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    
    errChan := make(chan error, 1)
    go func() {
        errChan <- fn()
    }()
    
    select {
    case err := <-errChan:
        return err
    case <-ctx.Done():
        return fmt.Errorf("execution timeout")
    }
}

// Default: 5 minutes
ExecuteWithTimeout(ctx, pluginProcess, 5*time.Minute)
```

**File System Restrictions**:

```go
func RestrictFileAccess(pluginDir string) error {
    // Chroot to plugin directory
    if err := syscall.Chroot(pluginDir); err != nil {
        return err
    }
    
    // Change to root of chroot
    return os.Chdir("/")
}
```

**Network Access Restrictions**:

```go
type NetworkPolicy struct {
    Allowed []string  // Whitelisted domains
}

func (np *NetworkPolicy) CheckAccess(destination string) error {
    for _, allowed := range np.Allowed {
        if strings.HasSuffix(destination, allowed) {
            return nil
        }
    }
    return fmt.Errorf("network access denied: %s", destination)
}
```

### Pipeline Isolation

**Separate Execution Context**:

```go
type PipelineContext struct {
    ID        string
    Input     interface{}
    Output    interface{}
    Timeout   time.Duration
    Memory    int64
    Variables map[string]interface{}
}

func (pc *PipelineContext) Execute(pipeline *Pipeline) error {
    // Set resource limits
    SetMemoryLimit(pc.Memory)
    
    // Execute with timeout
    ctx, cancel := context.WithTimeout(context.Background(), pc.Timeout)
    defer cancel()
    
    return pipeline.Run(ctx, pc.Input)
}
```

**No Cross-Pipeline Data Access**:

```go
func (ps *PipelineService) GetData(pipelineID string, key string) (interface{}, error) {
    // Each pipeline has isolated storage
    storage := ps.storages[pipelineID]
    if storage == nil {
        return nil, fmt.Errorf("pipeline storage not found")
    }
    
    return storage.Get(key)
}
```

---

## Audit Logging

### Events Logged

```go
type AuditEvent struct {
    Timestamp  time.Time
    EventType  string
    UserID     string
    ResourceID string
    Action     string
    Result     string  // success, failure, denied
    Details    map[string]interface{}
}

const (
    EventAuthAttempt       = "auth.attempt"
    EventAuthSuccess       = "auth.success"
    EventAuthFailure       = "auth.failure"
    EventPermissionCheck   = "permission.check"
    EventPermissionDenied  = "permission.denied"
    EventConfigChange      = "config.change"
    EventPluginInstall     = "plugin.install"
    EventPluginRemove      = "plugin.remove"
    EventRegistryAccess    = "registry.access"
    EventSecretDetected    = "secret.detected"
    EventDataAccess        = "data.access"
)
```

### Log Protection

**Automatic Sanitization**:

```go
func AuditLog(event *AuditEvent) {
    // Sanitize before logging
    sanitized := SanitizeAuditEvent(event)
    
    // Structured logging
    log.Info("audit",
        "event_type", sanitized.EventType,
        "user_id", sanitized.UserID,
        "action", sanitized.Action,
        "result", sanitized.Result,
        "details", sanitized.Details,
    )
}

func SanitizeAuditEvent(event *AuditEvent) *AuditEvent {
    sanitized := *event
    
    // Remove sensitive fields
    for key := range sanitized.Details {
        if IsSensitiveField(key) {
            sanitized.Details[key] = "<REDACTED>"
        }
    }
    
    return &sanitized
}
```

**Immutable Log Appending**:

```go
func AppendAuditLog(event *AuditEvent) error {
    // Open in append-only mode
    f, err := os.OpenFile(auditLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
    if err != nil {
        return err
    }
    defer f.Close()
    
    // Write JSON line
    encoder := json.NewEncoder(f)
    return encoder.Encode(event)
}
```

**Syslog Integration**:

```go
func SendToSyslog(event *AuditEvent) error {
    syslogWriter, err := syslog.New(syslog.LOG_INFO|syslog.LOG_LOCAL0, "contexthelp")
    if err != nil {
        return err
    }
    defer syslogWriter.Close()
    
    message := fmt.Sprintf("event=%s user=%s action=%s result=%s",
        event.EventType, event.UserID, event.Action, event.Result)
    
    return syslogWriter.Info(message)
}
```

---

## Deployment Security

### Local Development

```bash
# Secure defaults
export ENV=development
export LOG_LEVEL=debug
export ENCRYPTION_ENABLED=false  # OK for local dev
export API_HOST=localhost
export API_PORT=7700
```

### Server Deployment

```bash
# Production requirements
export ENV=production
export ENCRYPTION_ENABLED=true
export ENCRYPTION_PASSPHRASE=$(vault kv get -field=passphrase secret/encryption)
export API_HOST=0.0.0.0
export API_PORT=8080
export TLS_ENABLED=true
export TLS_CERT_FILE=/etc/contexthelp/tls/server.crt
export TLS_KEY_FILE=/etc/contexthelp/tls/server.key
```

### Container Security

**Dockerfile Security**:

```dockerfile
FROM alpine:latest

# Non-root user
RUN addgroup -S contexthelp && adduser -S contexthelp -G contexthelp

# Copy binary
COPY --chown=contexthelp:contexthelp bin/dpkms /usr/local/bin/dpkms

# Run as non-root
USER contexthelp

# Health check
HEALTHCHECK --interval=30s --timeout=3s \
  CMD wget --quiet --tries=1 --spider http://localhost:7700/health || exit 1

ENTRYPOINT ["/usr/local/bin/dpkms"]
CMD ["serve"]
```

**Kubernetes Security**:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: contexthelp
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    fsGroup: 1000
  containers:
  - name: contexthelp
    image: contexthelp:latest
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop:
        - ALL
    resources:
      limits:
        memory: "512Mi"
        cpu: "500m"
```

---

## Security Testing

### Unit Tests

```go
func TestEncryption(t *testing.T) {
    key := make([]byte, 32)
    rand.Read(key)
    
    plaintext := []byte("sensitive data")
    ciphertext, err := Encrypt(plaintext, key)
    require.NoError(t, err)
    
    decrypted, err := Decrypt(ciphertext, key)
    require.NoError(t, err)
    require.Equal(t, plaintext, decrypted)
}
```

### Integration Tests

```go
func TestDatabaseEncryption(t *testing.T) {
    storage := NewEncryptedStorage(sqliteBackend, cipher)
    
    obj := Object{ID: "test", Data: []byte("secret")}
    err := storage.Save(obj)
    require.NoError(t, err)
    
    // Verify encrypted in database
    rawData := sqliteBackend.LoadRaw(obj.ID)
    require.NotEqual(t, obj.Data, rawData)
    
    // Verify decrypted on load
    loaded, err := storage.Load(obj.ID)
    require.NoError(t, err)
    require.Equal(t, obj.Data, loaded.Data)
}
```

---

## Related Documentation

- [threat.md](threat.md) — Threat model
- [boundaries.md](boundaries.md) — Security boundaries
- [compliance.md](compliance.md) — Compliance standards
- [../secret-management.md](../secret-management.md) — Secret lifecycle

---

**Last Updated**: 2026-01-26
**Version**: 1.0
**Status**: Active
**Maintained by**: Security Team
