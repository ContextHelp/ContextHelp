# ADR-086 – The Daemon Re-derives `reingest_selective` Changes by Itself

> **Status:** Accepted
> **Date:** 2026-11-28
> **Author:** jadb
> **Applies to:** dPKMS daemon, pipeline registry, `ctxt upgrade`
> **Supersedes:** None
> **Amends:** ADR-070 §1 (a `reingest_selective` change always needs the operator to run `ctxt upgrade run`)
> **References:** ADR-070 (pipeline and index versioning), ADR-007 (transactional outbox), `docs/dpkms/jobs-and-ingestion.md` (Automatic re-derive)

---

## Context

ADR-070 says a `reingest_selective` change needs the operator to run `ctxt upgrade run`. In practice stored objects stayed on their old output until someone ran it, and most operators never did. Two step changes already shipped this way: the distilled README of `url.github.repo`, and the page cleaner of the `url.*` pipelines.

Each stored object now carries a derivation signature over its pipeline's ordered `step@version` entries and its model entries. A changed step version or model makes the objects of that pipeline stale, and the daemon can tell which ones.

## Decision

The daemon re-derives stale objects by itself, as a background job at start. Consent moves from "the operator types a command" to "the work stays on the machine and is held back by one setting".

| Rule | Detail |
|---|---|
| Which pipelines | Only those that call no model, or only local ones. A hosted, unknown or unavailable provider defers the group. |
| Source of the new output | Stored content when the changed step runs on stored content. A guarded re-fetch when the changed step is fetch-side (a fetcher, a cleaner of fresh content, or a step that needs the fetch's output, such as the repository mapper). |
| Refresh guard | A re-fetched page replaces the stored text only if it is still the same content on the same host. A repository README must not be empty or much shorter. |
| Retries | Never blind. Only a local decision (refused, stored text too short to compare) is a skip at once. Every other failure is a try, and strikes only when the same run proved what failed was working. Five tries in a row stop the group, not the run. |
| What is not automatic | Deferred groups are logged, listed in the end-of-run summary and shown by `ctxt upgrade status`. Their objects keep their text. |
| `reingest_all` | Unchanged. It still needs the explicit command and its consent token. |
| Hold back | `upgrade.rederive.enabled: false`. |
| Cost control | A pause between two objects, `upgrade.rederive.pause`, default `2s`. |

A step version is bumped only when its output for the same input changes. Every bump comes with a `reingest_selective` row in the release notes, and a test pins the shipped versions so a bump cannot go unnoticed.

## Why

- **The default must be correct.** A stale corpus that needs a manual command is stale on most machines.
- **Local work is cheap and private.** Pipelines that call no model, or only local ones, send no stored content to a third party. A re-fetch goes to the source the object came from; a repository is fetched from the GitHub API.
- **Consent still holds where it matters.** Anything that would use a hosted provider, or cannot be classified, is deferred and visible.
- **No silent damage.** The refresh guard keeps the stored object when the source changed or moved. Skips are facts, not retries.

## Alternatives considered

| Alternative | Why not |
|---|---|
| Operator-run only (ADR-070 as written) | Objects stay stale in practice; the release-notes row is the only prompt. |
| A consent prompt on each run | A daemon at start has no one to ask, and the prompt would be answered by habit. The setting and the deferral rules carry the consent. |
| Re-derive hosted pipelines too | Sends stored content to a third party without the operator choosing it, and costs money per object. |

## Consequences

- Most `reingest_selective` releases need no operator action. The release-notes entry says what the daemon does and what it defers.
- A deferred group keeps its text. `ctxt upgrade run` re-derives from stored content only, cannot apply a change that needs a fetch, and leaves those objects stale.
- Step authors must not bump a version for a change that leaves the output the same, since a bump costs every operator a fetch and model calls per object.

## Known limits

- Changing the configured model to another local model re-derives every LLM pipeline's objects at the next start. There is no consent step. A hosted model is deferred.
- Five failures in a row stop a group until a later start.
- A skip is permanent for that target.
- A refresh replaces the stored text and keeps no copy of the old one.
- There is no command to clear skips. A skipped object is tried again only when its pipeline's signature changes.
- A replica stored before signatures were recorded was signed like a local row, and counts as local.
- An object alone on a host that never answers is retried at every start.
