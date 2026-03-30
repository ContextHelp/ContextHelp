# CLI Implementation Guide

This document describes the Go CLI implementation for ContextHelp using Cobra and Viper.

## Overview

ContextHelp provides two command-line interfaces:

- **`ctxt`** - User-facing commands for capture, search, composition, and profiles
- **`dpkms`** - Infrastructure commands for server operations and maintenance

Both CLIs are built with:
- [Cobra](https://github.com/spf13/cobra) for command structure and routing
- [Viper](https://github.com/spf13/viper) for configuration management
- Shared configuration package for consistency

## Project Structure

```
.
├── cmd/
│   ├── ctxt/
│   │   ├── main.go           # Entry point for ctxt
│   │   └── cmd/
│   │       ├── root.go       # Root command and global flags
│   │       ├── analyze.go    # Content ingestion
│   │       ├── jobs.go       # Job management
│   │       ├── list.go       # List knowledge objects
│   │       ├── find.go       # Semantic search
│   │       ├── open.go       # View object details
│   │       ├── delete.go     # Delete objects
│   │       ├── edit.go       # Edit object metadata
│   │       ├── profile.go    # Focus profile management
│   │       ├── make.go       # Composition generation
│   │       ├── config.go     # Configuration management
│   │       ├── registry.go   # Registry management
│   │       ├── entities.go   # Entity operations
│   │       ├── version.go    # Version information
│   │       └── completion.go # Shell completion
│   │
│   └── dpkms/
│       ├── main.go           # Entry point for dpkms
│       └── cmd/
│           ├── root.go       # Root command
│           ├── serve.go      # Start server and worker
│           ├── housekeeping.go # DB maintenance
│           ├── version.go    # Version information
│           └── completion.go # Shell completion
│
└── internal/
    └── config/
        └── config.go         # Shared configuration package
```

## Configuration System

### Configuration File

Default location: `~/.config/contexthelp/config.yaml`

Example configuration:

```yaml
storage:
  type: sqlite
  path: ~/.local/share/contexthelp/db.sqlite

server:
  port: 8080
  grpc_port: 9090
  workers: 4
  public: false

profile:
  default: founder

registries:
  - name: uxpatterns
    url: https://uxpatterns.example.com

plugins:
  - type: plugin
    plugin: ./plugins/screensuggester.so
    config:
      enabled: true

i18n:
  enabled: false
  preferred_languages:
    - en
  auto_translate: false
  translate_tags: false
```

### Environment Variables

Configuration can be overridden using environment variables:

- `CTXT_CONFIG` - Config file path
- `CTXT_DATA_DIR` - Data directory
- `CTXT_PROFILE` - Default focus profile
- `DPKMS_DATA_DIR` - dPKMS data directory
- `DPKMS_WORKERS` - Worker thread count
- `CH_STORAGE_TYPE` - Storage backend type
- `CH_SERVER_PORT` - HTTP port
- `CH_GRPC_PORT` - gRPC port

### Flag Binding

All command flags are bound to Viper configuration keys, allowing:
- Command-line flags to override config file
- Environment variables to override config file
- Precedence: CLI flags > Environment > Config file > Defaults

Example from `analyze.go`:

```go
analyzeCmd.Flags().String("type", "auto", "input type")
viper.BindPFlag("analyze.type", analyzeCmd.Flags().Lookup("type"))
```

## Building

### Using Task

```bash
# Build both binaries
task build

# Build individual binaries
task build:ctxt
task build:dpkms

# Install to $GOPATH/bin
task install

# Clean build artifacts
task clean

# Run tests
task test

# Format code
task fmt
```

### Using Go directly

```bash
# Build ctxt
go build -o bin/ctxt cmd/ctxt/main.go

# Build dpkms
go build -o bin/dpkms cmd/dpkms/main.go
```

### With version information

```bash
VERSION=0.4.0 BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S') \
  go build -ldflags "\
    -X main.Version=$VERSION \
    -X main.BuildTime=$BUILD_TIME \
    -X main.GitCommit=$(git rev-parse --short HEAD)" \
  -o bin/ctxt cmd/ctxt/main.go
```

## Usage Examples

### ctxt Commands

```bash
# Analyze content
echo "Fix signup flow" | ctxt analyze --type text --hints "#ux #bad"
ctxt analyze --file screenshot.png --type image
ctxt analyze https://example.com --wait

# Manage jobs
ctxt job list
ctxt job status job_12345678
ctxt job retry job_12345678

# Query knowledge
ctxt list --tag ux,onboarding --limit 10
ctxt find "authentication best practices"
ctxt open obj_12345678

# Profiles
ctxt profile list
ctxt profile show founder
ctxt profile create myproject --config profile.yaml

# Compositions
ctxt make brief --tag ux,onboarding
ctxt make plan --mention @project.signup-redesign

# Configuration
ctxt config show
ctxt config path
ctxt config validate

# Registries
ctxt registry list
ctxt registry add uxpatterns https://uxpatterns.example.com
ctxt registry info uxpatterns
ctxt registry sync uxpatterns
ctxt registry remove uxpatterns

# Entities
ctxt entity list
ctxt entity show ui.best-practice
ctxt entity backlink ui.best-practice
```

### dpkms Commands

```bash
# Start server
dpkms serve
dpkms serve --port 8080 --workers 8 --public

# Database maintenance
dpkms housekeeping vacuum
dpkms housekeeping reindex
dpkms housekeeping compact
dpkms housekeeping prune --before 2024-01-01

# Version
dpkms version
```

## Shell Completion

Generate completion scripts for your shell:

```bash
# Bash
ctxt completion bash > /usr/local/etc/bash_completion.d/ctxt
dpkms completion bash > /usr/local/etc/bash_completion.d/dpkms

# Zsh
ctxt completion zsh > "${fpath[1]}/_ctxt"
dpkms completion zsh > "${fpath[1]}/_dpkms"

# Fish
ctxt completion fish > ~/.config/fish/completions/ctxt.fish
dpkms completion fish > ~/.config/fish/completions/dpkms.fish
```

## Implementation Notes

### Command Structure

Each command is implemented as a separate file in the `cmd/` directory. Commands follow this pattern:

```go
var myCmd = &cobra.Command{
    Use:   "mycommand [args]",
    Short: "Brief description",
    Long:  "Detailed description with examples",
    RunE:  runMyCommand,
}

func init() {
    rootCmd.AddCommand(myCmd)

    // Define flags
    myCmd.Flags().String("flag", "default", "description")

    // Bind to Viper
    viper.BindPFlag("mycommand.flag", myCmd.Flags().Lookup("flag"))
}

func runMyCommand(cmd *cobra.Command, args []string) error {
    // Get flag value from Viper
    flagValue := viper.GetString("mycommand.flag")

    // Implementation
    return nil
}
```

### Error Handling

- Commands return errors that are handled by Cobra
- `SilenceUsage: true` prevents usage from printing on errors
- `SilenceErrors: true` allows custom error formatting

### Configuration Loading

Configuration is loaded in `cobra.OnInitialize()`:

```go
func initConfig() {
    var err error
    cfg, err = config.Load(cfgFile)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
        cfg = &config.Config{}
    }

    config.EnsureConfigDir()
    config.EnsureDataDir()
}
```

### Shared Configuration Package

The `internal/config` package provides:
- Configuration struct definitions
- Loading from file with Viper
- Environment variable binding
- Default value management
- Directory creation utilities

## Next Steps

To complete the implementation:

1. **Add business logic**: Replace TODO comments with actual implementation
2. **Add storage layer**: Implement SQLite/Postgres storage backends
3. **Add job queue**: Implement job queue and worker pool
4. **Add pipeline runtime**: Implement pipeline execution engine
5. **Add HTTP/gRPC servers**: Implement REST and gRPC APIs
6. **Add tests**: Add unit and integration tests
7. **Add documentation**: Complete inline documentation

## References

- [Cobra Documentation](https://cobra.dev/)
- [Viper Documentation](https://github.com/spf13/viper)
- [ContextHelp CLI API Spec](./ctxt/api-cli.md)
- [ContextHelp Design Documentation](./design.md)
