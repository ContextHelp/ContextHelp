# XStrategy

Single platform-keyed strategy for x.com and twitter.com. URLs
normalise to x.com so the resolver collapses twitter↔x duplicates on
identity_key.

## Surfaces

| Host | Specificity |
|---|---|
| x.com / twitter.com (apex) | 1 |
| any *.x.com / *.twitter.com (mobile., m., business.) | 2 |

## Sub-paths

- `/<user>` — author profile (`x_profile`).
- `/<user>/status/<id>` — tweet → also emits `/<user>/media`
  (`x_media`) and the thread anchor (`x_thread`).
- `/<user>` (bare profile capture) — also emits `/<user>/lists`
  (`x_list`) and `/<user>/likes` (`x_likes`).
- Reserved path segments (search, explore, i, intent, tos, privacy,
  hashtag, etc.) skip the probe.

## Identity keys

| Type | Form |
|---|---|
| `x_profile` | `x/profile/<user>` |
| `x_thread` | `x/thread/<user>_<tweet_id>` |
| `x_list` | `x/profile/<user>/lists` |
| `x_likes` | `x/profile/<user>/likes` |
| `x_media` | `x/profile/<user>/media` |

## Wiring

`x.New(client)` takes an `x.XClient`; only `FetchProfile(username) →
user_id` is consulted today (and only when wired). Nil client → strategy
uses the username as the identity-key id segment.

## Limits

- No quoted-tweet expansion. Spec calls out quoted_url in Preview hints
  but the substrate doesn't yet thread them; that lateral path is
  deferred.
- Status URLs containing the legacy `/i/web/status/<id>` form go through
  the reserved-user filter and produce no candidates.
