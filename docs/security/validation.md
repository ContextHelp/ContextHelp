# Input Validation & Secret Patterns

Input validation and secret detection patterns for ContextHelp.

---

## Overview

ContextHelp implements comprehensive input validation and secret detection to protect against:

- Injection attacks (SQL, command, path traversal)
- Malformed data
- Secret exposure
- Configuration errors

---

## Secret Detection Patterns

### API Keys and Tokens

| Pattern | Example | Regex | Priority |
|---------|---------|-------|----------|
| OpenAI API keys | `sk-proj-...` | `sk-proj-[a-zA-Z0-9]{40,}` | Critical |
| Anthropic API keys | `sk-ant-...` | `sk-ant-[a-zA-Z0-9]{40,}` | Critical |
| GitHub tokens | `ghp_...` | `ghp_[a-zA-Z0-9]{36,}` | High |
| GitHub OAuth | `gho_...` | `gho_[a-zA-Z0-9]{36,}` | High |
| GitHub PAT | `github_pat_...` | `github_pat_[a-zA-Z0-9_]{82}` | High |
| AWS Access Keys | `AKIA...` | `AKIA[0-9A-Z]{16}` | Critical |
| Google API keys | `AIza...` | `AIza[0-9A-Za-z\\-_]{35}` | High |
| Stripe API keys | `sk_live_...` | `sk_(live|test)_[0-9a-zA-Z]{24,}` | High |
| JWT tokens | `eyJ...` | `eyJ[A-Za-z0-9_-]{10,}\\.[A-Za-z0-9_-]{10,}\\.[A-Za-z0-9_-]{10,}` | High |
| Bearer tokens | `Bearer ...` | `Bearer [a-zA-Z0-9_\\-]{40,}` | High |

### Database Credentials

| Pattern | Example | Regex | Priority |
|---------|---------|-------|----------|
| PostgreSQL URL | `postgres://user:pass@host/db` | `postgres(ql)?://[^:]+:[^@]+@` | Critical |
| MySQL URL | `mysql://user:pass@host/db` | `mysql://[^:]+:[^@]+@` | Critical |
| MongoDB URL | `mongodb://user:pass@host/db` | `mongodb://[^:]+:[^@]+@` | Critical |
| Password fields | `password: value` | `password["\']?\\s*[:=]\\s*["\']?([^"'\s]+)` | Critical |

### Encryption Keys

| Pattern | Example | Regex | Priority |
|---------|---------|-------|----------|
| Private keys | `-----BEGIN PRIVATE KEY-----` | `-----BEGIN [A-Z ]+ PRIVATE KEY-----` | Critical |
| SSH keys | `-----BEGIN OPENSSH PRIVATE KEY-----` | `-----BEGIN OPENSSH PRIVATE KEY-----` | Critical |
| Encryption passphrases | `passphrase: value` | `passphrase["\']?\\s*[:=]\\s*["\']?([^"'\s]+)` | Critical |

### Authentication Headers

| Pattern | Example | Regex | Priority |
|---------|---------|-------|----------|
| Authorization | `Authorization: ...` | `Authorization["\']?\\s*[:=]\\s*["\']?([^"'\s]+)` | High |
| X-API-Key | `X-API-Key: ...` | `X-API-Key["\']?\\s*[:=]\\s*["\']?([^"'\s]+)` | High |
| Basic auth | `Authorization: Basic ...` | `Authorization:\\s*Basic\\s+[A-Za-z0-9+/=]{20,}` | High |

---

## Pattern Implementation

### Go Implementation

```go
type SecretPattern struct {
    Name        string
    Pattern     *regexp.Regexp
    Priority    Priority
    Description string
}

var SecretPatterns = []SecretPattern{
    {
        Name:        "openai_api_key",
        Pattern:     regexp.MustCompile(`sk-proj-[a-zA-Z0-9]{40,}`),
        Priority:    PriorityCritical,
        Description: "OpenAI API key",
    },
    {
        Name:        "anthropic_api_key",
        Pattern:     regexp.MustCompile(`sk-ant-[a-zA-Z0-9]{40,}`),
        Priority:    PriorityCritical,
        Description: "Anthropic API key",
    },
    {
        Name:        "github_token",
        Pattern:     regexp.MustCompile(`ghp_[a-zA-Z0-9]{36,}`),
        Priority:    PriorityHigh,
        Description: "GitHub personal access token",
    },
    {
        Name:        "aws_access_key",
        Pattern:     regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
        Priority:    PriorityCritical,
        Description: "AWS access key",
    },
    {
        Name:        "postgres_url",
        Pattern:     regexp.MustCompile(`postgres(ql)?://[^:]+:[^@]+@`),
        Priority:    PriorityCritical,
        Description: "PostgreSQL connection string",
    },
    {
        Name:        "private_key",
        Pattern:     regexp.MustCompile(`-----BEGIN [A-Z ]+ PRIVATE KEY-----`),
        Priority:    PriorityCritical,
        Description: "Private key",
    },
}
```

### Detection Function

```go
func DetectSecrets(content string) []SecretMatch {
    var matches []SecretMatch
    
    for _, pattern := range SecretPatterns {
        if pattern.Pattern.MatchString(content) {
            locations := pattern.Pattern.FindAllStringIndex(content, -1)
            for _, loc := range locations {
                matches = append(matches, SecretMatch{
                    Pattern:  pattern.Name,
                    Location: loc,
                    Priority: pattern.Priority,
                })
            }
        }
    }
    
    return matches
}
```

### Validation Function

```go
func ValidateNoSecrets(content string) error {
    matches := DetectSecrets(content)
    if len(matches) > 0 {
        return fmt.Errorf("detected %d potential secrets", len(matches))
    }
    return nil
}
```

---

## Input Validation

### Size Limits

```go
const (
    MaxRequestSize   = 10 * MB
    MaxQueryLength   = 1000
    MaxResultLimit   = 100
    MaxBatchSize     = 50
    MaxFilenameLen   = 255
    MaxPathDepth     = 10
)

func ValidateSize(data []byte, maxSize int) error {
    if len(data) > maxSize {
        return fmt.Errorf("input too large: %d bytes (max %d)", len(data), maxSize)
    }
    return nil
}
```

### Type Validation

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

### Whitelist Validation

```go
var AllowedSortFields = []string{"relevance", "date", "title", "size"}

func ValidateSortField(field string) error {
    for _, allowed := range AllowedSortFields {
        if field == allowed {
            return nil
        }
    }
    return fmt.Errorf("invalid sort field: %s (allowed: %v)", field, AllowedSortFields)
}
```

### Path Traversal Prevention

```go
func ValidatePath(path string, baseDir string) error {
    // Clean path
    cleanPath := filepath.Clean(path)
    
    // Get absolute paths
    absPath, err := filepath.Abs(cleanPath)
    if err != nil {
        return fmt.Errorf("invalid path: %w", err)
    }
    
    absBase, err := filepath.Abs(baseDir)
    if err != nil {
        return fmt.Errorf("invalid base directory: %w", err)
    }
    
    // Check path is within base directory
    if !strings.HasPrefix(absPath, absBase) {
        return fmt.Errorf("path traversal detected: %s not in %s", path, baseDir)
    }
    
    return nil
}
```

### SQL Injection Prevention

```go
// GOOD - Parameterized queries
func GetObject(db *sql.DB, id string) (*Object, error) {
    var obj Object
    err := db.QueryRow("SELECT * FROM objects WHERE id = ?", id).Scan(&obj)
    return &obj, err
}

// BAD - String concatenation (vulnerable)
func GetObjectUnsafe(db *sql.DB, id string) (*Object, error) {
    query := "SELECT * FROM objects WHERE id = '" + id + "'"
    // Vulnerable to: id = "1' OR '1'='1"
    return nil, nil
}
```

### Command Injection Prevention

```go
// GOOD - No shell execution
func RunCommand(binary string, args []string) error {
    cmd := exec.Command(binary, args...)
    return cmd.Run()
}

// BAD - Shell execution (vulnerable)
func RunCommandUnsafe(command string) error {
    cmd := exec.Command("sh", "-c", command)
    // Vulnerable to: command = "ls; rm -rf /"
    return cmd.Run()
}
```

---

## Configuration Validation

### Schema Validation

```go
type Config struct {
    Storage  StorageConfig  `json:"storage" validate:"required"`
    Server   ServerConfig   `json:"server" validate:"required"`
    Security SecurityConfig `json:"security"`
}

type StorageConfig struct {
    Type string `json:"type" validate:"required,oneof=sqlite postgres"`
    Path string `json:"path" validate:"required_if=Type sqlite"`
}

func ValidateConfig(config *Config) error {
    validate := validator.New()
    if err := validate.Struct(config); err != nil {
        return fmt.Errorf("invalid configuration: %w", err)
    }
    
    // Additional validation
    if config.Storage.Type == "sqlite" && config.Storage.Path == "" {
        return fmt.Errorf("sqlite requires path")
    }
    
    return nil
}
```

### URL Validation

```go
func ValidateURL(rawURL string) error {
    u, err := url.Parse(rawURL)
    if err != nil {
        return fmt.Errorf("invalid URL: %w", err)
    }
    
    // Enforce HTTPS for external URLs
    if u.Scheme != "https" {
        return fmt.Errorf("URL must use HTTPS: %s", rawURL)
    }
    
    // Check domain not localhost (for external URLs)
    if u.Host == "localhost" || u.Host == "127.0.0.1" {
        return fmt.Errorf("external URL cannot be localhost")
    }
    
    return nil
}
```

---

## Custom Validation Rules

### Add Custom Patterns

```yaml
# config.yaml
security:
  validation:
    custom_patterns:
      - name: internal_api_key
        pattern: "x-internal-[a-z0-9]{32}"
        priority: high
        description: "Internal API key"
      
      - name: legacy_token
        pattern: "token_legacy_[a-zA-Z0-9]{64}"
        priority: medium
        description: "Legacy authentication token"
```

### Load Custom Patterns

```go
func LoadCustomPatterns(configFile string) ([]SecretPattern, error) {
    data, err := ioutil.ReadFile(configFile)
    if err != nil {
        return nil, err
    }
    
    var config struct {
        Security struct {
            Validation struct {
                CustomPatterns []struct {
                    Name        string `yaml:"name"`
                    Pattern     string `yaml:"pattern"`
                    Priority    string `yaml:"priority"`
                    Description string `yaml:"description"`
                } `yaml:"custom_patterns"`
            } `yaml:"validation"`
        } `yaml:"security"`
    }
    
    if err := yaml.Unmarshal(data, &config); err != nil {
        return nil, err
    }
    
    var patterns []SecretPattern
    for _, cp := range config.Security.Validation.CustomPatterns {
        pattern, err := regexp.Compile(cp.Pattern)
        if err != nil {
            return nil, fmt.Errorf("invalid pattern %s: %w", cp.Name, err)
        }
        
        patterns = append(patterns, SecretPattern{
            Name:        cp.Name,
            Pattern:     pattern,
            Priority:    ParsePriority(cp.Priority),
            Description: cp.Description,
        })
    }
    
    return patterns, nil
}
```

---

## Testing Validation

### Unit Tests

```go
func TestSecretDetection(t *testing.T) {
    tests := []struct {
        input    string
        expected int  // Number of secrets expected
    }{
        {"sk-proj-abc123def456", 1},
        {"postgres://user:pass@host/db", 1},
        {"normal text", 0},
        {"sk-proj-abc123 and ghp_def456", 2},
    }
    
    for _, tt := range tests {
        matches := DetectSecrets(tt.input)
        assert.Len(t, matches, tt.expected)
    }
}

func TestPathTraversal(t *testing.T) {
    baseDir := "/var/lib/contexthelp"
    
    tests := []struct {
        path    string
        valid   bool
    }{
        {"/var/lib/contexthelp/data/file.txt", true},
        {"../../../etc/passwd", false},
        {"/etc/passwd", false},
        {"data/file.txt", true},
    }
    
    for _, tt := range tests {
        err := ValidatePath(tt.path, baseDir)
        if tt.valid {
            assert.NoError(t, err)
        } else {
            assert.Error(t, err)
        }
    }
}
```

### Validation CLI

```bash
# Test secret detection
ctxt security test-pattern "sk-proj-abc123"

# Validate configuration
ctxt config validate

# Scan file for secrets
ctxt security scan-file config.yaml

# Scan directory
ctxt security scan-dir ~/.config/contexthelp/
```

---

## Performance Optimization

### Pattern Compilation

```go
// Compile patterns once at startup
var compiledPatterns []*regexp.Regexp

func init() {
    for _, pattern := range SecretPatterns {
        compiledPatterns = append(compiledPatterns, pattern.Pattern)
    }
}
```

### Early Termination

```go
func QuickCheck(content string) bool {
    // Fast checks before regex
    if len(content) < 20 {
        return false  // Too short for secrets
    }
    
    // Check for common prefixes
    quickPrefixes := []string{"sk-", "ghp_", "AKIA", "-----BEGIN"}
    for _, prefix := range quickPrefixes {
        if strings.Contains(content, prefix) {
            return true  // Needs full validation
        }
    }
    
    return false
}
```

---

## Exclusions

### Test/Mock Data

```yaml
security:
  validation:
    exclusions:
      - pattern: "test_api_key_example"
        reason: "Test fixture"
      
      - pattern: "mock_password_123"
        reason: "Mock data for tests"
      
      - path: "test/**"
        reason: "Test directory"
```

### Apply Exclusions

```go
func IsExcluded(match SecretMatch, exclusions []Exclusion) bool {
    for _, exclusion := range exclusions {
        if exclusion.Pattern != "" && strings.Contains(match.Content, exclusion.Pattern) {
            return true
        }
        
        if exclusion.Path != "" && matchesPath(match.File, exclusion.Path) {
            return true
        }
    }
    return false
}
```

---

## Related Documentation

- [log-sanitization.md](log-sanitization.md) — Log sanitization
- [secret-management.md](secret-management.md) — Secret lifecycle
- [model/controls.md](model/controls.md) — Security controls
- [secrets-validation-and-log-sanitization.md](secrets-validation-and-log-sanitization.md) — Comprehensive guide

---

**Last Updated**: 2026-01-26
**Version**: 1.0
**Status**: Active
**Maintained by**: Security Team
