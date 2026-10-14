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

### Create Configuration Files

Each binary reads its own file: `ctxt` reads `ctxt.yaml`, `dpkms` reads
`dpkms.yaml`. Both live in `$XDG_CONFIG_HOME/contexthelp/` (default
`~/.config/contexthelp/`). Both binaries run with built-in defaults when no file
exists, so this step is optional.

The fastest way to create the client file is the setup wizard, which writes
`~/.config/contexthelp/ctxt.yaml` (or `$CTXT_CONFIG` when set):

```bash
ctxt setup                    # interactive
ctxt setup --non-interactive  # write defaults without prompting
```

Or copy the template and edit it. `config/config.example.yaml` covers the keys of
both binaries; copy it once per binary you run:

```bash
cp config/config.example.yaml ~/.config/contexthelp/ctxt.yaml    # client (ctxt)
cp config/config.example.yaml ~/.config/contexthelp/dpkms.yaml   # daemon (dpkms)
$EDITOR ~/.config/contexthelp/ctxt.yaml
```

Validate the result. `ctxt config validate` checks the effective ctxt
configuration (every file layer, env var and `-c` override) and exits 1 on errors:

```bash
ctxt config validate
ctxt config paths      # print the resolution chain and which files exist
dpkms config paths     # same for dpkms
```

See [ctxt configuration](./ctxt/configuration.md) for the file cascade and keys.

### Environment Variables (Optional)

You can override configuration using environment variables:

See [environment variables](./environment-variables/README.md) for the full list.

```bash
# Data directory (database defaults to $CTXT_DATA_DIR/db.sqlite)
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
./bin/dpkms serve                   # foreground (Ctrl-C to stop)
./bin/dpkms serve --daemon          # background daemon (detached from terminal)
./bin/dpkms serve --name work       # named instance for multi-instance setups
```

This starts:
- Background job worker for processing ingestion
- REST API on port 8080 (a free port if 8080 is busy; `--port`, `CH_SERVER_PORT` or `server.port` requires that exact port)
- gRPC API on port 9090 (a free port if 9090 is busy; `--grpc-port`, `CH_GRPC_PORT` or `server.grpc_port` requires that exact port)
- Browser-extension cookie bridge on `127.0.0.1:9377` (a free port if 9377 is busy; `--cookie-bridge-port`, `CH_COOKIE_BRIDGE_PORT` or `server.cookie_bridge_port` requires that exact port)

Manage running instances:

```bash
./bin/dpkms ps                      # list running instances
./bin/dpkms shutdown                # graceful stop
```

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
./bin/ctxt analyze "Checkout button is hidden on mobile" \
  --hint ux,critical \
  --mention "@ui.best-practice"

# Analyze a URL
./bin/ctxt analyze "https://example.com/article" --type url

# Analyze an image using a file
./bin/ctxt --file screenshot.png --type image

# Wait for job completion
./bin/ctxt "Important note" --wait
```

### 3. Manage Jobs

Monitor and control ingestion jobs with `dpkms job` (ctxt has no job command):

```bash
# List all jobs
./bin/dpkms job list

# Check job status
./bin/dpkms job status job_12345678

# View job logs
./bin/dpkms job log job_12345678

# Retry failed job
./bin/dpkms job retry job_12345678

# Cancel a pending or running job
./bin/dpkms job cancel job_12345678
```

### 4. Query Knowledge

Search and query your knowledge base:

```bash
# List all objects
./bin/ctxt list

# Filter by one tag
./bin/ctxt list --tagged ux

# Several tags at once (RSQL query)
./bin/ctxt list --q "tag=in=(ux,onboarding)"

# Filter by type
./bin/ctxt list --type url

# Hybrid search (FTS + vector, default)
./bin/ctxt find "authentication best practices"

# FTS-only or vector-only
./bin/ctxt find "authentication" --fts
./bin/ctxt find "authentication" --semantic

# See every candidate the search considered, as an interactive graph
# (Ctrl-C to stop; see manual/workflows/search-graph.md)
./bin/ctxt find "authentication" --graph

# View object details
./bin/ctxt show obj_12345678
```

### 5. Use Focus Profiles

Manage focus profiles for contextual filtering:

```bash
# List profiles
./bin/ctxt profile list

# View profile details
./bin/ctxt profile view founder

# Create a profile (adds a placeholder entry to the resolved config file)
./bin/ctxt profile create myproject

# Set default profile
./bin/ctxt profile set founder

# Clear the default profile
./bin/ctxt profile unset founder

# Use profile with commands
./bin/ctxt analyze "Strategic insight" --profile founder
./bin/ctxt find "tech stack" --profile engineer
```

### 6. Generate Compositions

Create briefs, plans, and summaries:

```bash
# Generate brief
./bin/ctxt compose brief --tagged ux

# Generate plan
./bin/ctxt compose plan --mention @project.signup-redesign

# Generate summary of objects created since a date
./bin/ctxt compose summary --since 2025-01-01

# Save to file
./bin/ctxt compose draft --tagged launch --output-file launch-plan.md
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
./bin/ctxt registry delete uxpatterns
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

### 9. Background Watchers and Browser Capture

`ctxt watch` runs passive watchers on this machine: the clipboard (opt-in) and the
directories listed in `watch.dirs`. New or changed content is enqueued on the
dpkms instance ctxt resolves.

```bash
ctxt watch enable clipboard
ctxt watch start
ctxt watch status
ctxt watch stop
```

Browser tabs and history are captured explicitly, per browser profile:

```bash
ctxt capture browsers          # list installed Chromium browsers and profiles
ctxt capture tabs              # capture the open tabs of one profile
ctxt capture history           # capture the browsing history of one profile
ctxt capture schedule --help   # run tab/history capture on a timer (macOS LaunchAgent)
```

Not implemented: `ctxt capture --ambient` (and `--input`, `--skip`, `--window`)
is reserved and does nothing yet. There is no `ctxd` binary, no meeting
recorder, and no `ctxt session` or `ctxt mcp` command.

#### Agent integration (MCP)

A running `dpkms serve` exposes an MCP endpoint at `/api/v1/mcp` on its REST
port, behind the same inbound auth as the rest of `/api/v1`. It serves two
read-only tools: `search` and `schema`. Point an MCP client that speaks
JSON-RPC over HTTP at `http://127.0.0.1:8080/api/v1/mcp`.

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
echo "Remember to review onboarding metrics" | ./bin/ctxt analyze --hint todo

# Capture with context
echo "Authentication pattern from competitor" | ./bin/ctxt analyze \
  --hint research,competitor \
  --mention "@auth.best-practice"
```

### Workflow 2: Research Collection

```bash
# Analyze multiple URLs
./bin/ctxt analyze https://example.com/article1 --type url --profile research
./bin/ctxt analyze https://example.com/article2 --type url --profile research
./bin/ctxt analyze https://example.com/article3 --type url --profile research

# Generate research brief
./bin/ctxt compose brief --profile research --since 2025-01-26
```

### Workflow 3: Project Documentation

```bash
# Analyze project resources
./bin/ctxt analyze --file docs/architecture.md --mention "@project.platform"
./bin/ctxt analyze https://github.com/org/repo --type url --mention "@project.platform"

# Generate project plan
./bin/ctxt compose plan --mention @project.platform
```

### Workflow 4: Daily Review

```bash
# List recent additions
./bin/ctxt list --after $(date -u -d '1 day ago' +%Y-%m-%d) --sort recent

# Search for specific topics
./bin/ctxt find "decisions made today"

# Generate daily summary
./bin/ctxt compose summary --since $(date -u +%Y-%m-%d)
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

# Validate configuration (exit 1 on errors)
./bin/ctxt config validate

# Lint configuration: schema, secrets, permissions, deprecated keys
./bin/ctxt config doctor

# Show the config file ctxt loads, and the full resolution chain
./bin/ctxt config path
./bin/ctxt config paths
./bin/dpkms config paths
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

`ctxt config path` exits 1 when no file exists; both binaries still run on
defaults. To create the files:
```bash
mkdir -p ~/.config/contexthelp
ctxt setup --non-interactive                                     # writes ctxt.yaml
cp config/config.example.yaml ~/.config/contexthelp/dpkms.yaml   # daemon file
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
5. Extend processing with [custom pipelines and steps](./ctxt/pipelines.md#extending-pipelines)

## Getting Help

```bash
# General help
./bin/ctxt --help
./bin/dpkms --help

# Command-specific help
./bin/ctxt analyze --help
./bin/dpkms job --help
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
