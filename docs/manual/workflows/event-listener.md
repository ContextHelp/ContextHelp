# Workflow: Listen for Events

## Goal

React to objects as ctxt ingests them from an external program, without polling `GET /objects`.

## Prerequisites

- An API key with `read:objects`. A non-owner key listens too: it gets only the object events for objects its share grant lets it read (see [Non-owner listeners](#non-owner-listeners)). The owner's key gets every event. A private instance needs none.
- The instance URL, for example `http://127.0.0.1:8080`.

## Quick start

```bash
curl -N -H "Authorization: Bearer $CTXT_TOKEN" http://127.0.0.1:8080/api/v1/events
```

Each event is one `data:` line holding a CloudEvents JSON document (object events have an `id:` line before it, see [Event ids and resume](#event-ids-and-resume)):

```text
data: {"source":"worker.pool","specversion":"1.0","type":"ctxt.runtime.object.ingested","data":{"object_id":"852e0fac-…","type":"text","source":"yta","source_key":"","pipeline":"text.short","tags":["star-candidate"],"profile_id":"","duration_ms":173}}
```

Filter in the listener, not the server: keep `type == "ctxt.runtime.object.ingested"` and match `data.source` (and `data.source_key`, when set). Fetch the object with `GET /api/v1/objects/{object_id}` for its content. Topics and fields: [REST API, Events](../../api/api-rest.md#events).

## Non-owner listeners

A listener should hold a read-only key with a narrow grant, not an owner key.

- It receives `object.ingested`, `object.updated`, `object.deleted` and `object.raw_stored` for objects its grant reads, and nothing else. Jobs, upgrade and embedding events stay owner-only, and so do the inbox events (`inbox.captured`, `inbox.triaged`), because the inbox is an owner-only surface.
- The payload matches what a read of the object shows. `object.updated` has no `fields` list. `source_key` is present when the grant's policy projects `source`, and absent otherwise. When a payload lacks what the listener needs, fetch the object with `GET /api/v1/objects/{object_id}`.
- A key with no grant gets `200` and a stream that carries only heartbeats. That is not an error: the grant is empty.
- `object.deleted` arrives only for an object the same connection was already told about, a replayed event included. A listener that was away while an object it knew was deleted does not get the delete: after a gap, reconcile with `find` or `list` and treat objects that no longer appear as deleted.
- The stream follows the key. A grant revoked or added takes effect on the next event, without a reconnect (a check is reused for up to 2 seconds during a burst). When the key is revoked or expires, the server writes `: closed: credential no longer valid` and ends the stream; reconnecting then answers 401.

## Heartbeat

An idle stream writes the comment line `: heartbeat` every 15 seconds (`server.events_heartbeat`). SSE clients ignore comment lines. A listener can treat a long silence (a few intervals with no line at all) as a dead connection and reconnect.

## Event ids and resume

Each object event frame carries `id: <seq>`, a number that grows with every event and is never reused:

```text
id: 2210
data: {"id":"0198…","source":"worker.pool","specversion":"1.0","type":"ctxt.runtime.object.ingested",…}
```

Keep the last `id` you processed. After a disconnect, reconnect with it and the server replays what you missed, then follows live:

```bash
curl -N -H "Authorization: Bearer $CTXT_TOKEN" -H "Last-Event-ID: 2210" http://127.0.0.1:8080/api/v1/events
```

A browser `EventSource` does this on its own. Where you cannot set a header, use `?last_event_id=2210`. `Last-Event-ID: 0` replays everything still retained; with no id, the stream starts at the present.

- **No gap, no duplicate.** The replay and the live tail read one cursor. An event written while the replay runs arrives once, in order.
- **Same filter.** A non-owner key gets replayed events through the same grant check as live ones.
- **Retention.** The server keeps events for 7 days (`server.events_retention`). An id older than that gets `410 Gone` with code `EVENT_ID_EXPIRED`, and an `EventSource` then stops. Resync, then reconnect without an id:

  ```bash
  ctxt list --cursor my-listener --advance --tagged star-candidate
  ```

  It returns the objects created since the cursor's last advance. The 410 body names the oldest retained id (`details.oldest_retained`) and the newest (`details.newest`).
- **At least once.** If you crash between handling an event and recording its id, the next replay includes it again: deduplicate on the envelope `id` (a UUIDv7, stable across connections).
- Events that are not object events (jobs, upgrades) are owner-only, live, and carry no `id`.

## Output validation

- The stream sends headers immediately; `curl -N` shows no output until an event happens.
- Ingest a test object (`ctxt analyze`) and expect one `object.ingested` with your `source`.

## Common failure modes

| Symptom | Cause |
|---|---|
| 401 | No or invalid key on a protected instance |
| Stream stays empty (heartbeats only) | The key's principal has no grant, or none that reads the objects you expect |
| 410 `EVENT_ID_EXPIRED` on connect | The `Last-Event-ID` is older than `server.events_retention`, or ahead of the instance's newest event (restored database). Resync, then connect without an id |
| 400 `INVALID_EVENT_ID` | `Last-Event-ID` is not a non-negative integer |
| Event missing | Disconnected past retention (410), or the write path publishes no event (`ctxt ingest`) |

## Durable delivery

Ids, resume and retention above are the first part of the at-least-once outbox of [ADR-075](../../decisions/ADR-075-external-hook-surface.md). Pull, webhooks and the hooks come later.
