# Secrets Validation and Log Sanitization

This document provides comprehensive guidance on secrets validation and log sanitization in ContextHelp, including how the system detects and protects sensitive information.

## Overview

Secrets validation and log sanitization are critical security features that prevent accidental exposure of sensitive information such as API keys, tokens, passwords, and database credentials. ContextHelp implements a multi-layered approach to detect potential secrets, validate them, and automatically sanitize logs to prevent leakage.

### Key Features

- **Pattern-Based Detection**: Identifies common secret patterns (API keys, tokens, passwords, etc.)
- **Contextual Analysis**: Distinguishes between actual secrets and false positives
- **Automatic Log Sanitization**: Redacts sensitive information from all log output
- **Configurable Rules**: Custom patterns and exclusions for your environment
- **Audit Trail**: Maintains sanitized logs for compliance and troubleshooting
- **Developer Feedback**: Helpful messages about detected secrets without exposing them

## Secrets Detection Patterns

ContextHelp automatically detects potential secrets matching these patterns:

### API Keys and Tokens

| Pattern | Examples | Priority |
|---------|----------|----------|
| OpenAI API keys | `sk-proj-...` (40+ chars) | High |
| Anthropic API keys | `sk-ant-...` (40+ chars) | High |
| GitHub tokens | `ghp_...` (36+ chars) | High |
| GitHub OAuth tokens | `gho_...` (36+ chars) | High |
| GitHub Personal Access Tokens | `github_pat_...` | High |
| AWS Access Keys | `AKIA` + 16 chars | High |
| AWS Secret Access Keys | Long base64-like strings | High |
| Azure credentials | `DefaultEndpointsProtocol=https;...` | High |
| Google API keys | `AIza...` (39 chars) | High |
| Stripe API keys | `sk_live_...` or `sk_test_...` | High |
| JWT tokens | Pattern: `eyJ...eyJ....*` (base64 encoded) | High |
| Bearer tokens | `Bearer [40+ alphanumeric]` | High |
| API tokens (generic) | `(api|API)[-_]?(key|token)` + value | Medium |

### Database Credentials

| Pattern | Examples | Priority |
|---------|----------|----------|
| PostgreSQL connection strings | `postgres://user:pass@host/db` | High |
| MySQL connection strings | `mysql://user:pass@host/db` | High |
| MongoDB connection strings | `mongodb://user:pass@host/db` | High |
| Password fields | `password` followed by value | High |
| Database URLs | Any URL-like string with embedded credentials | High |

### Encryption Keys and Passphrases

| Pattern | Examples | Priority |
|---------|----------|----------|
| Private keys | `-----BEGIN PRIVATE KEY-----` | Critical |
| PEM certificates | `-----BEGIN CERTIFICATE-----` | Critical |
| RSA keys | `-----BEGIN RSA PRIVATE KEY-----` | Critical |
| SSH keys | `-----BEGIN OPENSSH PRIVATE KEY-----` | Critical |
| Encryption passphrases | `passphrase` followed by value | High |
| ENCRYPTION_KEY patterns | Environment variables | High |

### Authentication Headers

| Pattern | Examples | Priority |
|---------|----------|----------|
| Authorization headers | `Authorization: [value]` | High |
| X-API-Key headers | `X-API-Key: [value]` | High |
| X-Auth-Token headers | `X-Auth-Token: [value]` | High |
| Basic auth | `Authorization: Basic [base64]` | High |

### Environment Variable Secrets

| Pattern | Examples | Priority |
|---------|----------|----------|
| Key-value secrets | `POSTGRES_PASSWORD=...` | High |
| Prefixed secrets | `CH_REGISTRY_TOKEN_*=...` | High |
| Quoted values | `"api_key": "sk_..."` | High |
| YAML nested secrets | `auth: token: sk_...` | High |

### Configuration Secrets

| Pattern | Examples | Priority |
|---------|----------|----------|
| YAML secrets | `password: ***` | High |
| JSON secrets | `"secret": "value"` | High |
| .env file secrets | `KEY=value` | High |
| INI file secrets | `key = value` | Medium |

## Detection Configuration

### Built-In Detection Rules

ContextHelp ships with detection rules for the most common secret patterns. These rules are:

- **Always active** for API keys, database credentials, and private keys
- **Highly accurate** with low false positive rates
- **Continuously updated** to match new secret formats
- **Configurable** through environment variables and configuration files

### Enabling/Disabling Detection

Detection is enabled by default. To configure:

```yaml
# config.yaml
security:
  secrets:
    enabled: true
    detection:
      enabled: true
      patterns:
        # Enable/disable specific pattern categories
        api_keys: true
        database_credentials: true
        private_keys: true
        tokens: true
        passwords: true
      severity: high  # high, medium, low
```

Via environment variables:

```bash
# Enable all detection
CH_SECURITY_SECRETS_DETECTION_ENABLED=true

# Set severity threshold (only report high/critical)
CH_SECURITY_SECRETS_DETECTION_SEVERITY=high

# Enable specific pattern categories
CH_SECURITY_SECRETS_DETECTION_API_KEYS=true
CH_SECURITY_SECRETS_DETECTION_PASSWORDS=true
```

### Custom Detection Patterns

Add custom patterns for organization-specific secrets:

```yaml
security:
  secrets:
    detection:
      custom_patterns:
        - name: internal_api_key
          pattern: "x-internal-[a-z0-9]{32}"
          case_insensitive: false
          severity: high

        - name: legacy_token
          pattern: "token_legacy_[a-zA-Z0-9]{64}"
          case_insensitive: false
          severity: medium

        - name: company_key
          pattern: "COMPANY_SECRET_[A-Z0-9]{40}"
          case_insensitive: false
          severity: critical
```

Via environment variables:

```bash
# Load custom patterns from file
CH_SECURITY_SECRETS_CUSTOM_PATTERNS_FILE=~/.config/contexthelp/secret-patterns.yaml

# Or define inline (JSON format)
CH_SECURITY_SECRETS_CUSTOM_PATTERNS='[{"name":"my_secret","pattern":"my_.*","severity":"high"}]'
```

## Log Sanitization

Log sanitization automatically redacts sensitive information from all log output, preventing accidental exposure in:

- Console output
- Log files
- Error messages
- Debug traces
- Stack traces
- API response bodies

### How Log Sanitization Works

1. **Detection**: Before logging, the system scans content for secret patterns
2. **Redaction**: Matched secrets are replaced with placeholders like `[REDACTED]`
3. **Context Preservation**: Non-sensitive content is preserved for debugging
4. **Logging**: Sanitized content is logged safely

### Log Sanitization Configuration

Enable sanitization globally:

```yaml
# config.yaml
security:
  logging:
    sanitize: true
    redaction_mode: full           # full, partial, hash
    include_context: true
    preserve_path_structure: true
```

Via environment variables:

```bash
# Enable log sanitization
CH_SECURITY_LOGGING_SANITIZE=true

# Choose redaction mode (full, partial, hash)
CH_SECURITY_LOGGING_REDACTION_MODE=full

# Include context around redacted values
CH_SECURITY_LOGGING_INCLUDE_CONTEXT=true

# Log level filters
CH_SECURITY_LOGGING_MIN_LEVEL=warn  # Only sanitize warn and above
```

### Redaction Modes

ContextHelp supports three redaction modes:

#### Full Redaction (Recommended)

Replaces entire secret with `[REDACTED]`:

```
[INFO] Connecting to postgres://user:password@host/db
[INFO] Connecting to postgres://[REDACTED]@host/db
```

Configuration:

```yaml
security:
  logging:
    redaction_mode: full
```

#### Partial Redaction

Shows beginning and end of secret:

```
[INFO] API key: sk-proj-1234567890abcdef...wxyz
[INFO] API key: sk-proj-****...****
```

Configuration:

```yaml
security:
  logging:
    redaction_mode: partial
    redaction_chars: 4  # Show first and last 4 characters
```

#### Hash Redaction

Replaces secret with SHA256 hash (for matching secrets across logs):

```
[INFO] Token: sk-proj-abcdef1234567890
[INFO] Token: [HASH:a3c5e7f9...]
```

Configuration:

```yaml
security:
  logging:
    redaction_mode: hash
    hash_algorithm: sha256
    hash_length: 8  # Show first 8 chars of hash
```

### Log Levels and Sanitization

Configure which log levels trigger sanitization:

```yaml
security:
  logging:
    sanitize_levels:
      error: true       # Sanitize error logs
      warn: true        # Sanitize warnings
      info: true        # Sanitize info
      debug: false      # Don't sanitize debug logs (dev-only)
      trace: false      # Don't sanitize trace logs (dev-only)
```

In development, you can disable sanitization for specific components:

```bash
# Disable sanitization for debugging (DANGEROUS - dev only!)
export CH_SECURITY_LOGGING_SANITIZE_LEVELS_DEBUG=false
export CH_DEBUG_SKIP_SANITIZATION=true
```

### Sanitization Examples

#### Example 1: Database Connection

Before sanitization:
```
[INFO] Database connection established: postgres://ctxt_user:SecurePass123!@db.example.com:5432/contexthelp
[DEBUG] Connection string: postgresql://ctxt_user:SecurePass123!@db.example.com:5432/contexthelp?sslmode=require
```

After sanitization (full mode):
```
[INFO] Database connection established: postgres://[REDACTED]@db.example.com:5432/contexthelp
[DEBUG] Connection string: postgresql://[REDACTED]@db.example.com:5432/contexthelp?sslmode=require
```

#### Example 2: API Key in Error

Before sanitization:
```
[ERROR] Failed to authenticate with OpenAI: Authorization header 'Bearer sk-proj-...' was rejected
[ERROR] Stack: at authenticate (openai.go:42) with key sk-proj-abc123def456
```

After sanitization (partial mode):
```
[ERROR] Failed to authenticate with OpenAI: Authorization header 'Bearer sk-proj-****...****' was rejected
[ERROR] Stack: at authenticate (openai.go:42) with key sk-proj-****...****
```

#### Example 3: Configuration Dump

Before sanitization:
```yaml
[DEBUG] Configuration loaded:
registries:
  - name: private-registry
    url: https://registry.company.com
    auth:
      token: ghp_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6
```

After sanitization:
```yaml
[DEBUG] Configuration loaded:
registries:
  - name: private-registry
    url: https://registry.company.com
    auth:
      token: [REDACTED]
```

## Handling False Positives

Some legitimate values might match secret patterns. Handle these with:

### Whitelisting Specific Values

```yaml
security:
  secrets:
    detection:
      whitelist:
        # Exact string matching
        exact_matches:
          - "example-token-that-looks-like-secret"

        # Pattern-based whitelist
        pattern_whitelist:
          - pattern: "test_.*"  # Exclude test_ prefixed values
            reason: "Test values only"
```

### Excluding Specific Fields

```yaml
security:
  secrets:
    detection:
      exclusions:
        # Don't check these file patterns
        paths:
          - "docs/**/*.md"
          - "examples/**"
          - "test/**"

        # Don't check these config keys
        config_keys:
          - "example_values"
          - "documentation"
          - "deprecated_settings"
```

### File-Level Exclusions

Add to specific files:

```go
// Allow secrets in this file (e.g., test fixtures)
// contexthelp:skip-secret-check
package fixtures

const (
    TestAPIKey = "sk-test-abc123def456"
    TestPassword = "password123"
)
```

### Using Comments for Inline Exclusions

```bash
#!/bin/bash

# Example GitHub token for documentation
# contexthelp:skip-secret-check: ghp_abc123def456

# This will be flagged
REAL_TOKEN="ghp_real_token_here"
```

## Validating Secrets in Code

### Using the Validation API

Programmatically check if strings contain secrets:

```go
package main

import (
    "fmt"
    "github.com/ideacrafterslabs/ctxt/internal/security"
)

func main() {
    validator := security.NewSecretsValidator()

    // Check a single value
    value := "sk-proj-abc123def456xyz"
    result := validator.ValidateString(value)

    if result.IsSecret {
        fmt.Printf("Detected secret: %s (severity: %s)\n",
            result.Pattern, result.Severity)
    }

    // Check with custom threshold
    result = validator.ValidateStringWithThreshold(
        value,
        security.SeverityHigh,  // Only report high+ severity
    )
}
```

### Batch Validation

```go
package main

import (
    "github.com/ideacrafterslabs/ctxt/internal/security"
)

func main() {
    validator := security.NewSecretsValidator()

    content := `
        OPENAI_API_KEY=sk-proj-abc123def456
        DATABASE_URL=postgres://user:pass@localhost/db
        DEBUG_TOKEN=test_token_123
    `

    results := validator.ValidateContent(content)

    for _, result := range results {
        if result.IsSecret {
            fmt.Printf("Line %d: %s secret detected\n",
                result.Line, result.Severity)
        }
    }
}
```

### CLI Validation Command

Validate files or directories:

```bash
# Check a single file
ctxt security validate ./config.yaml

# Check directory recursively
ctxt security validate ./config/ --recursive

# Show detailed report
ctxt security validate ./config/ --verbose

# Generate JSON report
ctxt security validate ./config/ --output json > secrets-report.json

# Fail if secrets found
ctxt security validate ./config/ --fail-on-secret
```

## Configuration File Security

### Protecting Configuration Files

Configuration files often contain secrets. Protect them:

```bash
# Secure file permissions (owner read/write only)
chmod 600 ~/.config/contexthelp/config.yaml
chmod 700 ~/.config/contexthelp/

# For system-wide config
sudo chmod 600 /etc/contexthelp/config.yaml
sudo chmod 700 /etc/contexthelp/

# Verify permissions
ls -la ~/.config/contexthelp/config.yaml
# Should show: -rw------- (600)
```

### Safe Configuration Patterns

Use environment variables for secrets, never hardcode them:

```yaml
# GOOD - Use environment variable references
registries:
  - name: private-registry
    url: https://registry.example.com
    auth:
      token: ${CH_REGISTRY_TOKEN_PRIVATE}  # From env var

# GOOD - Use file-based secrets
registries:
  - name: private-registry
    url: https://registry.example.com
    auth:
      token_file: /etc/contexthelp/secrets/registry-token  # From file
```

```bash
# BAD - Don't do this
# Hardcoding secrets in config
CH_REGISTRY_TOKEN_PRIVATE="ghp_abc123def456"  # DON'T

# GOOD - Source from secure storage
export CH_REGISTRY_TOKEN_PRIVATE=$(pass show contexthelp/registry-token)
export CH_REGISTRY_TOKEN_PRIVATE=$(aws secretsmanager get-secret-value --secret-id contexthelp/registry --query SecretString --output text)
```

### Configuration as Code

When managing configuration in code:

```go
// BAD - Hardcoded secret
cfg := &config.Config{
    Registries: []config.RegistryConfig{
        {
            Name:  "private",
            Token: "ghp_hardcoded_secret_xyz",  // DON'T
        },
    },
}

// GOOD - Load from environment
cfg := &config.Config{
    Registries: []config.RegistryConfig{
        {
            Name:  "private",
            Token: os.Getenv("CH_REGISTRY_TOKEN_PRIVATE"),
        },
    },
}

// GOOD - Load from secret file
token, err := ioutil.ReadFile("/etc/contexthelp/secrets/registry-token")
cfg := &config.Config{
    Registries: []config.RegistryConfig{
        {
            Name:  "private",
            Token: string(token),
        },
    },
}
```

## Verifying Log Sanitization

### Testing Log Output

Verify logs are properly sanitized:

```bash
# Test with a fake secret
export OPENAI_API_KEY="sk-proj-test-123456789abcdef"
ctxt analyze "test content" 2>&1 | grep -i "sk-proj"

# Should output nothing (secret is redacted)

# Check logs for redaction
tail -f ~/.local/share/contexthelp/logs/contexthelp.log | grep REDACTED
```

### Audit Log Sanitization

Enable detailed sanitization audit logging:

```yaml
security:
  logging:
    sanitize: true
    audit_redactions: true  # Log what was redacted
```

```bash
# View what was redacted
ctxt logs show --filter "REDACTED"

# Show redaction statistics
ctxt logs stats --filter "REDACTED"
```

### Log Sanitization Verification Command

```bash
# Verify logs are sanitized
ctxt security verify-logs

# Show detected secrets that were redacted
ctxt security verify-logs --show-redacted

# Export sanitization report
ctxt security verify-logs --export pdf > sanitization-report.pdf
```

## Integration Points

### For Developers Using ContextHelp APIs

When calling ContextHelp APIs, logs are automatically sanitized:

```go
package main

import (
    "github.com/ideacrafterslabs/ctxt/pkg/client"
    "github.com/ideacrafterslabs/ctxt/internal/security"
)

func setupClient() (*client.Client, error) {
    // Create sanitized logger
    logger := security.NewSanitizedLogger(
        security.LoggerConfig{
            RedactionMode: security.RedactionModeFull,
        },
    )

    // Create client with sanitized logger
    return client.NewClient(
        client.WithLogger(logger),
        client.WithSanitization(true),
    )
}
```

### For Plugin Developers

Plugins should use the sanitization utilities:

```go
package myplugin

import (
    "github.com/ideacrafterslabs/ctxt/internal/security"
)

func ProcessData(data string) error {
    // Get sanitized logger
    logger := security.GetSanitizedLogger()

    // This will automatically redact secrets
    logger.Infof("Processing data: %s", data)

    // Or manually sanitize strings
    sanitized := security.SanitizeString(data)
    return nil
}
```

### For Custom Integrations

When integrating external systems:

```go
package integration

import (
    "github.com/ideacrafterslabs/ctxt/internal/security"
)

func fetchFromRegistry(token string) error {
    // Validate token doesn't leak
    validator := security.NewSecretsValidator()
    if result := validator.ValidateString(token); result.IsSecret {
        return fmt.Errorf("token looks like a secret: %s", result.Pattern)
    }

    // Use token safely
    result, err := callRegistry(token)

    // Sanitize any response logging
    logger := security.GetSanitizedLogger()
    logger.Debugf("Response: %v", result)

    return err
}
```

## Best Practices

### For Development

1. **Use test/mock secrets**: Never use real secrets in code
   ```go
   const (
       TestToken = "sk-test-mock-token-12345"  // Obviously fake
       TestPassword = "test_password_123"       // Not real
   )
   ```

2. **Load secrets from environment**: Don't hardcode
   ```bash
   export OPENAI_API_KEY=$(pass show openai-key)
   go run main.go
   ```

3. **Use git hooks to prevent commits**: Prevent accidental secret commits
   ```bash
   # Install pre-commit hook
   cp hooks/pre-commit .git/hooks/
   chmod +x .git/hooks/pre-commit
   ```

4. **Enable debug logs safely**: Only enable in local dev
   ```bash
   # This redacts even in debug mode
   export CH_DEBUG=true
   export CH_SECURITY_LOGGING_SANITIZE_LEVELS_DEBUG=true
   ```

### For Operations

1. **Audit logs regularly**: Check for any unredacted secrets
   ```bash
   ctxt security verify-logs --daily
   ```

2. **Use log aggregation**: Ensure centralized logs are also sanitized
   ```yaml
   logging:
     outputs:
       - type: file
         sanitize: true
       - type: syslog
         sanitize: true
   ```

3. **Monitor for suspicious patterns**: Watch for repeated redactions
   ```bash
   # Alert if redactions increase unexpectedly
   ctxt logs stats --watch
   ```

4. **Rotate secrets regularly**: Follow schedule in [Secret Management](./secret-management.md#secret-rotation)

### For Compliance

1. **Maintain audit trails**: Logs show what was redacted (not the secret)
   ```
   [WARN] Sanitized 1 API key, 2 database passwords, 1 JWT token
   ```

2. **Generate compliance reports**: Document sanitization effectiveness
   ```bash
   ctxt security compliance-report > compliance-$(date +%Y-%m-%d).md
   ```

3. **Test incident response**: Verify procedures work
   ```bash
   # Simulate secret exposure
   ctxt security simulate-exposure --secret-type api_key
   ```

## Troubleshooting

### Secrets Not Being Detected

**Problem**: Expected secret isn't being detected

**Solutions**:
```bash
# Check if detection is enabled
ctxt config show | grep -A5 "secrets:"

# Test detection directly
echo "sk-proj-abc123def456" | ctxt security validate -

# Verify pattern is in rules
ctxt security list-patterns

# Add custom pattern if needed
# Edit config.yaml and add custom pattern
```

### False Positives

**Problem**: Legitimate values being flagged as secrets

**Solutions**:
```bash
# Add to whitelist
ctxt security whitelist add "my-value"

# Or edit config manually
vim ~/.config/contexthelp/config.yaml
# Add to security.secrets.detection.whitelist.exact_matches

# Check whitelist
ctxt security whitelist list
```

### Logs Not Sanitized

**Problem**: Secrets appearing in log files

**Solutions**:
```bash
# Check sanitization is enabled
ctxt config show | grep -A5 "logging:"

# Verify log level
export CH_SECURITY_LOGGING_SANITIZE_LEVELS_INFO=true

# Check log file directly
tail -f ~/.local/share/contexthelp/logs/contexthelp.log

# Force sanitization on/off
export CH_SECURITY_LOGGING_SANITIZE=true
```

### Performance Issues

**Problem**: Log sanitization slowing down application

**Solutions**:
```bash
# Use faster redaction mode
export CH_SECURITY_LOGGING_REDACTION_MODE=partial

# Reduce checked log levels
export CH_SECURITY_LOGGING_SANITIZE_LEVELS_DEBUG=false
export CH_SECURITY_LOGGING_SANITIZE_LEVELS_TRACE=false

# Use sampling in high-volume scenarios
export CH_SECURITY_LOGGING_SAMPLE_RATE=0.1  # Check 10% of logs
```

## API Reference

### SecretsValidator Interface

```go
type SecretsValidator interface {
    // Check if string contains a secret
    ValidateString(value string) *ValidationResult

    // Check with severity threshold
    ValidateStringWithThreshold(value string, severity Severity) *ValidationResult

    // Check content and return all matches
    ValidateContent(content string) []*ValidationResult

    // Check file
    ValidateFile(path string) ([]*ValidationResult, error)

    // Check directory recursively
    ValidateDirectory(path string) ([]*ValidationResult, error)
}

type ValidationResult struct {
    IsSecret    bool
    Pattern     string
    Severity    Severity
    Line        int
    Column      int
    Match       string
    Context     string
}
```

### SanitizedLogger Interface

```go
type SanitizedLogger interface {
    // Standard logging with automatic sanitization
    Debugf(format string, args ...interface{})
    Infof(format string, args ...interface{})
    Warnf(format string, args ...interface{})
    Errorf(format string, args ...interface{})

    // Log with context
    DebugfWithContext(ctx context.Context, format string, args ...interface{})

    // Manual sanitization
    Sanitize(value string) string
}
```

## Related Documentation

- [Secret Management Best Practices](./secret-management.md)
- [Configuration Security](./configuration-security.md)
- [dPKMS Security Model](../dpkms/security.md)
- [Environment Variables - Security Section](../environment-variables.md#security)
- [ADR-019: Encryption and Privacy](../decisions/ADR-019-encryption-and-privacy.md)
- [ADR-023: Authentication and Authorization](../decisions/ADR-023-authentication-authorization-model.md)

## Examples and Recipes

### Example 1: Secure Registry Configuration

```yaml
# config.yaml
registries:
  - name: company-registry
    url: https://registry.company.com
    type: taxonomy
    auth:
      token_file: ${CH_SECRETS_DIR}/registry-token
```

```bash
# Store token securely
mkdir -p ~/.config/contexthelp/secrets
chmod 700 ~/.config/contexthelp/secrets
echo "ghp_abc123def456" > ~/.config/contexthelp/secrets/registry-token
chmod 600 ~/.config/contexthelp/secrets/registry-token
```

### Example 2: Database Connection with Rotation

```yaml
# config.yaml
storage:
  type: postgres
  connection_string: ${CONTEXTHELP_DB_URL}
```

```bash
# Use connection string from environment
export CONTEXTHELP_DB_URL=$(aws secretsmanager get-secret-value \
  --secret-id contexthelp/db \
  --query SecretString \
  --output text)

dpkms serve
```

### Example 3: Multi-Environment Configuration

```bash
# .envrc (using direnv)
# Development
export CH_STORAGE_TYPE=sqlite
export CH_DEBUG=true

# Staging
if [ "$ENVIRONMENT" = "staging" ]; then
  export CH_STORAGE_TYPE=postgres
  export POSTGRES_PASSWORD=$(pass show staging/db)
  export OPENAI_API_KEY=$(pass show staging/openai-key)
fi

# Production
if [ "$ENVIRONMENT" = "production" ]; then
  export CH_STORAGE_TYPE=postgres
  export POSTGRES_PASSWORD=$(aws secretsmanager get-secret-value --secret-id prod/db --query SecretString --output text)
  export OPENAI_API_KEY=$(aws secretsmanager get-secret-value --secret-id prod/openai --query SecretString --output text)
fi
```

## Support and Questions

For questions about secrets validation or log sanitization:

1. Check the [troubleshooting section](#troubleshooting)
2. Review related documentation linked below
3. Check the [FAQ](../FAQ.md#security)
4. File an issue with details (without exposing secrets)

---

**Last Updated**: 2025-01-26
**Version**: 1.0
**Status**: Active
**Maintained by**: Security Team
