---
title: "IBR Browser Automation Integration"
tracks:
  - ibr-browser-20260403
tasks:
  - title: "Add BrowserConfig and BrowserCookieConfig structs to internal/config/config.go"
    effort: S
    priority: P1
    tags: [phase:1, config, browser]

  - title: "Create internal/browser/daemon.go — IBR daemon HTTP client"
    effort: M
    priority: P1
    tags: [phase:1, browser, client]

  - title: "Create internal/browser/manager.go — daemon process manager"
    effort: M
    priority: P1
    tags: [phase:1, browser, manager]
    blocked-by: [1]

  - title: "Create internal/pipeline/steps/ibr_fetcher.go — ibr_fetcher pipeline step"
    effort: M
    priority: P2
    tags: [phase:2, pipeline, browser]
    blocked-by: [1]

  - title: "Register ibr_fetcher in internal/pipeline/builtins/builtins.go"
    effort: S
    priority: P2
    tags: [phase:2, pipeline, builtins]
    blocked-by: [3]

  - title: "Create internal/pipeline/builtins/url_interactive.go — url.interactive pipeline"
    effort: S
    priority: P2
    tags: [phase:2, pipeline, builtins]
    blocked-by: [4]

  - title: "Create internal/pipeline/builtins/url_authenticated.go — url.authenticated pipeline"
    effort: S
    priority: P2
    tags: [phase:2, pipeline, builtins]
    blocked-by: [4]

  - title: "Add browser to CapabilitiesFromOpts() when BrowserClient != nil"
    effort: S
    priority: P2
    tags: [phase:2, pipeline, capabilities]
    blocked-by: [4]

  - title: "Wire browser manager into cmd/dpkms/cmd/serve.go with --browser flag"
    effort: M
    priority: P3
    tags: [phase:3, wiring, serve]
    blocked-by: [2, 4]

  - title: "Update internal/pidfile/ to include BrowserPort field"
    effort: S
    priority: P3
    tags: [phase:3, pidfile]
    blocked-by: [8]

  - title: "Create internal/browser/integration_test.go — full mock daemon integration test"
    effort: L
    priority: P3
    tags: [phase:3, testing, integration]
    blocked-by: [8]
---

# Implementation Plan: IBR Browser Automation Integration

Track ID: `ibr-browser_20260403`
Created: 2026-04-03
Status: pending

## Overview

Integrate IBR as a built-in browser automation backend for dPKMS/ctxt. Go manages IBR as a subprocess daemon, communicates via HTTP JSON API, and exposes browser-based fetching as pipeline steps with capability gating.

Detailed implementation code: `docs/plans/2026-04-03-ibr-browser-integration.md`

## Phase 1: Foundation (Config + Client + Manager)

### Tasks

- [ ] **Task 1.1**: Add `BrowserConfig` and `BrowserCookieConfig` structs to `internal/config/config.go`
  - Add defaults in `setDefaults()` and env bindings in `bindEnvVars()`
  - Write test `TestBrowserConfigDefaults`
- [ ] **Task 1.2**: Create `internal/browser/daemon.go` — IBR daemon HTTP client
  - Types: `HealthResponse`, `TokenUsage`, `ExecuteResult`, `Client`
  - Methods: `NewClient()`, `Health()`, `Execute()`
  - Write tests: `TestClient_Health_ReturnsStatus`, `TestClient_Execute_SendsCommandAndReturnsExtracts`
- [ ] **Task 1.3**: Create `internal/browser/manager.go` — daemon process manager
  - Types: `Manager`
  - Methods: `NewManager()`, `Start()`, `Stop()`, `IsRunning()`, `Client()`, `Port()`, `PID()`
  - Write tests: `TestManager_IsRunning_FalseWhenNoClient`, `TestManager_IsRunning_TrueWhenHealthy`, `TestManager_Client_ReturnsNilWhenDisabled`

### Verification

- [ ] **Verify 1.1**: `go test ./internal/config/ -run TestBrowserConfig -v` passes
- [ ] **Verify 1.2**: `go test ./internal/browser/ -v` passes
- [ ] **Verify 1.3**: `go build ./cmd/dpkms/` compiles (no wiring yet, just verify no import cycles)

## Phase 2: Pipeline Integration (Step + Builtins + Capabilities)

### Tasks

- [ ] **Task 2.1**: Create `internal/pipeline/steps/ibr_fetcher.go` — `ibr_fetcher` pipeline step
  - Implements `PipelineStep` interface with `browser` capability
  - Reads URL from `Source`, optional instructions from `Metadata["ibr_instructions"]`
  - Cookie config from `Metadata["ibr_cookies"]`
  - Write tests: `TestIBRFetcher_Name`, `TestIBRFetcher_Contract`, `TestIBRFetcher_Run_FetchesViaIBRDaemon`, `TestIBRFetcher_Run_NilClient_ReturnsError`
- [ ] **Task 2.2**: Register `ibr_fetcher` in `internal/pipeline/builtins/builtins.go`
  - Add to `stepConstructors` (nil client fallback)
  - Add `browserStepConstructors` map
  - Add `BrowserClient` field to `BuildOpts`
  - Update `resolveStep()` to check browser constructors
- [ ] **Task 2.3**: Create `internal/pipeline/builtins/url_interactive.go` — `url.interactive` pipeline
  - Steps: `ibr_fetcher`, `html_cleaner`, `typedetector`, `textcleaner`, `sectioner`, `tagger`, `entity_extractor`, `entity_resolver`, `embedding`
  - Providers: `["browser"]`
- [ ] **Task 2.4**: Create `internal/pipeline/builtins/url_authenticated.go` — `url.authenticated` pipeline
  - Same step chain as `url.interactive`
  - Providers: `["browser"]`
- [ ] **Task 2.5**: Add `browser` to `CapabilitiesFromOpts()` when `BrowserClient != nil`
  - Write test: `TestCapabilitiesFromOpts_IncludesBrowser`

### Verification

- [ ] **Verify 2.1**: `go test ./internal/pipeline/steps/ -run TestIBRFetcher -v` passes
- [ ] **Verify 2.2**: `go test ./internal/pipeline/builtins/ -v` passes
- [ ] **Verify 2.3**: Pipeline contract validation passes (no composability errors)

## Phase 3: Wiring + Integration Tests + Polish

### Tasks

- [ ] **Task 3.1**: Wire browser manager into `cmd/dpkms/cmd/serve.go`
  - Add `--browser` flag
  - Start manager after pipeline registry, before errgroup
  - Pass `BrowserClient` into `BuildOpts`
  - Stop manager in shutdown section
- [ ] **Task 3.2**: Update `internal/pidfile/` to include `BrowserPort` field
  - Write to pidfile after manager starts
  - Display in `dpkms ps` output
- [ ] **Task 3.3**: Create `internal/browser/integration_test.go` — full mock daemon integration test
  - Mock HTTP server simulating IBR daemon (health + command endpoints)
  - Test `ibr_fetcher` step end-to-end with custom instructions
  - Verify metadata population (source_url, ibr_token_usage, ibr_extracts)
- [ ] **Task 3.4**: Verify `go build ./cmd/dpkms/` compiles cleanly
- [ ] **Task 3.5**: Verify all tests pass: `go test ./internal/browser/ ./internal/config/ ./internal/pipeline/... -v`

### Verification

- [ ] **Verify 3.1**: `dpkms serve --help` shows `--browser` flag
- [ ] **Verify 3.2**: `go build ./cmd/dpkms/` succeeds
- [ ] **Verify 3.3**: Full test suite green

## Checkpoints

| Phase | Checkpoint SHA | Date | Status |
|-------|---------------|------|--------|
| Phase 1 | | | pending |
| Phase 2 | | | pending |
| Phase 3 | | | pending |
