# Contributing to ContextHelp

Thank you for your interest in contributing to **ContextHelp**, a decentralized, local-first knowledge engine.
This document explains how to contribute code, documentation, registry definitions, plugins, and ideas in a consistent and high-quality manner.

We welcome contributions from everyone — whether you are fixing bugs, proposing new ADRs, writing documentation, or extending the engine with plugins.

---

# Getting Started

Clone the repository:

```bash
git clone https://github.com/ContextHelp/ContextHelp.git
cd ContextHelp
```

Then follow **[DEVELOPING.md](DEVELOPING.md)** to set up the toolchain, build
the binaries, and run the test tiers. It covers the prerequisites (mise, the
mandatory `CGO_ENABLED=1` and `-tags fts5` settings), which tests need Docker,
and how to match the CI lint gate locally.

This document covers the rest: how to propose a change, and the standards it
must meet.

---

# Code of Conduct

By participating in this project, you agree to follow our **Code of Conduct**
(found at `.github/CODE_OF_CONDUCT.md`).

---

# How to Contribute

Contributions fall into the following categories:

- 🧠 **Ideas & Feature Requests**
- 🐛 **Bug Reports**
- 🛠️ **Code Contributions**
- 📚 **Documentation Improvements**
- 📦 **Plugin Contributions**
- 🗂️ **Registry Definitions / Taxonomy / Tag Specs**
- 🧪 **Tests / Reliability improvements**
- 📄 **ADRs (Architectural Decision Records)**

Each category has its own workflow.

---

# Filing Issues

Use GitHub Issues for:

- Bugs (with reproduction steps)
- Missing features
- Performance regressions
- Questions about architecture
- Suggestions for plugins or pipeline improvements

### Issue Template Includes:

- ContextHelp version
- OS/platform
- Steps to reproduce
- Expected vs actual behavior
- Logs (if available)

Always search existing issues before creating a new one.

---

# Proposing Features

Large features MUST be backed by:

1. **An Issue** describing motivation
2. **A Proposal** (short design doc)
3. **An ADR** if the feature changes architecture or introduces new patterns

Do not open large PRs without design consensus.

---

# Development Workflow

## Branch Strategy

- `main` → stable code
- `develop` → active development (if project adopts GitFlow)
- feature branches:
  - `feature/<short-description>`
- bugfix branches:
  - `fix/<issue-number>`

## Commit Messages

Follow conventional commits:

```
feat: add new pipeline type
fix: handle job retry for image pipeline
docs: update registry protocol
chore: improve build script
test: add unit test for AST parser
refactor: extract interface for vector provider
```

---

# Coding Standards

## Language & Style

- Go code must follow `gofmt` and `golangci-lint`. See
  [DEVELOPING.md](DEVELOPING.md#lint-and-formatting) for the CI-pinned linter
  version and the pre-commit hooks that mirror the gate.
- Avoid global state except where documented.
- Prefer interfaces over concrete structs for:
  - storage
  - pipelines
  - AI providers
  - vector stores
  - registries
  - query operators

## Error Handling

- Always wrap errors with context using `%w`.
- Prefer sentinel errors for known failure modes.

## Logging

- Use structured logging.
- Avoid excessive verbosity.
- Never log sensitive data (API keys, user content, etc.).

---

# Testing Expectations

For the commands — the four test tiers, which ones need Docker, and how to run
them — see [DEVELOPING.md](DEVELOPING.md#test-tiers). What a contribution is
expected to include:

- New behavior ships with tests.
- A bug fix ships with a test that fails without the fix.
- Both the unit and integration tiers must pass before you open a PR.

### Test Philosophy

- Unit tests MUST NOT call external APIs.
- Mocks should be used for:
  - AI providers
  - registries
  - storage backends
  - pipeline steps
- End-to-end tests use local JSON/SQLite fixtures.

---

# Adding Pipelines

Pipelines live in `pipelines/` and consist of:

- Pipeline definition
- Ordered list of steps
- Steps implementing `PipelineStep` interface

To contribute a pipeline:

1. Create a new pipeline module under `pipelines/`.
2. Define steps as pure, deterministic functions.
3. Ensure steps are retryable or marked non-retryable.
4. Add tests for:
   - inference
   - error handling
   - typical cases
5. Register it in the pipeline registry.

---

# Adding Plugins

Plugins may extend:

- CLI commands
- registry providers
- storage drivers
- AI clients
- query operators
- pipelines

Plugin directory structure:

```
plugins/
  yourplugin/
    main.go
    README.md
    go.mod
```

Plugins MUST:

- expose `Register(HostContext)`
- use plugin-scoped configuration
- declare required permissions
- follow semantic versioning

Plugins MUST NOT:

- access private user files outside allowed scopes
- phone home without explicit user consent
- bypass the job system for ingestion

---

# Adding Registries

Registry contributions include:

- Tag definitions
- Taxonomies
- Semantic labels
- Subtype definitions
- Mappings & aliases
- Language translations

Registry format is documented in:

```
docs/registries.md
docs/schema-registry.md
docs/schema-taxonomy.md
docs/schema-tag.md
```

Registries MUST:

- be valid JSON or YAML
- follow stable namespace conventions
- provide canonical labels
- support future multilingual extensions

---

# Modifying the Query Language

Changes to the Query Language require:

1. Updating `query-language-spec.md`
2. Updating the AST implementation
3. Updating tests
4. Updating documentation
5. Possibly updating registries that define new operators

This must be done carefully — QL is foundational for agents.

---

# Modifying ADRs

ADRs are immutable except:

- to mark as superseded
- to append clarification sections

To propose a new ADR:

1. Open an issue describing the necessity
2. Draft ADR with:
   - context
   - decision
   - alternatives
   - consequences
3. Submit PR
4. Wait for maintainer review

---

# Submitting PRs

### Pull Request Requirements

- PR must pass all tests
- PR must be linted
- PR must include documentation updates if needed
- PR must reference related issues
- PR must include justification in the description
- PR must avoid large, unrelated code changes

### PR Review Workflow

1. Automated checks run: tests, builds, linters
2. Maintainers review:
   - architecture alignment
   - correctness
   - documentation
   - performance impact
   - backward compatibility
3. Discussion/resolution
4. Merge into `main`

---

# Performance Guidelines

Changes must not introduce:

- unnecessary allocations
- blocking IO in critical paths
- concurrency hazards
- n+1 registry calls
- excessive memory growth during pipeline execution

Where applicable:

- measure before/after
- use `pprof`
- optimize only with evidence

---

# Security Guidelines

Contributors must respect:

- user data privacy
- permission boundaries for plugins
- local-first security model
- no unauthorized external calls
- safe handling of user content

If you discover a vulnerability:

- **Do not open an issue**
- Email the security contact (listed in `SECURITY.md`)

---

# Documentation Contributions

We love documentation improvements.

You can contribute:

- instruction guides
- diagrams
- architecture clarifications
- examples
- plugin tutorials
- improvements to API reference

Docs live under `docs/`.

Use clean Markdown and keep formatting consistent.

---

# Release Process

Only maintainers perform releases.

Release includes:

- version bump
- CHANGELOG entry
- building binaries
- tagging version
- packaging plugin examples

---

# Thank You

ContextHelp grows through community effort.
We deeply appreciate every contributor — whether you're fixing a small typo or building a full new registry, your work helps shape a powerful ecosystem of decentralized knowledge tools.

If you have questions, open an issue or join the discussions.

Happy contributing! 🚀