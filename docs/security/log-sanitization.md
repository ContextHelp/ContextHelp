# Log Sanitization

Automatic redaction of sensitive information from log output.

---

## Overview

Log sanitization automatically redacts sensitive information from all log output, preventing accidental exposure in:

- Console output
- Log files
- Error messages
- Debug traces
- Stack traces
- API response bodies

---

## How Log Sanitization Works

1. **Detection**: Before logging, scan content for secret patterns
2. **Redaction**: Match secrets and replace with placeholders
3. **Context Preservation**: Preserve non-sensitive content for debugging
4. **Logging**: Output sanitized content safely

```go
func SanitizeLog(message string) string {
    for _, pattern := range secretPatterns {
        message = pattern.Redact(message)
    }
    return message
}
```

---

## Configuration

### Enable Sanitization

```yaml
# config.yaml
security:
  logging:
    sanitize: true
    redaction_mode: full           # full, partial, hash
    include_context: true
    preserve_path_structure: true
```

Environment variables:

```bash
# Enable log sanitization
SECURITY_LOGGING_SANITIZE=true

# Choose redaction mode
SECURITY_LOGGING_REDACTION_MODE=full

# Include context
SECURITY_LOGGING_INCLUDE_CONTEXT=true
```

---

## Redaction Modes

### Full Redaction (Recommended)

Replaces entire secret with `<REDACTED>`:

```
[INFO] Connecting to postgres://user:password@host/db
[INFO] Connecting to postgres://<REDACTED>@host/db
```

Configuration:

```yaml
security:
  logging:
    redaction_mode: full
```

### Partial Redaction

Shows beginning and end:

```
[INFO] API key: sk-proj-1234567890abcdef...wxyz
[INFO] API key: sk-proj-****...****
```

Configuration:

```yaml
security:
  logging:
    redaction_mode: partial
    redaction_chars: 4  # First and last 4 chars
```

### Hash Redaction

Replaces with SHA256 hash (for matching across logs):

```
[INFO] Token: sk-proj-abcdef1234567890
[INFO] Token: <HASH:a3c5e7f9>
```

Configuration:

```yaml
security:
  logging:
    redaction_mode: hash
    hash_algorithm: sha256
    hash_length: 8  # First 8 chars of hash
```

---

## Log Levels

Configure which levels trigger sanitization:

```yaml
security:
  logging:
    sanitize_levels:
      error: true       # Sanitize errors
      warn: true        # Sanitize warnings
      info: true        # Sanitize info
      debug: false      # Don't sanitize debug (dev only)
      trace: false      # Don't sanitize trace (dev only)
```

Development override (DANGEROUS):

```bash
# Disable for debugging (dev only!)
export DEBUG_SKIP_SANITIZATION=true
```

---

## Sanitization Examples

### Example 1: Database Connection

**Before Sanitization**:
```
[INFO] Connecting to postgres://ctxt_user:SecurePass123@db.example.com:5432/contexthelp
```

**After Sanitization**:
```
[INFO] Connecting to postgres://<REDACTED>@db.example.com:5432/contexthelp
```

### Example 2: API Key in Request

**Before**:
```
[DEBUG] Calling OpenAI with key: sk-proj-abcdef1234567890
```

**After**:
```
[DEBUG] Calling OpenAI with key: <REDACTED>
```

### Example 3: JSON with Secrets

**Before**:
```json
{
  "config": {
    "api_key": "sk-proj-abc123",
    "db_password": "SecurePass456"
  }
}
```

**After**:
```json
{
  "config": {
    "api_key": "<REDACTED>",
    "db_password": "<REDACTED>"
  }
}
```

---

## Implementation

### Go Implementation

```go
type LogSanitizer struct {
    patterns []SecretPattern
    mode     RedactionMode
}

type SecretPattern struct {
    Name    string
    Regex   *regexp.Regexp
    Replace string
}

func (ls *LogSanitizer) Sanitize(message string) string {
    for _, pattern := range ls.patterns {
        switch ls.mode {
        case RedactionModeFull:
            message = pattern.Regex.ReplaceAllString(message, "<REDACTED>")
        case RedactionModePartial:
            message = ls.partialRedact(message, pattern.Regex)
        case RedactionModeHash:
            message = ls.hashRedact(message, pattern.Regex)
        }
    }
    return message
}

func (ls *LogSanitizer) partialRedact(message string, pattern *regexp.Regexp) string {
    return pattern.ReplaceAllStringFunc(message, func(match string) string {
        if len(match) <= 8 {
            return "****"
        }
        return match[:4] + "****" + match[len(match)-4:]
    })
}

func (ls *LogSanitizer) hashRedact(message string, pattern *regexp.Regexp) string {
    return pattern.ReplaceAllStringFunc(message, func(match string) string {
        hash := sha256.Sum256([]byte(match))
        return fmt.Sprintf("<HASH:%x>", hash[:4])
    })
}
```

### Logging Middleware

```go
func SanitizingLogger(logger *log.Logger, sanitizer *LogSanitizer) *log.Logger {
    return log.New(&SanitizingWriter{
        wrapped:   logger.Writer(),
        sanitizer: sanitizer,
    }, logger.Prefix(), logger.Flags())
}

type SanitizingWriter struct {
    wrapped   io.Writer
    sanitizer *LogSanitizer
}

func (sw *SanitizingWriter) Write(p []byte) (n int, err error) {
    sanitized := sw.sanitizer.Sanitize(string(p))
    return sw.wrapped.Write([]byte(sanitized))
}
```

---

## Performance Considerations

### Pattern Matching Overhead

Sanitization adds minimal overhead:
- ~10-50µs per log message (typical)
- Regex patterns compiled once at startup
- Parallel processing for large volumes

### Optimization Tips

```go
// Compile patterns once
var compiledPatterns = CompilePatterns(secretPatterns)

// Skip sanitization for known-safe content
if !mayContainSecrets(message) {
    return message  // Fast path
}

// Use buffered sanitization for high volume
bufferedSanitizer := NewBufferedSanitizer(1000)
```

---

## Testing Sanitization

### Unit Tests

```go
func TestSanitization(t *testing.T) {
    sanitizer := NewLogSanitizer(RedactionModeFull)
    
    tests := []struct {
        input    string
        expected string
    }{
        {
            "API key: sk-proj-abc123",
            "API key: <REDACTED>",
        },
        {
            "postgres://user:pass@host/db",
            "postgres://<REDACTED>@host/db",
        },
    }
    
    for _, tt := range tests {
        result := sanitizer.Sanitize(tt.input)
        assert.Equal(t, tt.expected, result)
    }
}
```

### Verify Sanitization

```bash
# Check logs for secrets
ctxt security verify-logs --show-unredacted

# Scan specific log file
ctxt security scan-logs /var/log/contexthelp/app.log
```

---

## Common Patterns Sanitized

### API Keys

- OpenAI: `sk-proj-*`
- Anthropic: `sk-ant-*`
- GitHub: `ghp_*`, `gho_*`, `github_pat_*`
- AWS: `AKIA*`
- Google: `AIza*`
- Stripe: `sk_live_*`, `sk_test_*`

### Database Credentials

- PostgreSQL: `postgres://user:password@host/db`
- MySQL: `mysql://user:password@host/db`
- MongoDB: `mongodb://user:password@host/db`
- Connection strings with embedded credentials

### Tokens and Headers

- JWT: `eyJ...`
- Bearer: `Bearer [token]`
- Authorization headers
- X-API-Key headers

### Private Keys

- RSA private keys: `-----BEGIN RSA PRIVATE KEY-----`
- PEM certificates: `-----BEGIN CERTIFICATE-----`
- SSH keys: `-----BEGIN OPENSSH PRIVATE KEY-----`

---

## Monitoring Sanitization

### Metrics

```yaml
# Prometheus metrics
contexthelp_log_sanitization_redactions_total
contexthelp_log_sanitization_patterns_matched
contexthelp_log_sanitization_duration_seconds
```

### Alerts

```yaml
# Alert if sanitization disabled
- alert: LogSanitizationDisabled
  expr: contexthelp_log_sanitization_enabled == 0
  for: 5m
  labels:
    severity: critical
```

---

## Troubleshooting

### False Positives

If legitimate content is being redacted:

```yaml
security:
  logging:
    exclusions:
      - pattern: "test_api_key_example"
      - pattern: "mock_password_123"
```

### Missing Redaction

If secrets are not being redacted:

1. Check sanitization enabled:
   ```bash
   ctxt config get security.logging.sanitize
   ```

2. Verify pattern matches:
   ```bash
   ctxt security test-pattern "sk-proj-abc123"
   ```

3. Check log level configuration:
   ```bash
   ctxt config get security.logging.sanitize_levels
   ```

---

## Related Documentation

- [validation.md](validation.md) — Input validation and secret patterns
- [secret-management.md](secret-management.md) — Secret lifecycle
- [model/controls.md](model/controls.md) — Security controls
- [secrets-validation-and-log-sanitization.md](secrets-validation-and-log-sanitization.md) — Comprehensive guide

---

**Last Updated**: 2026-01-26
**Version**: 1.0
**Status**: Active
**Maintained by**: Security Team
