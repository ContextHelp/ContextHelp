# Story: Set Reminder on Knowledge Object

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to set a time-based reminder on a knowledge object so I
am prompted to revisit it at a specific future moment — without having to remember it
manually.

---

## Context

Knowledge workers capture content continuously but often need to act on it later. A
reminder bridges capture and action: it pins a future moment to an object and surfaces
it when `dpkms serve` is running. Natural-language time expressions (`in 2h`,
`tomorrow 9am`) make it frictionless from the CLI.

---

## Acceptance Criteria

- [ ] `ctxt remind <id> <time-expression>` sets a reminder on the object
- [ ] Time expressions accepted: `in Xh`, `in Xm`, `in XhYm`, `today HH:mm`,
      `tomorrow HH:mm`, `<weekday> HH:mm`, `YYYY-MM-DD`, `YYYY-MM-DD HH:mm`
- [ ] Confirmation printed: `Reminder set for <id> at <timestamp>`
- [ ] `ctxt remind --clear <id>` removes the reminder; prints confirmation
- [ ] `ctxt reminders` lists all objects with pending reminders (table: ID, title,
      remind-at, status)
- [ ] Status values: `pending`, `overdue`, `notified`
- [ ] `ctxt reminders --output json` returns structured JSON
- [ ] Setting a reminder on a non-existent object returns a clear error
- [ ] Invalid time expression returns a parse error with examples

---

## Spec Reference

No ADR. Feature implemented from scratch; time-based reminders are a user-driven
complement to the proactive surfacing described in
[ADR-016](../../decisions/ADR-016-just-in-time-surfacing.md).

---

## Implementation Notes

### CLI surface

```
ctxt remind <id> <time-expression>   # set
ctxt remind --clear <id>             # clear
ctxt reminders                       # list pending
ctxt reminders --output json
```

### Time parsing (`internal/remind`)

- `ParseTime(expr, now)` returns `time.Time` or error
- Supports: relative (`in 2h`), weekday (`monday 9am`), absolute (`2026-04-01 10:00`)

### Storage

- `KnowledgeObject.RemindAt *time.Time` — target time (migration 014)
- `KnowledgeObject.RemindedAt *time.Time` — set by worker when notification fires
- `Service.SetObjectReminder(ctx, id, at)` / `ClearObjectReminder(ctx, id)`
- `Service.ListPendingReminders(ctx)` — returns objects where `RemindAt IS NOT NULL`

### Worker notification

- `dpkms serve` polls for overdue reminders and fires desktop notifications
- Sets `RemindedAt` after firing to prevent repeat

---

## E2E Checklist

- [ ] `ctxt remind <id> "in 1h"` → confirm message with timestamp
- [ ] `ctxt reminders` shows the object with status `pending`
- [ ] `ctxt remind --clear <id>` → confirm cleared
- [ ] `ctxt reminders` no longer shows the object
- [ ] Invalid expr (`ctxt remind <id> "next fortnight"`) → parse error

---

## Related Stories

- US-0319 — Resurface knowledge objects by profile relevance
- US-0054 — Saved search and alerts
- US-0020 — Apply focus profile to search
