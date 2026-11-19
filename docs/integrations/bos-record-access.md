# Access and records from an external authority

ctxt can take access from an authority speaking `bos.access/v1` (document
1.1): type grants, and record-level entries that let a person assigned to an
engagement, a project or a client file read the records under it without one
grant per record. It enforces them on reads, builds knowledge objects from
the authority's records, and keeps enforcing while the authority is
unreachable.

Code: `internal/bosaccess` (peer side of the contract),
`internal/sharing/records.go` (entries become a viewer predicate),
`internal/server/http/handlers_access.go` (routes), `internal/bosfeed`
(feed reader), `internal/bosingest` (records as objects).

## Enable

```yaml
sharing:
  authority:
    enabled: true
    principal: prn_...          # the peer principal the authority provisions as (required)
    scope: bos://core.business_scopes/...   # the one scope it provisions (required)
    feed:                       # optional: without it record-level reads stay denied
      url: https://bos.example
      peer: ctxt
      interval: 1m
```

The feed's bearer credential is the secret `bos/feed-token`; the feed reads
the business scope named by `scope`. Provisioning is bound to one
principal and one scope: only `principal` (kind `peer`) may push, only for
`scope` (compared as the full address). With either unset every call is
refused, so nobody claims a scope by pushing first.

## Routes

| Route | Meaning |
| --- | --- |
| `PUT /bos/v1/access/{business}` | replace the set (idempotent by epoch) |
| `PATCH /bos/v1/access/{business}` | apply a delta at `base_epoch` |
| `GET /bos/v1/access/{business}/state` | applied epoch, digest, time, what is not representable |
| `GET /bos/v1/profile` | the profile ctxt declares |

Errors carry the contract's codes (`epoch_stale` 409, `base_epoch_mismatch`
409, `digest_mismatch` 422, `set_invalid` 400, `scope_unknown` 404,
`forbidden` 403). Every set applied or refused is one audit-log entry
(`access.applied`, `access.refused`): epoch, mode, digest and counts, never
content.

## What ctxt declares

Contract `bos.access/v1` with features `record_access` and
`principal_identities`, shapes `type_grant`, `expiry`, `row_relation`,
`record_grant`. No field exclusion or direction limit: a grant needing one is
denied and named in the acknowledgement. Operation grants are denied and
named by id; an entry with an unknown property is denied and named by entry
id.

## Who a person is

Sets carry each principal's external identities (`issuer`, `subject`, e.g. a
Microsoft Entra tenant issuer and object id). A ctxt principal is the
authority's principal when an active `oidc` identity of it matches
(issuer, subject) exactly; an identity two principals carry maps to neither.
Identities grant nothing.

## How a read is decided

`Peer.Readable(principal)` lists the records a person may read now. Every
condition of the contract holds for each: the set is fresh (the shorter of
the set's and ctxt's limit), a type grant or an entry covers the record's
scope and type (restricted types only by exact key), entries are in force
(`valid_from` inclusive, `valid_until` exclusive), the feed catch-up is
within `record_basis_max_staleness_seconds`, the record's `under` is known
and complete, and a special-category record needs the clearance grant.
Everything fails closed. Only records the feed announced can be listed.

`sharing.Resolver.WithRecords` turns the list into one more grant of the
viewer: objects with source `bos` whose `source_key` is a listed address,
rendered with every field, with search (fts, rsql), entities, MCP and export.
The ceiling, key policy and class exclusions narrow them like any grant on
every path; the policy key names the list, so a start, end or move changes
the compiled viewer on the next request. A viewer names at most
`sharing.MaxRecordAddresses` records. Entry dates live on the entries; ctxt's
own share grants are unchanged.

## Feed and ingest

`bosfeed.Reader` reads `GET /v1/commands/peer/feed` pages, applies each event
to the access state and the sinks, moves the cursor only after a whole page
was applied, and tells the state it caught up at the head of the feed. An
expired cursor starts over from the oldest retained event.

`bosingest.Sink` stores a record as an object of source `bos` keyed by its
address and retracts it on `deleted`, `erased` or when the reading principal
can no longer read it. Content comes from the authority's served read
commands over REST (`bosingest.RESTReader`):
`POST /v1/commands/<path>` with `{"args": [<record id>]}` and ctxt's bearer
credential (the feed token), so the authority's access decision applies. A
403 or 404 means do not store, and retract what is held. Default commands:
`core.parties` (`party get`), `core.documents` (`document get`),
`billing.documents` (`billing get`), `contracts.contracts` (`contract show`);
a record of another type is left alone. The text of a record is the fields
the command returns; `document get` returns metadata, not the file's text.
