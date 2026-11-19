# Record-level access from an external authority

ctxt can hold the record-level access entries an authority speaking
`bos.access/v1` (document 1.1) provisions, and enforce them when it reads
knowledge. A person assigned to an engagement, a project or a client file
reads the records that stand under it without one grant per record.

Code: `internal/bosaccess` (the peer side of the contract),
`internal/sharing/records.go` (how entries become a viewer predicate).

## What ctxt declares

Profile (`bosaccess.DeclaredProfile`): contract `bos.access/v1` with the
feature `record_access`, the shapes `row_relation` and `record_grant`, and a
24 h staleness limit. It lists no type-grant shape: ctxt reads through its
own share policies, so a type grant the authority holds is one ctxt does not
enforce and the authority lists as not representable. Operation grants are
reported unrepresentable in the acknowledgement (by id). An entry with a
property ctxt does not know is denied and named by entry id.

## What ctxt holds

- The last set (full or delta, by epoch and digest), durably
  (`sharing.KVState` over the instance key-value store, or `FileStore`).
- What each record stands under, its scope and sensitivity, from the change
  feed events (`Peer.Consume`), and the time of the last complete catch-up
  (`Peer.CatchUp`).

## How a read is decided

`Peer.Readable(principal)` lists the records a person may read now. Every
condition of the contract holds for each: the set is fresh (shorter of the set's
and ctxt's limit), the feed catch-up is within
`record_basis_max_staleness_seconds`, the record's `under` is known and
complete, and an entry of the person names an anchor in `under` (or the
record), is in force (`valid_from` inclusive, `valid_until` exclusive), and
covers the record's scope and type (a restricted type only by exact key). All
of it fails closed.

`sharing.Resolver.WithRecords` turns the list into one more grant of the
viewer: objects with source `bos` whose `source_key` is a listed address.
The ceiling, key policy and exclusions narrow it like any grant, and the
viewer's policy key names the list, so a start, an end or a move changes the
compiled viewer on the next request. A viewer names at most
`sharing.MaxRecordAddresses` records.

A ctxt principal is bound to its address at the authority by an active
`oidc` identity with issuer `bos` and the bos principal address as subject.
`service.WithAuthorityAccess` switches the viewers on.

Entry dates live on the entries themselves; ctxt's own share grants are not
changed.

## Not covered

The HTTP routes that receive sets and serve the state, the feed reader that
calls `Consume` and `CatchUp`, and the ingest adapter that stores the
records as objects of source `bos` are separate pieces.
