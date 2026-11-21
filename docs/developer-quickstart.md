# Developer Quickstart

Get up and running with **dPKMS + `ctxt`** development in under 5 minutes.

---

## Prerequisites

### Required

- **Go 1.26.1+** — [Download](https://go.dev/dl/)
- **Git** — For version control
- **make** — Build and task runner (`make help` lists targets)

### Optional (but recommended)

- **golangci-lint** — Fast linter ([Install](https://golangci-lint.run/usage/install/))
- **Docker** — For the optional Postgres service

### Media Processing Dependencies

Without these, the system falls back to stubs that return placeholder data.

**FFmpeg** — Video frame extraction, audio extraction, format probing

```bash
# macOS
brew install ffmpeg

# Ubuntu/Debian
sudo apt install ffmpeg

# Verify
ffmpeg -version && ffprobe -version
```

**Tesseract** — OCR for images and video frames

```bash
# macOS
brew install tesseract

# Ubuntu/Debian
sudo apt install tesseract-ocr

# Additional language packs (optional, default is English)
brew install tesseract-lang          # macOS (all languages)
sudo apt install tesseract-ocr-fra   # Ubuntu (French, etc.)

# Verify
tesseract --version
```

**Whisper** — Audio and video transcription

```bash
# macOS (whisper.cpp via Homebrew)
brew install whisper-cpp

# Or via pip (slower but easier)
pip install openai-whisper

# Verify
whisper-cpp --help
# or
whisper --help
```

**pdftotext** — PDF text extraction (faster than pure-Go fallback)

```bash
# macOS
brew install poppler

# Ubuntu/Debian
sudo apt install poppler-utils

# Verify
pdftotext -v
```

**pyannote** — Speaker diarization (who said what in audio/video)

```bash
# Requires Python 3.8+ and a Hugging Face token
pip install pyannote.audio

# Verify
python -c "import pyannote.audio; print('ok')"
```

**Ollama** — Local LLM, vision analysis, and embeddings

```bash
# macOS / Linux
curl -fsSL https://ollama.com/install.sh | sh

# Pull required models
ollama pull llava          # Vision analysis
ollama pull nomic-embed-text  # Embeddings
ollama pull llama3         # LLM (tagging, summaries)

# Verify
ollama list
```

#### What runs without optional dependencies

| Feature | Without deps | With deps |
|---------|-------------|-----------|
| Video frame extraction | stub (no frames) | FFmpeg |
| Image OCR | stub (placeholder text) | Tesseract |
| Audio/video transcription | stub (placeholder text) | Whisper |
| PDF text extraction | pure-Go (golib) | pdftotext |
| Speaker diarization | stub | pyannote |
| Vision analysis | stub | Ollama + llava |
| Embeddings | stub | Ollama + nomic-embed-text |

---

## Quick Setup (Automated)

The fastest way to get started:

```bash
# Clone the repository
git clone https://github.com/ideacrafterslabs/ctxt.git
cd ctxt

# Run automated setup script
./scripts/dev-setup.sh

# Build both binaries
make build

# Verify installation
./bin/ctxt version
./bin/dpkms version
```

**Done!** Skip to [First Commands](#first-commands).

---

## Manual Setup

If you prefer manual control:

### 1. Clone Repository

```bash
git clone https://github.com/ideacrafterslabs/ctxt.git
cd ctxt
```

### 2. Install Dependencies

```bash
# Download Go dependencies
go mod download
go mod tidy
go mod verify
```

### 3. Build Binaries

```bash
# Using make
make build

# Or manually
go build -o bin/ctxt ./cmd/ctxt
go build -o bin/dpkms ./cmd/dpkms
```

### 4. Install Development Tools (Optional)

```bash
# Toolchain (Go, golangci-lint, pnpm) at the mise.toml pins
mise trust && mise install
```

`make lint`, `make gosec` and `make govulncheck` run their tools at the
pinned versions; no separate install step.

### 5. Environment Overrides (Optional)

The defaults work for local development, so this step is optional.
Nothing loads a `.env` file automatically: not `ctxt`, `dpkms`, `make`
nor the compose files. To keep overrides in one, copy the template
and export it into your shell:

```bash
cp .env.example .env
$EDITOR .env               # uncomment what you need
set -a; . ./.env; set +a   # export it into the current shell
```

---

## First Commands

### Test the CLI

```bash
# ctxt - the agentic context brain
./bin/ctxt --help

# dpkms - the knowledge substrate
./bin/dpkms --help
```

### Run Tests

```bash
make test

# Or directly
go test -v ./...
```

### Start Development Server

```bash
make build-dpkms
./bin/dpkms serve
```

The dPKMS server will start on:
- REST API: `http://localhost:8080`
- gRPC API: `localhost:9090`

---

## Development Workflow

### Using `make`

```bash
# List all targets
make help

# Run linters (golangci-lint + gosec)
make lint

# Format code
make fmt

# Scan dependencies for vulnerabilities
make vuln-scan
```

## Project Structure

```
ctxt/
├── cmd/
│   ├── dpkms/      # dPKMS server entry point
│   └── ctxt/       # ctxt CLI entry point
├── internal/       # Private application code
│   ├── dpkms/      # dPKMS substrate implementation
│   └── ctxt/       # ctxt brain implementation
├── pkg/            # Public libraries
├── data/           # Local data (SQLite, migrations, logs)
├── config/        # Configuration files
├── test/           # Tests
│   ├── integration/
│   ├── smoke/
│   ├── testdata/
│   └── testutil/
├── docs/           # Documentation
├── .env.example    # Environment configuration template
├── Makefile        # Build, test, lint and CI targets
└── go.mod          # Go module definition
```

---

## Configuration

### Default Configuration (Zero-Config)

The system works out-of-the-box with sensible defaults:
- **Storage:** SQLite at `$XDG_DATA_HOME/contexthelp/db.sqlite`
- **Workers:** 4 background workers
- **Jobs:** Persistent queue in SQLite
- **API:** REST on `:8080`, gRPC on `:9090`

### Custom Configuration

Export overrides in the shell (neither binary loads a `.env` file):

```bash
# Storage
CH_STORAGE_TYPE=sqlite

# Workers (config key server.workers)
DPKMS_WORKERS=4

# API
CH_SERVER_PORT=8080
CH_GRPC_PORT=9090
```

The database path is the `storage.path` config key (`-c storage.path=...`);
logging has no env var: pass `--verbose` for debug output.

See [environment-variables/README.md](environment-variables/README.md) for the complete reference.

---

## Optional Services

For advanced development, start optional services:

```bash
# Start Postgres via Docker Compose
make services-up

# Stop services (services-clean also drops volumes)
make services-down
```

Services:
- **PostgreSQL** - Alternative storage backend (`:5432`)

---

## Common Tasks

### Add a Database Migration

Migrations are embedded in the binaries and run automatically whenever
dpkms or ctxt opens the database. To add one, see
[Database & Migrations](development.md#database--migrations).

### Lint and Format Code

```bash
# Run linters
make lint

# Lint only what changed since the merge base
make lint-new

# Format code
make fmt

# Run go vet
make ci-vet
```

### Run CI Checks Locally

```bash
# Run every CI job locally, in ci.yml order
make ci

# Or individually
make ci-lint
make ci-security
make ci-test
make ci-build
```

---

## Troubleshooting

### `go mod` Issues

```bash
# Clean and re-download modules
go clean -modcache
go mod download
go mod tidy
```

### Build Failures

```bash
# Clean build artifacts
make clean

# Rebuild from scratch
make build
```

### Database Locked

Another dpkms instance usually holds the database:

```bash
dpkms ps         # list running instances
dpkms shutdown   # stop the running instance
```

### Port Already in Use

```bash
# Start on other ports (or --port / --grpc-port)
CH_SERVER_PORT=8081 CH_GRPC_PORT=9091 dpkms serve
```

---

## Next Steps

**For Contributors:**
1. Read [development.md](development.md) — Complete development guide
2. Review [architecture.md](architecture.md) — System architecture
3. Check [dpkms/README.md](dpkms/README.md) and [ctxt/README.md](ctxt/README.md)

**For Users:**
1. Read [ctxt/api-cli.md](ctxt/api-cli.md) — CLI usage
2. Try [ctxt/user-story.md](ctxt/user-story.md) — Daily usage narrative

**For DevOps:**
1. Review [ci-cd.md](ci-cd.md) — CI/CD pipeline
2. Check [scaling.md](scaling.md) — Deployment patterns
3. Read [environment-variables/README.md](environment-variables/README.md) — Environment variable reference

---

## Getting Help

**Documentation:**
- [docs/](.) — Full documentation index
- [domains.md](domains.md) — All 40 domains cataloged

**Community:**
- GitHub Issues — Bug reports and feature requests
- Discussions — Questions and ideas

---

## Quick Reference Card

```bash
# Build
make build              # Build both binaries

# Test
make test               # Run CI's test jobs
make test-smoke         # Binary smoke tests
make test-cover         # With coverage report

# Run
./bin/dpkms serve       # Start dPKMS server
./bin/ctxt analyze ...  # Capture knowledge

# Quality
make check              # Primary local gate
make lint               # Lint code
make vuln-scan          # Dependency vulnerability scan

# Clean
make clean              # Remove build artifacts

# Services
make services-up        # Start Docker services
make services-down      # Stop Docker services
```

---

**You're ready to contribute!** 🚀

For complete development documentation, see [development.md](development.md).
