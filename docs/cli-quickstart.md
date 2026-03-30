# Quick Start Guide - CLI Implementation

This guide will help you get started with the ContextHelp CLI implementation.

## Prerequisites

- Go version compatible with `go.mod`
- `task` (preferred; if it is not on `PATH`, use `mise exec -- task ...`)

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/ideacrafterslabs/ctxt.git
cd ctxt

# Build both binaries
task build

# Or build individually
task build:ctxt
task build:dpkms

# Install to $GOPATH/bin
task install
```

### Manual Build

```bash
# Build ctxt
go build -o bin/ctxt cmd/ctxt/main.go

# Build dpkms
go build -o bin/dpkms cmd/dpkms/main.go
```

## Configuration

### Create Configuration Directory

```bash
mkdir -p ~/.config/contexthelp
```

### Create Configuration File

Copy the example configuration:

```bash
cp config/config.example.yaml ~/.config/contexthelp/config.yaml
```

Edit the configuration file to customize settings:

```bash
# Use your preferred editor
$EDITOR ~/.config/contexthelp/config.yaml
```

### Environment Variables (Optional)

You can override configuration using environment variables:

```bash
# Data directory
export CTXT_DATA_DIR=~/.local/share/contexthelp

# Default profile
export CTXT_PROFILE=founder

# Worker count
export DPKMS_WORKERS=8
```

## Register ctxt:// URI Scheme (Optional)

Make `ctxt://` links clickable system-wide — clicking one opens ctxt:

```bash
ctxt uri register
```

After registration, `ctxt://obj_12345678` opens that object and `ctxt://search/my+query` runs a search.
For packaging (app bundles, plist, `.desktop`), use `ctxt uri snippet --platform <platform>`.

## Usage

### 1. Start the dPKMS Server (Optional)

For full functionality, start the background worker and API server:

```bash
./bin/dpkms serve
```

This starts:
- Background job worker for processing ingestion
- REST API on port 8080
- gRPC API on port 9090

### 2. Analyze Content

Capture and analyze various types of content from arguments, stdin, or the clipboard.

**If no subcommand is provided, `ctxt` defaults to `analyze`.**

```bash
# Analyze text directly (default behavior)
./bin/ctxt "Fix the signup flow to reduce friction"

# Analyze from clipboard (if argument and stdin are empty)
./bin/ctxt

# Analyze text from piped stdin
echo "UX improvements needed" | ./bin/ctxt

# Analyze with metadata using the explicit 'analyze' subcommand
./bin/ctxt analyze \
  --hints "#ux #critical" \
  --mentions "@ui.best-practice"

# Analyze a URL
./bin/ctxt "https://example.com/article"

# Analyze an image using a file
./bin/ctxt --file screenshot.png --type image

# Wait for job completion
./bin/ctxt "Important note" --wait
```

### 3. Manage Jobs

Monitor and control ingestion jobs:

```bash
# List all jobs
./bin/ctxt job list

# Check job status
./bin/ctxt job status job_12345678

# View job logs
./bin/ctxt job log job_12345678

# Retry failed job
./bin/ctxt job retry job_12345678
```

### 4. Query Knowledge

Search and query your knowledge base:

```bash
# List all objects
./bin/ctxt list

# Filter by tags
./bin/ctxt list --tag ux,onboarding

# Filter by type
./bin/ctxt list --type url

# Hybrid search (FTS + vector, default)
./bin/ctxt find "authentication best practices"

# FTS-only or vector-only
./bin/ctxt find "authentication" --fts
./bin/ctxt find "authentication" --semantic

# View object details
./bin/ctxt open obj_12345678
```

### 5. Use Focus Profiles

Manage focus profiles for contextual filtering:

```bash
# List profiles
./bin/ctxt profile list

# Show profile details
./bin/ctxt profile show founder

# Create custom profile
./bin/ctxt profile create myproject --config profile.yaml

# Set default profile
./bin/ctxt profile set-default founder

# Use profile with commands
./bin/ctxt analyze "Strategic insight" --profile founder
./bin/ctxt find "tech stack" --profile engineer
```

### 6. Generate Compositions

Create briefs, plans, and summaries:

```bash
# Generate brief
./bin/ctxt make brief --tag ux,onboarding

# Generate plan
./bin/ctxt make plan --mention @project.signup-redesign

# Generate summary since date
./bin/ctxt make summary --since 2025-01-01

# Save to file
./bin/ctxt make draft --tag launch -o launch-plan.md
```

### 7. Manage Registries

Work with knowledge registries:

```bash
# List registries
./bin/ctxt registry list

# Add registry
./bin/ctxt registry add uxpatterns https://uxpatterns.example.com

# Show registry info
./bin/ctxt registry info uxpatterns

# Sync registry
./bin/ctxt registry sync uxpatterns

# Remove registry
./bin/ctxt registry remove uxpatterns
```

### 8. Work with Entities

Query and inspect canonical entities:

```bash
# List entities
./bin/ctxt entity list

# Show entity details
./bin/ctxt entity show ui.best-practice

# Search entities
./bin/ctxt entity search "checkout"

# Show backlinks
./bin/ctxt entity backlink ui.best-practice
```

## Shell Completion

Enable shell completion for better CLI experience:

### Bash

```bash
# Load for current session
source <(./bin/ctxt completion bash)
source <(./bin/dpkms completion bash)

# Install permanently
./bin/ctxt completion bash > /usr/local/etc/bash_completion.d/ctxt
./bin/dpkms completion bash > /usr/local/etc/bash_completion.d/dpkms
```

### Zsh

```bash
# Enable completion system (if not already)
echo "autoload -U compinit; compinit" >> ~/.zshrc

# Install completions
./bin/ctxt completion zsh > "${fpath[1]}/_ctxt"
./bin/dpkms completion zsh > "${fpath[1]}/_dpkms"

# Restart shell
exec zsh
```

### Fish

```bash
# Install completions
./bin/ctxt completion fish > ~/.config/fish/completions/ctxt.fish
./bin/dpkms completion fish > ~/.config/fish/completions/dpkms.fish
```

## Common Workflows

### Workflow 1: Quick Note Capture

```bash
# Capture quick thoughts
echo "Remember to review onboarding metrics" | ./bin/ctxt analyze --hints "#todo"

# Capture with context
echo "Authentication pattern from competitor" | ./bin/ctxt analyze \
  --hints "#research #competitor" \
  --mentions "@auth.best-practice"
```

### Workflow 2: Research Collection

```bash
# Analyze multiple URLs
./bin/ctxt analyze https://example.com/article1 --profile research
./bin/ctxt analyze https://example.com/article2 --profile research
./bin/ctxt analyze https://example.com/article3 --profile research

# Generate research brief
./bin/ctxt make brief --profile research --since 2025-01-26
```

### Workflow 3: Project Documentation

```bash
# Analyze project resources
./bin/ctxt analyze --file docs/architecture.md --mentions "@project.platform"
./bin/ctxt analyze https://github.com/org/repo --mentions "@project.platform"

# Generate project brief
./bin/ctxt make plan --mention @project.platform
```

### Workflow 4: Daily Review

```bash
# List recent additions
./bin/ctxt list --after $(date -u -d '1 day ago' +%Y-%m-%d) --sort recent

# Search for specific topics
./bin/ctxt find "decisions made today"

# Generate daily summary
./bin/ctxt make summary --since $(date -u +%Y-%m-%d)
```

## Database Maintenance

Keep your database optimized:

```bash
# Vacuum database (reclaim space)
./bin/dpkms housekeeping vacuum

# Rebuild indexes
./bin/dpkms housekeeping reindex

# Compact database
./bin/dpkms housekeeping compact

# Prune old data
./bin/dpkms housekeeping prune --before 2024-01-01
```

## Troubleshooting

### Check Configuration

```bash
# Show current configuration
./bin/ctxt config show

# Validate configuration
./bin/ctxt config validate

# Show config file path
./bin/ctxt config path
```

### Check Version

```bash
# Check ctxt version
./bin/ctxt version

# Check dpkms version
./bin/dpkms version
```

### Common Issues

**Issue: Config file not found**
```bash
# Create config directory
mkdir -p ~/.config/contexthelp

# Copy example config
cp config/config.example.yaml ~/.config/contexthelp/config.yaml
```

**Issue: Permission denied**
```bash
# Make binaries executable
chmod +x bin/ctxt bin/dpkms
```

**Issue: Data directory not found**
```bash
# Create data directory
mkdir -p ~/.local/share/contexthelp
```

## Next Steps

1. Explore the [CLI API documentation](./ctxt/api-cli.md)
2. Read the [design documentation](./design.md)
3. Learn about focus profiles in [ctxt configuration](./ctxt/configuration.md)
4. Understand [pipeline configuration](./ctxt/pipelines.md)
5. Check out the [plugin system](./plugins.md)

## Getting Help

```bash
# General help
./bin/ctxt --help
./bin/dpkms --help

# Command-specific help
./bin/ctxt analyze --help
./bin/ctxt job --help
./bin/dpkms serve --help
```

## Development

For developers working on the CLI:

```bash
# Build with version info
task build

# Run tests
task test

# Lint code
task lint

# Format code
task fmt

# Clean build artifacts
task clean
```

See [CLI Implementation Guide](./cli-implementation.md) for detailed implementation notes.
