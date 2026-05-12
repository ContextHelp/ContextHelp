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

`x.New()` takes no client. The strategy is purely URL-structural: it
parses the captured URL, normalises twitter.com → x.com, and emits
sub-path probe candidates. Identity keys are username-keyed. A future
patch may re-introduce a daemon-side client for username → user_id
resolution; until it does, no wiring is required.

## Limits

- No quoted-tweet expansion. Spec calls out quoted_url in Preview hints
  but the substrate doesn't yet thread them; that lateral path is
  deferred.
- Status URLs containing the legacy `/i/web/status/<id>` form go through
  the reserved-user filter and produce no candidates.
