# Diagrams

Source-of-truth diagrams in [Mermaid](https://mermaid.js.org/) format. Each `.mmd` file contains exactly one diagram and renders natively in GitHub, GitLab, VS Code (with the Markdown Mermaid extension), Obsidian, and the [Mermaid Live Editor](https://mermaid.live).

ADRs reference these files instead of inlining `\`\`\`mermaid` blocks so diagrams can be:

- Edited in isolation without ADR re-flow
- Reused across docs (architecture overview, manual workflows, user stories)
- Diff-reviewed independently
- Exported to PNG/SVG via `mmdc` if needed

## Top-level diagrams

| File | Purpose |
|---|---|
| [`../ecosystem-overview.mmd`](../ecosystem-overview.mmd) | High-level ecosystem (registry, businesses, SMEs) |
| [`../dataflow.mmd`](../dataflow.mmd) | Existing capture → mention extraction → storage flow |

## Ambient capture (ADR-066/067/068/069)

Four ADRs land the ambient capture substrate, sessions as a first-class type, the MCP read-surface, and meeting capture. Twelve diagrams cover their architecture, state machines, and flows.

| File | Diagram | Source ADR |
|---|---|---|
| [`ambient/066-architecture.mmd`](ambient/066-architecture.mmd) | High-level architecture: sources → runner → buffer → enqueue → dpkms; CLI/ctxd/service-manager launchers | [ADR-066](../decisions/ADR-066-ambient-capture-substrate.md) |
| [`ambient/066-event-flow.mmd`](ambient/066-event-flow.mmd) | Per-event flow: capture → redact → filter → dedup → compress → session-tag → buffer → enqueue with retry | [ADR-066](../decisions/ADR-066-ambient-capture-substrate.md) |
| [`ambient/066-buffer-backends.mmd`](ambient/066-buffer-backends.mmd) | Buffer backend selection: local FS / S3 / memory with retention strategy per backend | [ADR-066](../decisions/ADR-066-ambient-capture-substrate.md) |
| [`ambient/067-lifecycle.mmd`](ambient/067-lifecycle.mmd) | Session lifecycle state machine (v1 terminal at `ended`; reducer states reserved for Phase 6+) | [ADR-067](../decisions/ADR-067-session-workunit.md) |
| [`ambient/067-cutter-flowchart.mmd`](ambient/067-cutter-flowchart.mmd) | Three-rule cutter decision flow: idle / soft-cut + frequent-switching exception / timeout | [ADR-067](../decisions/ADR-067-session-workunit.md) |
| [`ambient/067-replay-sequence.mmd`](ambient/067-replay-sequence.mmd) | Network-loss replay sequence demonstrating soft-FK behavior (events arrive before/after session row) | [ADR-067](../decisions/ADR-067-session-workunit.md) |
| [`ambient/068-topology.mmd`](ambient/068-topology.mmd) | Two-server MCP topology: dpkms-side authoritative + ctxd-side local; agents attach to both | [ADR-068](../decisions/ADR-068-mcp-read-surface.md) |
| [`ambient/068-tool-dispatch.mmd`](ambient/068-tool-dispatch.mmd) | MCP tool dispatch sequence with bus-event emission and CEL veto path | [ADR-068](../decisions/ADR-068-mcp-read-surface.md) |
| [`ambient/068-tool-surface.mmd`](ambient/068-tool-surface.mmd) | Tool surface groupings: 10 dpkms-side tools + 5 ctxd-side tools, organized by purpose | [ADR-068](../decisions/ADR-068-mcp-read-surface.md) |
| [`ambient/069-recording-state.mmd`](ambient/069-recording-state.mmd) | Meeting recording state machine with first-class failure states | [ADR-069](../decisions/ADR-069-meeting-capture-source.md) |
| [`ambient/069-multi-platform.mmd`](ambient/069-multi-platform.mmd) | Multi-platform architecture: Mac/Windows/Linux desktop + iOS/Android companions → enqueue | [ADR-069](../decisions/ADR-069-meeting-capture-source.md) |
| [`ambient/069-recording-sequence.mmd`](ambient/069-recording-sequence.mmd) | End-to-end recording flow: trigger → permission → capture → file → pipeline → KO with bus events | [ADR-069](../decisions/ADR-069-meeting-capture-source.md) |

## Authoring conventions

1. **One diagram per file.** Don't pack two diagrams into the same `.mmd`.
2. **Wrap in `\`\`\`mermaid` … `\`\`\`` fences.** Matches the existing top-level files (`ecosystem-overview.mmd`, `dataflow.mmd`) and renders correctly when the file is included verbatim into a Markdown doc.
3. **Use base theme** (default in this repo) unless the diagram has a domain-specific reason for a custom palette.
4. **Keep node labels short.** Use `<br/>` for multi-line labels rather than long single-line strings.
5. **Comment with `%%`** when the diagram has non-obvious structure.
6. **Reference from ADRs and docs by Markdown link.** Do not duplicate the diagram body inline; let the file be the single source of truth.

## Rendering and exporting

- **GitHub / GitLab**: rendered automatically in PR diffs and Markdown previews.
- **VS Code**: install [Markdown Mermaid](https://marketplace.visualstudio.com/items?itemName=bierner.markdown-mermaid).
- **Obsidian**: built-in support.
- **Live editor**: paste contents into [mermaid.live](https://mermaid.live) for interactive editing and PNG/SVG export.
- **CLI export**:
  ```bash
  npm install -g @mermaid-js/mermaid-cli
  mmdc -i docs/diagrams/ambient/066-architecture.mmd -o /tmp/066-architecture.png
  ```
