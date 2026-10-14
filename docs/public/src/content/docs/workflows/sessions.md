---
title: "Workflow: Work Sessions"
description: Work sessions (grouping ambient captures into bounded work units, per [ADR-067](../../decisions/ADR-067-session-workunit.md)) are not implemented. There is no `...
---

Work sessions (grouping ambient captures into bounded work units, per [ADR-067](../../decisions/ADR-067-session-workunit.md)) are not implemented. There is no `ctxt session` command, no session storage in dpkms, no `session_id` on knowledge objects, and no `ambient.session` config block.

What exists:

- The session cutter as library code in `internal/ambient/session/` (defaults: 5 min idle gap, 3 min soft cut, 2 h timeout). No command or daemon runs it.
- Composing a time range: `ctxt compose summary --since <YYYY-MM-DD or RFC 3339> --until <...>` composes the objects created in that window. See `ctxt compose --help`.

Design notes: [ADR-067](../../decisions/ADR-067-session-workunit.md), [`../../diagrams/ambient/067-lifecycle.mmd`](../../diagrams/ambient/067-lifecycle.mmd), [`../../diagrams/ambient/067-cutter-flowchart.mmd`](../../diagrams/ambient/067-cutter-flowchart.mmd).
