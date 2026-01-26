# Git Hooks for Secret Detection

ContextHelp includes pre-commit hooks that automatically scan for secrets and sensitive data before allowing commits. This prevents accidental exposure of API keys, passwords, and other credentials.

---

## Overview

The pre-commit hook performs 5 security checks:

1. **Gitleaks Scan** - Industry-standard secret detection
2. **Custom Pattern Matching** - ContextHelp-specific secret patterns
3. **Sensitive File Check** - Blocks `.env`, `.key`, `.token` files
4. **YAML Validation** - Detects plaintext secrets in config files
5. **Summary Report** - Clear pass/fail with remediation guidance

---

## Installation

### Automatic Installation (Recommended)

```bash
# Run the installer script
./scripts/install-hooks.sh
```

This will:
- Check for gitleaks installation
- Install the pre-commit hook
- Verify `.gitleaks.toml` configuration
- Check `.gitignore` completeness

### Manual Installation

```bash
# Make sure the hook is executable
chmod +x .git/hooks/pre-commit

# Verify installation
.git/hooks/pre-commit --version
```

### Install Gitleaks (Required)

**macOS:**
```bash
brew install gitleaks
```

**Linux:**
```bash
wget https://github.com/gitleaks/gitleaks/releases/download/v8.18.1/gitleaks_8.18.1_linux_x64.tar.gz
tar -xzf gitleaks_8.18.1_linux_x64.tar.gz
sudo mv gitleaks /usr/local/bin/
```

**Windows:**
```powershell
choco install gitleaks
```

---

## Usage

### Normal Workflow

The hook runs automatically before every commit:

```bash
# Stage your changes
git add file.go

# Commit (hook runs automatically)
git commit -m "Add feature"

# If secrets detected, commit is blocked
# Fix the issues and try again
```

### Example Output

**✅ When no secrets detected:**
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ContextHelp Pre-Commit Security Check (v1.0.0)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

[1/5] Checking for gitleaks installation...
✓ gitleaks found: gitleaks version 8.18.1

[2/5] Scanning staged files for secrets with gitleaks...
✓ No secrets detected by gitleaks

[3/5] Checking for common secret patterns...
✓ No common secret patterns detected

[4/5] Checking for sensitive files...
✓ No sensitive files detected

[5/5] Validating YAML configuration files...
✓ No plaintext secrets in YAML files

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ All security checks passed!

Your commit is safe to proceed.
```

**❌ When secrets detected:**
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ContextHelp Pre-Commit Security Check (v1.0.0)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

[1/5] Checking for gitleaks installation...
✓ gitleaks found: gitleaks version 8.18.1

[2/5] Scanning staged files for secrets with gitleaks...
✗ gitleaks found potential secrets!

Secret:      sk-proj-abc...
RuleID:      openai-api-key
File:        config.yaml:15
Commit:      (staged)

Please review the output above and:
  1. Remove the secrets from the files
  2. Use environment variables or config references instead
  3. Add false positives to .gitleaks.toml

[3/5] Checking for common secret patterns...
✗ Found OpenAI project API key in: config.yaml

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✗ Security checks failed!

Your commit has been blocked to prevent committing secrets.
```

---

## Bypass Options

### Skip All Checks (NOT Recommended)

```bash
# Emergency bypass - USE WITH CAUTION
git commit --no-verify -m "message"
```

**⚠️ Warning:** This skips ALL pre-commit checks, including secret detection. Only use in emergencies.

### Skip Specific Checks

Skip individual checks when you know they're false positives:

```bash
# Skip gitleaks only
SKIP_GITLEAKS=1 git commit -m "message"

# Skip pattern matching only
SKIP_PATTERNS=1 git commit -m "message"

# Skip file type check only
SKIP_FILE_CHECK=1 git commit -m "message"

# Skip YAML validation only
SKIP_YAML_CHECK=1 git commit -m "message"

# Combine multiple skips
SKIP_GITLEAKS=1 SKIP_PATTERNS=1 git commit -m "message"
```

---

## Detected Secret Types

### API Keys

- **OpenAI API Keys**: `sk-proj-...` (project keys), `sk-...` (legacy)
- **Anthropic API Keys**: `sk-ant-...`
- **Generic API Keys**: `api_key="..."`

### Authentication

- **JWT Tokens**: `eyJ...`
- **GitHub Tokens**: `ghp_...`, `gho_...`, `ghr_...`
- **Bearer Tokens**: `Authorization: Bearer ...`
- **Basic Auth**: `Authorization: Basic ...`

### Cloud Providers

- **AWS Access Keys**: `AKIA...`
- **AWS Secret Keys**: Pattern in config
- **Google Cloud API Keys**
- **Azure Connection Strings**

### Databases

- **PostgreSQL Connection Strings**: `postgresql://user:password@...`
- **MySQL Connection Strings**: `mysql://user:password@...`
- **Redis URLs**: `redis://password@...`
- **MongoDB Connection Strings**

### Webhooks & Integrations

- **Slack Webhooks**: `https://hooks.slack.com/...`
- **Discord Webhooks**
- **Generic Webhook URLs with Tokens**

### Cryptographic Keys

- **Private Keys**: `-----BEGIN PRIVATE KEY-----`
- **SSH Keys**: `id_rsa`, `id_ed25519`
- **PGP Keys**: `.gpg`, `.asc`

---

## Configuration

### .gitleaks.toml

Customize secret detection rules in `.gitleaks.toml`:

```toml
[[rules]]
id = "custom-api-key"
description = "My Custom API Key Format"
regex = '''myapi-[a-zA-Z0-9]{32}'''
tags = ["api-key", "custom"]
```

### Allowlist

Add false positives to the allowlist:

```toml
[allowlist]
paths = [
  '''example\.yaml$''',        # Allow all example files
  '''testdata/''',              # Allow test fixtures
  '''docs/''',                  # Allow documentation
]

regexes = [
  '''\$\{[A-Z_]+\}''',         # Allow ${ENV_VAR} syntax
  '''your[_-]?key[_-]?here''', # Allow placeholder text
]
```

### Custom Patterns

Add ContextHelp-specific patterns to the pre-commit hook:

Edit `.git/hooks/pre-commit` and add to the `PATTERNS` array:

```bash
declare -a PATTERNS=(
    "registry[_-]?token[_-][a-zA-Z0-9]{32}"  # Registry tokens
    "plugin[_-]?key[_-][a-zA-Z0-9]{20,}"     # Plugin keys
)
```

---

## Testing

### Test the Hook

```bash
# Run test suite
./scripts/test-hooks.sh
```

This runs 8 test scenarios:
1. ✓ Safe config with environment variables
2. ✗ Config with OpenAI key (should be blocked)
3. ✗ Config with Anthropic key (should be blocked)
4. ✗ .env file (should be blocked)
5. ✓ Safe markdown documentation
6. ✗ File with AWS key (should be blocked)
7. ✗ File with GitHub token (should be blocked)
8. ✓ Example file (should be allowed)

### Manual Testing

```bash
# Create a test file with a fake secret
echo "api_key: sk-proj-test123fake" > test-secret.yaml

# Stage it
git add test-secret.yaml

# Try to commit (should be blocked)
git commit -m "test"

# Clean up
git reset HEAD test-secret.yaml
rm test-secret.yaml
```

---

## Troubleshooting

### Hook Not Running

**Problem:** Pre-commit hook doesn't execute

**Solutions:**
```bash
# Check if hook exists
ls -la .git/hooks/pre-commit

# Make it executable
chmod +x .git/hooks/pre-commit

# Verify it's not disabled
git config --get core.hooksPath
```

### Gitleaks Not Found

**Problem:** `gitleaks: command not found`

**Solutions:**
```bash
# Install gitleaks
brew install gitleaks  # macOS
apt-get install gitleaks  # Ubuntu/Debian

# Or skip gitleaks temporarily
SKIP_GITLEAKS=1 git commit -m "message"
```

### False Positives

**Problem:** Hook blocks legitimate code

**Solutions:**

1. **Add to .gitleaks.toml allowlist:**
```toml
[allowlist]
regexes = [
  '''your-false-positive-pattern''',
]
```

2. **Skip the check:**
```bash
SKIP_PATTERNS=1 git commit -m "message"
```

3. **Use environment variable syntax:**
```yaml
# Instead of:
api_key: sk-proj-actual-key

# Use:
api_key: ${OPENAI_API_KEY}
```

### Hook Too Slow

**Problem:** Hook takes too long to run

**Solutions:**
```bash
# Skip gitleaks for faster commits (less secure)
SKIP_GITLEAKS=1 git commit -m "message"

# Or disable pattern matching
SKIP_PATTERNS=1 git commit -m "message"

# For very large commits, bypass temporarily
git commit --no-verify -m "message"
```

### Example Files Blocked

**Problem:** Example files like `config.example.yaml` are blocked

**Solution:**
Add to `.gitleaks.toml`:
```toml
[allowlist]
paths = [
  '''\.example\.yaml$''',
  '''config\.example\.''',
]
```

---

## Best Practices

### 1. Never Bypass Without Reason

```bash
# ❌ Bad: Always using --no-verify
git commit --no-verify -m "quick fix"

# ✓ Good: Fix the issue, then commit normally
# Remove secrets, use env vars, then:
git commit -m "fix configuration"
```

### 2. Use Environment Variables

```yaml
# ❌ Bad: Hardcoded secrets
api_key: sk-proj-abc123...

# ✓ Good: Environment variable reference
api_key: ${OPENAI_API_KEY}
```

### 3. Keep .gitignore Updated

Ensure `.gitignore` includes:
```gitignore
.env
.env.*
*.key
*.token
*.pem
config.yaml
```

### 4. Regularly Update Gitleaks

```bash
# Update to latest version
brew upgrade gitleaks

# Or manually download from GitHub
# https://github.com/gitleaks/gitleaks/releases
```

### 5. Review Hook Output

Always read what the hook tells you:
- **File paths**: Which files contain secrets
- **Line numbers**: Exact location of the issue
- **Secret type**: What kind of secret was detected
- **Remediation**: How to fix the issue

### 6. Test Before Committing

```bash
# Run hook manually before committing
.git/hooks/pre-commit

# Or use test suite
./scripts/test-hooks.sh
```

---

## CI/CD Integration

### GitHub Actions

Add secret scanning to your CI pipeline:

```yaml
name: Security Scan

on: [push, pull_request]

jobs:
  gitleaks:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
        with:
          fetch-depth: 0

      - name: Run Gitleaks
        uses: gitleaks/gitleaks-action@v2
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

### GitLab CI

```yaml
security-scan:
  stage: test
  image: zricethezav/gitleaks:latest
  script:
    - gitleaks detect --verbose --redact --report-path gitleaks-report.json
  artifacts:
    reports:
      sast: gitleaks-report.json
```

---

## Additional Resources

### Documentation
- [SECURITY.md](../SECURITY.md) - Complete security policy
- [Environment Variables](environment-variables.md) - Secret management guide
- [Configuration Guide](ctxt/configuration.md) - Config file best practices

### External Tools
- [Gitleaks](https://github.com/gitleaks/gitleaks) - Secret detection tool
- [TruffleHog](https://github.com/trufflesecurity/trufflehog) - Alternative scanner
- [git-secrets](https://github.com/awslabs/git-secrets) - AWS Labs tool

### Related Guides
- [OWASP Secrets Management](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html)
- [GitHub Secret Scanning](https://docs.github.com/en/code-security/secret-scanning)
- [GitLab Secret Detection](https://docs.gitlab.com/ee/user/application_security/secret_detection/)

---

## Support

If you encounter issues with the pre-commit hooks:

1. **Check the documentation** - Most issues are covered here
2. **Run the test suite** - `./scripts/test-hooks.sh`
3. **Check gitleaks logs** - Look for `.gitleaks` output
4. **Review .gitleaks.toml** - Verify configuration
5. **Open an issue** - https://github.com/ideacrafterslabs/ctxt/issues

**Last Updated:** 2024-01-26
**Version:** 1.0.0
