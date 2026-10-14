# Ambient capture configuration (`policy/ambient.yaml`)

`policy/ambient.yaml` and `CTXT_AMBIENT_FILE` are not implemented: no code reads either. `ctxt capture --ambient` (and `--input`, `--skip`, `--window`) exits with "not yet implemented". The sensor list, per-sensor schemas and permission model specced in the [ADR-065 amendment](../decisions/ADR-065-pluggable-adapters.md#amendment-2026-05-06--service-vs-sensor-distinction-ambient-capture-os-platform-slots) were never wired to a command.

What does exist:

- `policy/ctxt.yaml` (kit policy rules; `CTXT_POLICY_FILE` overrides the path) — see [policies.md](policies.md).
- Local watchers: `ctxt watch` with the `watch:` block in `ctxt.yaml` — see `ctxt watch --help`.
- Browser capture: `ctxt capture tabs|history|schedule` with the `capture:` block — see [`../manual/workflows/ambient-capture.md`](../manual/workflows/ambient-capture.md).
- Adapters for external sources: `ctxt ingest --source cardamum|himalaya` — see `ctxt ingest --help`.
