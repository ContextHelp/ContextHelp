# Task Breakdown by Sprint

This directory contains detailed task lists for each Living Skeleton sprint, extracted from the sprint documentation in `docs/sprints/`.

## Structure

- `000-shared-kernel.md` - Foundation: schema, domain model, interfaces
- `001-echo-loop.md` - First end-to-end flow
- `002-real-data-queries.md` - LLM integration and query language
- `003-beyond-text.md` - Multimodal ingestion and semantic identity
- `004-interfaces-independence.md` - REST/gRPC APIs and agent profiles
- `005-semantics-sidecars.md` - Vector search and plugin system
- `006-polish-multimedia.md` - Production stability and multimedia
- `007-sovereign-scalable.md` - Offline AI and scale optimization
- `008-trust-automation.md` - Security, permissions, and automation

## Legend

- `[PLAN]` = Needs breakdown into smaller, actionable tasks
- 📦 *dPKMS* = Task owned by dPKMS substrate package
- 📦 *ctxt* = Task owned by ctxt brain package
- 📦 *dPKMS + ctxt* = Cross-package integration task
- `(ADR-NNN)` = References an Architectural Decision Record

## Usage

Each file follows this structure:

1. **Package Focus** - Primary package ownership percentage
2. **Tasks by Team** - Organized by responsible team
3. **Integration Check** - End-to-end validation scenarios
4. **Risks** - Known challenges to watch for

## Related Documentation

- Sprint specifications: `docs/sprints/`
- Cross-package contracts: `docs/sprints/CROSS-PACKAGE-CONTRACTS.md`
- Package placement guide: `docs/dpkms-or-ctxt.md`
- High-level roadmap: `ROADMAP.md`
