# IBR Browser Automation Integration

## Overview

Integrate IBR (AI-powered browser automation) as a built-in capability in dPKMS/ctxt, enabling interactive web ingestion pipelines that handle JavaScript-rendered pages, login walls, pagination, cookie consent banners, and structured data extraction from dynamic content.

IBR is an internal project (`@hop/ibr`) that converts natural language instructions into browser actions via Playwright and multi-provider LLMs. It runs as a daemon with an HTTP API, making it a natural fit as a subprocess managed by `dpkms serve`.

## Functional Requirements

### FR-1: Browser Configuration

Config system (`config.yaml`) gains a `browser` section controlling enablement, binary path, port, concurrency, headless mode, AI provider, and cookie import settings.

- Acceptance: `BrowserConfig` struct exists, defaults work, env vars override, tests pass

### FR-2: IBR Daemon Client

Go HTTP client that communicates with IBR's daemon API (`GET /health`, `POST /command`).

- Acceptance: Client can health-check and execute tasks against a running daemon, with proper error handling for timeouts, 503s, and auth failures

### FR-3: Daemon Process Manager

Manages IBR daemon lifecycle: start as subprocess, wait for healthy, stop on shutdown. Generates random auth token, auto-assigns port.

- Acceptance: Manager starts daemon, verifies health within 15s, stops cleanly on SIGINT, cleans up on failure

### FR-4: `ibr_fetcher` Pipeline Step

New `PipelineStep` implementation that sends URLs to the IBR daemon for browser-based fetching. Supports custom instructions via metadata and falls back gracefully when browser is unavailable.

- Acceptance: Step fetches JS-rendered content, populates `RawContent` and `Metadata`, returns error when client is nil

### FR-5: Built-in Pipelines

Two new pipelines registered in the builtins system:
- `url.interactive` — browser-based fetch + standard enrichment chain
- `url.authenticated` — same but with cookie import for auth-walled pages

- Acceptance: Both pipelines resolve correctly, steps compose without contract violations

### FR-6: `dpkms serve` Integration

Browser manager wired into daemon startup/shutdown lifecycle. Optional `--browser` flag. Browser port recorded in pidfile.

- Acceptance: `dpkms serve --browser` starts IBR daemon, `dpkms shutdown` stops it, `dpkms ps` shows browser port

### FR-7: Capability Gating

`browser` capability added to the pipeline capability system. Steps requiring `browser` are pruned when `BrowserClient` is nil (browser disabled).

- Acceptance: `url.interactive` pipeline degrades gracefully (pruned) when browser is disabled; works when enabled

## Non-Functional Requirements

### NFR-1: Optional Heavy Dependency

Playwright/Chromium (~400MB) must not be required for users who don't need browser automation.

- Target: `browser.enabled: false` (default) means zero Chromium download, zero IBR subprocess
- Verification: Fresh install without browser config has no IBR-related processes or downloads

### NFR-2: Subprocess Isolation

IBR runs as a separate Node.js process. Go never embeds V8 or Playwright.

- Target: Communication exclusively via HTTP JSON API
- Verification: Process list shows separate `node` process for IBR daemon

### NFR-3: Token Cost Awareness

IBR uses LLM calls per action. Pipeline metadata must track token usage.

- Target: `ibr_token_usage` in KnowledgeObject metadata after ibr_fetcher runs
- Verification: Token usage recorded and queryable

## Acceptance Criteria

- [ ] `config.yaml` with `browser:` section loads correctly with defaults
- [ ] IBR daemon starts as subprocess when `browser.enabled: true`
- [ ] `ibr_fetcher` step fetches content from JS-rendered pages
- [ ] `url.interactive` pipeline is selectable via `--pipeline url.interactive`
- [ ] `url.authenticated` pipeline imports browser cookies
- [ ] `dpkms serve --browser` starts daemon; shutdown stops it
- [ ] Browser-dependent steps pruned when browser is disabled
- [ ] All tests pass including integration test with mock daemon
- [ ] `go build ./cmd/dpkms/` compiles without IBR installed

## Scope

### In Scope

- Config system extension
- Go HTTP client for IBR daemon API
- Process manager (start/stop/health)
- `ibr_fetcher` pipeline step
- `url.interactive` and `url.authenticated` builtin pipelines
- `browser` capability in pipeline system
- `dpkms serve` wiring and pidfile update
- Integration tests with mock HTTP server

### Out of Scope

- Auto-detection of URLs needing browser rendering (future: `url_probe` step)
- Per-domain pipeline routing rules
- IBR tool YAML → ctxt capture recipe mapping
- Web page change monitoring (`ctxt watch` + IBR)
- NDJSON event streaming into CloudEvents bus
- Cookie refresh/rotation lifecycle
- UI for browser automation configuration

## Dependencies

### Internal

- Existing pipeline system (`internal/pipeline/`, `pkg/pluginapi/`)
- Existing config system (`internal/config/`)
- Existing builtin registry (`internal/pipeline/builtins/`)
- Pidfile system (`internal/pidfile/`)
- `dpkms serve` command (`cmd/dpkms/cmd/serve.go`)

### External

- IBR (`@hop/ibr`) — must be installed and on PATH (or binary path configured)
- Node.js runtime (for IBR subprocess)
- Playwright + Chromium (installed via `npx playwright install chromium`)

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| IBR binary not found at runtime | Medium | Clear error message; `browser.enabled` defaults to false |
| Chromium crash takes down daemon | Medium | Manager detects unhealthy daemon, logs error; job retries |
| AI token cost in bulk pipelines | High | Token usage tracked in metadata; future: budget controls |
| Port conflicts | Low | Auto-assign port (port 0 → OS picks free port) |

## Open Questions

- [x] Built-in vs plugin? — Built-in with optional heavy deps (resolved in conversation)
- [ ] Should `url.generic` auto-fallback to `url.interactive` when HTTP fetch returns JS-only page?
- [ ] Cookie import: per-pipeline config or global browser config only?
