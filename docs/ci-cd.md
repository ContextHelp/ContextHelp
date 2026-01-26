# CI/CD Pipeline

Continuous Integration and Continuous Deployment for **dPKMS + `ctxt`**.

---

## Overview

The CI/CD pipeline automates:
- **Code quality** — Linting, formatting, static analysis
- **Security** — Vulnerability scanning, dependency audits
- **Testing** — Unit, integration, and E2E tests
- **Building** — Multi-platform binary compilation
- **Deployment** — Automated releases and artifacts

**Platform:** GitHub Actions
**Configuration:** `.github/workflows/ci.yml`

---

## Pipeline Stages

### 1. Lint Stage

**Runs on:** Every push and pull request

**Jobs:**
- golangci-lint (with 40+ linters)
- Code formatting check (`gofmt`)
- Go vet static analysis

**Configuration:**
```yaml
lint:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version: '1.23'
    - name: Run golangci-lint
      run: golangci-lint run --timeout=5m
    - name: Check formatting
      run: gofmt -l .
    - name: Run go vet
      run: go vet ./...
```

**Linters enabled:**
- `gofmt`, `goimports` — Formatting
- `govet` — Static analysis
- `errcheck` — Unchecked errors
- `staticcheck` — Static analysis
- `gosec` — Security issues
- `ineffassign` — Ineffectual assignments
- `misspell` — Spelling errors
- `unused` — Unused code
- And 30+ more (see `.golangci.yml`)

---

### 2. Security Stage

**Runs on:** Every push and pull request

**Jobs:**
- `govulncheck` — Known vulnerabilities
- Trivy scanner — Container and dependency scanning
- SARIF upload to GitHub Security

**Configuration:**
```yaml
security:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - name: Install govulncheck
      run: go install golang.org/x/vuln/cmd/govulncheck@latest
    - name: Run govulncheck
      run: govulncheck ./...
    - name: Run Trivy
      uses: aquasecurity/trivy-action@master
      with:
        scan-type: 'fs'
        format: 'sarif'
        output: 'trivy-results.sarif'
    - name: Upload to GitHub Security
      uses: github/codeql-action/upload-sarif@v3
      with:
        sarif_file: 'trivy-results.sarif'
```

**What's scanned:**
- Go module vulnerabilities
- Dependency security issues
- License compliance
- Secrets in code (gitleaks)
- Configuration vulnerabilities

---

### 3. Test Stage

**Runs on:** Every push and pull request

**Matrix:**
- **OS:** Ubuntu, macOS, Windows
- **Go version:** 1.23

**Jobs:**
- Unit tests (`go test -short`)
- Race detector enabled
- Code coverage report
- Codecov upload

**Configuration:**
```yaml
test:
  strategy:
    matrix:
      os: [ubuntu-latest, macos-latest, windows-latest]
      go-version: ['1.23']
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
    - name: Run tests
      run: go test -v -short -race -coverprofile=coverage.out ./...
    - name: Upload coverage
      uses: codecov/codecov-action@v4
      if: matrix.os == 'ubuntu-latest'
```

**Test Types:**
- **Unit tests** — Fast, isolated, mocked dependencies
- **Coverage minimum** — 70% (enforced)
- **Race detection** — Concurrent access bugs

---

### 4. Integration Test Stage

**Runs on:** Pull requests and main branch pushes

**Services:**
- PostgreSQL 16
- Redis 7

**Jobs:**
- Integration tests with real services
- Database migration tests
- Job queue tests
- Registry connector tests

**Configuration:**
```yaml
integration:
  runs-on: ubuntu-latest
  services:
    postgres:
      image: postgres:16-alpine
      env:
        POSTGRES_USER: contexthelp
        POSTGRES_PASSWORD: test_password
        POSTGRES_DB: contexthelp_test
      options: >-
        --health-cmd pg_isready
        --health-interval 10s
      ports:
        - 5432:5432
    
    redis:
      image: redis:7-alpine
      options: >-
        --health-cmd "redis-cli ping"
        --health-interval 10s
      ports:
        - 6379:6379
  
  steps:
    - name: Run integration tests
      run: go test -v -tags=integration ./test/integration/...
      env:
        POSTGRES_HOST: localhost
        REDIS_HOST: localhost
```

---

### 5. Build Stage

**Runs on:** After lint, security, and test pass

**Jobs:**
- Build `dpkms` binary
- Build `ctxt` binary
- Verify binaries execute
- Upload artifacts (7-day retention)

**Configuration:**
```yaml
build:
  runs-on: ubuntu-latest
  needs: [lint, security, test]
  steps:
    - uses: actions/checkout@v4
    - name: Build dpkms
      run: go build -v -o bin/dpkms ./cmd/dpkms
    - name: Build ctxt
      run: go build -v -o bin/ctxt ./cmd/ctxt
    - name: Verify binaries
      run: |
        ./bin/dpkms version
        ./bin/ctxt version
    - name: Upload artifacts
      uses: actions/upload-artifact@v4
      with:
        name: binaries
        path: bin/
```

---

## Workflow Triggers

### Push to Main/Develop

```yaml
on:
  push:
    branches:
      - main
      - develop
```

**Runs:**
- Full pipeline (lint, security, test, integration, build)
- Coverage upload
- Artifact creation

### Pull Requests

```yaml
on:
  pull_request:
    branches:
      - main
      - develop
```

**Runs:**
- Full pipeline
- Required checks must pass before merge
- Codecov comment on PR

### Manual Dispatch

```yaml
on:
  workflow_dispatch:
```

**Allows:**
- Manual pipeline runs via GitHub UI
- Useful for testing or re-running failed jobs

---

## Local CI Simulation

Run the entire CI pipeline locally:

```bash
# Run all CI checks
task ci

# Individual stages
task ci:lint
task ci:security
task ci:test
task ci:build
```

**Task definitions** (from `Taskfile.yml`):
```yaml
ci:
  desc: Run all CI checks locally
  deps: [ci:lint, ci:security, ci:test, ci:build]

ci:lint:
  cmds:
    - task: lint
    - task: fmt
    - task: vet

ci:security:
  cmds:
    - govulncheck ./...
    - gosec -quiet ./...

ci:test:
  cmds:
    - go test -v -short -race -cover ./...

ci:build:
  cmds:
    - go build -v -o bin/dpkms ./cmd/dpkms
    - go build -v -o bin/ctxt ./cmd/ctxt
```

---

## Release Pipeline

### Automatic Releases

**Trigger:** Git tag push (`v*.*.*`)

```bash
git tag -a v0.2.0 -m "Release v0.2.0"
git push origin v0.2.0
```

**Actions:**
1. Run full CI pipeline
2. Build multi-platform binaries
3. Generate changelog
4. Create GitHub Release
5. Upload release assets

**Platforms:**
- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64)

### GoReleaser Configuration

`.goreleaser.yml`:
```yaml
builds:
  - id: dpkms
    binary: dpkms
    main: ./cmd/dpkms
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    
  - id: ctxt
    binary: ctxt
    main: ./cmd/ctxt
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]

archives:
  - format: tar.gz
    name_template: >-
      {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}

checksum:
  name_template: 'checksums.txt'

changelog:
  sort: asc
  filters:
    exclude:
      - '^docs:'
      - '^test:'
```

---

## Environment Secrets

**Required secrets** (configured in GitHub repo settings):

### For Testing
- `POSTGRES_PASSWORD` — Test database password
- `REDIS_PASSWORD` — Test Redis password (if auth enabled)

### For Releases
- `GITHUB_TOKEN` — Automatically provided by GitHub
- `CODECOV_TOKEN` — Code coverage reporting

### For Container Registry (Future)
- `DOCKER_USERNAME` — Docker Hub username
- `DOCKER_PASSWORD` — Docker Hub token
- `GHCR_TOKEN` — GitHub Container Registry token

**Setting secrets:**
1. Go to repository → Settings → Secrets and variables → Actions
2. Click "New repository secret"
3. Add name and value
4. Save

---

## Branch Protection Rules

### Main Branch

**Required checks:**
- Lint stage must pass
- Security stage must pass
- Test stage must pass (all OS)
- Integration tests must pass

**Rules:**
- Require PR reviews (1 approver minimum)
- Dismiss stale reviews on new commits
- Require status checks to pass
- Require branches to be up to date
- Require linear history
- Include administrators (best practice)

**Configuration:**
```yaml
# In repository settings → Branches → Branch protection rules
Branch name pattern: main

☑ Require a pull request before merging
  ☑ Require approvals: 1
  ☑ Dismiss stale pull request approvals

☑ Require status checks to pass before merging
  ☑ Require branches to be up to date
  Required checks:
    - Lint
    - Security Scan
    - Test (ubuntu-latest)
    - Test (macos-latest)
    - Test (windows-latest)
    - Integration Tests

☑ Require linear history
☑ Include administrators
```

### Develop Branch

**Required checks:**
- Lint must pass
- Tests must pass

**Rules:**
- Less strict than main
- Allows force push (for rebasing)
- No review required (optional)

---

## Monitoring & Notifications

### Status Badges

Add to `README.md`:

```markdown
[![CI](https://github.com/ideacrafterslabs/ctxt/workflows/CI/badge.svg)](https://github.com/ideacrafterslabs/ctxt/actions)
[![codecov](https://codecov.io/gh/ideacrafterslabs/ctxt/branch/main/graph/badge.svg)](https://codecov.io/gh/ideacrafterslabs/ctxt)
[![Go Report Card](https://goreportcard.com/badge/github.com/ideacrafterslabs/ctxt)](https://goreportcard.com/report/github.com/ideacrafterslabs/ctxt)
```

### Slack Notifications

Add to workflow (optional):

```yaml
- name: Slack Notification
  if: always()
  uses: 8398a7/action-slack@v3
  with:
    status: ${{ job.status }}
    text: 'CI Pipeline: ${{ job.status }}'
    webhook_url: ${{ secrets.SLACK_WEBHOOK }}
```

---

## Debugging Failed Builds

### View Logs

```bash
# Via GitHub CLI
gh run list --workflow=ci.yml
gh run view <run-id>
gh run view <run-id> --log

# Or via web UI
# https://github.com/ideacrafterslabs/ctxt/actions
```

### Common Failures

**Lint failures:**
```bash
# Fix locally
task lint:fix
task fmt
git commit -am "fix: lint issues"
git push
```

**Test failures:**
```bash
# Run locally
task test
# Or specific test
go test -v -run TestFailingTest ./...
```

**Dependency issues:**
```bash
# Update dependencies
go mod tidy
go mod verify
git commit -am "chore: update dependencies"
```

---

## Performance

**Average pipeline duration:**
- Lint: ~2 minutes
- Security: ~3 minutes
- Test: ~5 minutes (per OS)
- Integration: ~4 minutes
- Build: ~2 minutes

**Total:** ~15-20 minutes for full pipeline

**Optimization tips:**
- Use caching for Go modules
- Run independent jobs in parallel
- Use matrix for multi-OS testing
- Cache golangci-lint

---

## Future Enhancements

**Planned additions:**
- E2E tests with Playwright
- Performance benchmarking
- Container image builds
- Deployment to staging
- Automated dependency updates (Dependabot/Renovate)
- Automated security scanning (Snyk)

---

## Related Documentation

- [development.md](development.md) — Development workflow
- [developer-quickstart.md](developer-quickstart.md) — Quick setup
- [testing.md](dpkms/testing.md) — Testing strategies
- [security/](security/) — Security documentation

---

For local development, see [development.md](development.md).
