# YouTubeStrategy

Single platform-keyed strategy for youtube.com and youtu.be. Shortlinks
normalise to `/watch?v=<id>` so the resolver collapses duplicates on
identity_key.

## Surfaces

| Host | Specificity |
|---|---|
| youtube.com (apex) | 1 |
| any *.youtube.com (m., music., kids., etc.) | 2 |
| youtu.be | 2 |

## Sub-paths

| Captured form | Candidates |
|---|---|
| `/watch?v=<id>` | video; uploader channel via `VideoUploader` |
| `/shorts/<id>` | short; uploader channel via `VideoUploader` |
| `/playlist?list=<id>` | playlist |
| `/channel/<UCxxxx>` | channel + uploads facet |
| `/@<handle>` | vanity channel; canonical UC channel + uploads via `ResolveChannel` |
| `/c/<custom>` | vanity channel; canonical UC channel + uploads via `ResolveChannel` |
| `/user/<legacy>` | vanity channel; canonical UC channel + uploads via `ResolveChannel` |
| `youtu.be/<id>` | normalised to /watch?v=<id> |

## Identity keys

| Type | Form |
|---|---|
| `yt_video` | `youtube/video/<videoID>` |
| `yt_short` | `youtube/video/<videoID>` (same as video) |
| `yt_channel` (canonical) | `youtube/channel/<UCxxxx>` |
| `yt_channel` (vanity) | `youtube/channel/<kind>_<vanity>` |
| `yt_uploads` | `youtube/channel/<UCxxxx>/uploads` |
| `yt_playlist` | `youtube/playlist/<listID>` |

## Wiring

`youtube.New(client)` takes a `YouTubeClient` exposing
`ResolveChannel(vanity)` and `VideoUploader(videoID)`. Vanity channel
captures degrade to a vanity-keyed identity without the client; the
resolver dedups on the canonical UC id once the client wires up.

## Limits

- No transcript / chapters / description-link extraction in v1 —
  strategy emits only structural candidates. Content-level lateral is
  deferred.
- youtu.be normalisation strips path-based query suffixes (`?t=42`).
  Time-anchored shares dedup with the canonical video.
