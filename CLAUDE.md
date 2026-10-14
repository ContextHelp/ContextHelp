# CLAUDE.md — Agent Instructions for ContextHelp (ctxt)

This file helps AI coding tools work effectively in the core `ctxt` workspace.

## Core Concepts

- **dPKMS (Substrate):** The execution, storage, and graph layer (`dpkms` binary).
- **ctxt (Brain):** The intelligence, pipeline, and behavior layer (`ctxt` binary).

## Key Commands

- `make build` — Build both `ctxt` and `dpkms`
- `make test` — Run all tests
- `task <task-name>` — Run specific Taskfile commands
- `dpkms serve` — Start the background worker and API
- `ctxt analyze <content>` — Process and ingest content

## File Conventions

- **Internal logic:** Keep in `internal/`.
- **Shared logic:** Use `pkg/`.
- **API definitions:** Go in `api/` or `contracts/`.
- **Pipelines:** Defined in `ctxt` and executed by `dPKMS`.

## Standards

- **Manifests:** Every new extension or skill must include a `manifest.json` following `.github/manifest.schema.json`; every new plugin a plugin manifest (`apiVersion: v1`, `docs/plugin-authoring-guide.md` §2).
- **Error Wrapping:** Use `fmt.Errorf("context: %w", err)`.
- **Logging:** Use structured logging (don't log sensitive info).
- **PRs:** Reference relevant issues and include tests.
