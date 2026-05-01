# US-0026: Share Composition With Team

**System Types:** ctxt, dpkms cloud
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to share a composition (brief, plan, timeline, or other) with teammates or external stakeholders so they can view, comment on, or collaborate with the content without needing full system access.

---

## Context

Knowledge workers regularly need to circulate briefs and plans to people who are not active users of the knowledge system — reviewers, executives, or external partners. Share links with configurable permissions (read/comment/edit) and optional expiry let knowledge workers control access precisely, and revocation ensures shares can be withdrawn when no longer appropriate.

---

## Acceptance Criteria

- [ ] User can share a composition by ID (`--composition <id>`) with one or more recipients (`--to <user|team>`)
- [ ] User can set permission level: `read`, `comment`, or `edit` (`--permission <level>`)
- [ ] User can set an optional expiry duration (`--expires <duration>`)
- [ ] User can attach an optional message (`--message <text>`)
- [ ] Response includes `share_url` or `share_token`, `recipients`, `permission`, `expires_at`
- [ ] Share record stores `recipients`, `permission`, `expires_at`, `created_by`, `created_at`
- [ ] Share token is unique and tied to the specific composition and recipient set
- [ ] Accessing a share token before expiry returns the composition content
- [ ] Accessing a share token after expiry returns 403/410 with descriptive error
- [ ] Recipient with `read` permission can GET composition but cannot modify
- [ ] Recipient with `comment` permission can POST a comment but cannot edit content
- [ ] Recipient with `edit` permission can PATCH composition sections
- [ ] Non-recipient using another composition's share token receives 403
- [ ] User can revoke a share (`ctxt share revoke <share_id>`); token returns 403/404 after revocation
- [ ] Non-existent composition ID returns 404; non-existent recipient returns 404
- [ ] Invalid `--permission` value returns 400 with list of valid values

---

## Implementation Notes

### CLI Interface

```bash
# Share with read access
ctxt share composition c-xyz789 --to alice@example.com --permission read

# Share with team, comment access, expiry, and message
ctxt share composition c-xyz789 --to team:engineering --permission comment \
  --expires 7d --message "Please review before Friday"

# Revoke a share
ctxt share revoke share-abc123

# Returns JSON with share record
{
  "share_id": "share-abc123",
  "composition_id": "c-xyz789",
  "share_url": "https://ctxt.example.com/share/share-abc123",
  "recipients": ["alice@example.com"],
  "permission": "read",
  "expires_at": "2025-01-25T10:30:45Z",
  "created_at": "2025-01-18T10:30:45Z"
}
```

### REST API

```
POST /compositions/{id}/shares
Content-Type: application/json

{
  "recipients": ["alice@example.com"],
  "permission": "comment",
  "expires_at": "2025-01-25T10:30:45Z",
  "message": "Please review before Friday"
}

→ 200 OK  (share record created)
→ 400 Bad Request  (invalid permission value)
→ 404 Not Found    (non-existent composition or recipient)

GET /compositions/{id}/shares
→ 200 OK  (list of share records for this composition)

DELETE /compositions/{id}/shares/{share_id}
→ 200 OK  (share revoked)
```

---

## E2E Test Checklist

### CLI → Server payload propagation
- [ ] `--composition <id>` sends `composition_id` in POST body
- [ ] `--to <user|team>` sends `recipients` array in POST body
- [ ] `--permission <read|comment|edit>` sends `permission` in POST body
- [ ] `--expires <duration>` sends `expires_at` (computed timestamp) in POST body when provided
- [ ] `--message <text>` sends `message` in POST body when provided

### Server-side receipt and storage
- [ ] POST /compositions/{id}/shares persists share record; GET /compositions/{id}/shares lists it
- [ ] Share record stores `recipients`, `permission`, `expires_at`, `created_by`, `created_at`
- [ ] Share token in response is unique and tied to specific composition + recipient set
- [ ] Accessing share token before expiry returns composition content
- [ ] Accessing share token after expiry returns 403/410 with descriptive error

### CLI output validation
- [ ] `ctxt share composition <id> --to <user>` exits 0 and returns share record JSON
- [ ] Share record includes `share_url` or `share_token`
- [ ] Share record includes `recipients`, `permission`, `expires_at`

### Permission enforcement
- [ ] Recipient with `read` permission can GET composition but cannot modify
- [ ] Recipient with `comment` permission can POST comment; cannot edit content
- [ ] Recipient with `edit` permission can PATCH composition sections
- [ ] Non-recipient request with share token for another composition returns 403

### Revocation
- [ ] `ctxt share revoke <share_id>` sends DELETE /compositions/{id}/shares/{share_id}
- [ ] After revocation, share token returns 403/404

### Error handling
- [ ] Non-existent composition ID returns 404 with descriptive error
- [ ] Non-existent recipient returns 404 with descriptive error
- [ ] Invalid `--permission` value returns 400 with list of valid values

---

## Related Stories

- [US-0022: generate-brief-from-objects](./US-0022-generate-brief-from-objects.md) — Share generated brief
- [US-0023: generate-plan-from-decisions](./US-0023-generate-plan-from-decisions.md) — Share generated plan
- [US-0025: export-brief-to-markdown-pdf](./US-0025-export-brief-to-markdown-pdf.md) — Export before sharing
- [US-0056: compose-decision-timeline](./US-0056-compose-decision-timeline.md) — Share decision timelines
- [US-0059: compose-recommendation-document](./US-0059-compose-recommendation-document.md) — Share recommendation documents

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)

---

## E2E Tests

- `test/integration/us0026_share_composition_test.go::TestUS0026_ShareEventRecordedOnBus`
- `test/integration/us0026_share_composition_test.go::TestUS0026_CompositionObjectAccessibleAfterIngest`
- `test/integration/us0026_share_composition_test.go::TestUS0026_MultipleObjectsComposedAndAccessible`
- `test/integration/us0026_share_composition_test.go::TestUS0026_ShareLinkContainsObjectReference`
