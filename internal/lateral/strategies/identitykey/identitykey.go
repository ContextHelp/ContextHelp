// Package identitykey defines the per-platform identity-key conventions
// the lateral substrate's identity resolver consults to dedup candidates
// across captures.
//
// # Contract
//
// Every Candidate that represents an identifiable real-world entity
// (person, publication, repository, paper, video, organisation) carries
// an identity_key entry under Candidate.Preview. The resolver uses
// equality on identity_key to merge candidates that point at the same
// underlying entity even when their URLs differ (e.g. shortlinks, mobile
// hosts, custom domains, locale prefixes).
//
// Identity keys are opaque, lowercase, slash-separated strings of the
// form:
//
//	<platform>/<entity_type>/<id_part_1>[/<id_part_2>...]
//
// or, for locale-scoped entities:
//
//	<platform>/<entity_type>/<locale>/<id_part_1>[/<id_part_2>...]
//
// Examples:
//
//	github/repo/samber/lo
//	github/owner/jadb
//	youtube/channel/uc12345/uploads
//	substack/publication/anthropic-research
//	wikipedia/article/en/turing_machine
//	arxiv/paper/2401.12345
//
// Strategies SHOULD set Preview[KeyField] via Build / BuildLocalised.
// Resolver code SHOULD read it via Get and decompose it via Parse.
package identitykey

import (
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// KeyField is the Candidate.Preview map key under which the identity
// key string lives. Constant so resolver and strategy code stay in
// sync.
const KeyField = "identity_key"

// Entity-type segment vocabulary. Strategies pick the entry that best
// describes what the candidate URL points at. New entries are additive
// — the resolver treats unknown entity types as opaque.
const (
	EntityOwner       = "owner"
	EntityRepository  = "repo"
	EntityProfile     = "profile"
	EntityChannel     = "channel"
	EntityVideo       = "video"
	EntityPlaylist    = "playlist"
	EntityArticle     = "article"
	EntityPost        = "post"
	EntityPublication = "publication"
	EntityOrg         = "org"
	EntityPaper       = "paper"
	EntityNote        = "note"
	EntityTweet       = "tweet"
	EntityThread      = "thread"
	EntityUser        = "user"
	EntitySearch      = "search"
	EntityTopic       = "topic"
	EntityTrend       = "trend"
	EntityAdvisory    = "advisory"
	EntityGist        = "gist"
)

// Build composes a platform / entityType / id key. id can be a single
// canonical identifier ("samber-lo") or multiple segments that form one
// logical id ("samber", "lo" → "samber/lo"). Each segment is normalised
// (trimmed + lowercased); slashes embedded in any single segment escape
// to underscores so the resulting key remains parseable.
//
// Build returns "" when any of these would produce a non-unique key:
//   - platform normalises to empty
//   - entityType normalises to empty
//   - all id segments normalise to empty (i.e. no usable id)
//
// Callers must treat "" as "skip the candidate" or "don't emit
// identity_key" — emitting a candidate with an empty / "platform/entity"
// key would let the resolver merge unrelated entities under one key.
//
// Examples:
//
//	Build("github", "repo", "samber", "lo")     → "github/repo/samber/lo"
//	Build("substack", "publication", "anthropic-research")
//	                                            → "substack/publication/anthropic-research"
//	Build("youtube", "channel", "uc12345", "uploads")
//	                                            → "youtube/channel/uc12345/uploads"
//	Build("github", "repo", "owner/with/slash") → "github/repo/owner_with_slash"
//	Build("github", "repo")                     → ""  (no id)
//	Build("github", "repo", "", "  ")           → ""  (all id parts empty)
//	Build("", "repo", "samber")                 → ""  (no platform)
func Build(platform, entityType string, id ...string) string {
	p := normalize(platform)
	e := normalize(entityType)
	if p == "" || e == "" {
		return ""
	}
	out := []string{p, e}
	for _, part := range id {
		n := normalize(part)
		if n == "" {
			continue
		}
		out = append(out, escape(n))
	}
	if len(out) == 2 {
		// Only platform + entity remain — no id segments survived.
		return ""
	}
	return strings.Join(out, "/")
}

// BuildLocalised is Build with an interleaved locale segment, used for
// platforms whose canonical entity is locale-scoped (Wikipedia language
// editions). Locale precedes id and is itself normalised + escaped.
//
// BuildLocalised returns "" under the same conditions as Build, plus
// when locale normalises to empty — a localised key without a locale
// is malformed and would parse ambiguously.
func BuildLocalised(platform, entityType, locale string, id ...string) string {
	p := normalize(platform)
	e := normalize(entityType)
	l := normalize(locale)
	if p == "" || e == "" || l == "" {
		return ""
	}
	out := []string{p, e, escape(l)}
	for _, part := range id {
		n := normalize(part)
		if n == "" {
			continue
		}
		out = append(out, escape(n))
	}
	if len(out) == 3 {
		return ""
	}
	return strings.Join(out, "/")
}

// Parsed is the structured form of an identity key. IDParts holds the id
// segments after platform / entityType / (optional locale).
type Parsed struct {
	Platform   string
	EntityType string
	Locale     string   // empty for non-localised keys
	IDParts    []string // never empty for a well-formed key; may be empty if Parse rejected the input
	Localised  bool     // true if the caller used BuildLocalised
}

// Parse decomposes an identity key into its segments. If localised is
// true, the third segment is interpreted as a locale and the remainder
// is the id; otherwise the third+ segments are id parts and Locale is
// empty.
//
// Returns a zero-value Parsed when key has fewer than three segments.
// Round-trips Build and BuildLocalised so callers can rebuild the key
// from the result.
//
// Parse cannot tell a localised key from a non-localised one purely by
// shape (both look like /platform/entity/segment[/segment...]); the
// caller passes localised explicitly because only the platform itself
// knows. Wikipedia callers pass true; everyone else passes false.
func Parse(key string, localised bool) Parsed {
	parts := strings.Split(key, "/")
	if len(parts) < 3 {
		return Parsed{}
	}
	p := Parsed{
		Platform:   parts[0],
		EntityType: parts[1],
		Localised:  localised,
	}
	if localised {
		if len(parts) < 4 {
			return Parsed{}
		}
		p.Locale = parts[2]
		p.IDParts = parts[3:]
	} else {
		p.IDParts = parts[2:]
	}
	return p
}

// Get returns the identity key string from preview, or "" if missing.
//
// Get is the legacy reader against the Preview-map form. New code
// SHOULD prefer the typed lateral.Candidate.IdentityKey field; the
// substrate's identity resolver consults the typed field first and
// falls back to Get for one minor cycle (T-0307 / T-0309).
func Get(preview map[string]any) string {
	if preview == nil {
		return ""
	}
	v, _ := preview[KeyField].(string)
	return v
}

// Set writes key into preview under KeyField. Returns preview so it can
// be chained at the end of a builder. Allocates a new map if preview is
// nil.
//
// Deprecated: prefer setting lateral.Candidate.IdentityKey directly (or
// via SetField). Set survives for one minor cycle so legacy strategies
// keep compiling; T-0309 sweeps the in-tree call sites onto the typed
// field.
func Set(preview map[string]any, key string) map[string]any {
	if preview == nil {
		preview = map[string]any{}
	}
	preview[KeyField] = key
	return preview
}

// Of returns the canonical identity key for c. The typed IdentityKey
// field wins when non-empty; otherwise Of falls back to the legacy
// Preview[KeyField] entry. Substrate adapters wiring lateral.Candidate
// to identity.Candidate use Of so both forms feed the resolver
// identically during the T-0309 migration window.
//
// When both are set with conflicting non-empty values, the typed field
// wins — by intent. The typed field is the contract; Preview is the
// legacy back-channel.
func Of(c lateral.Candidate) string {
	if c.IdentityKey != "" {
		return c.IdentityKey
	}
	return Get(c.Preview)
}

// SetField writes key onto c.IdentityKey. Returns c so it can be
// chained at the end of a builder. The typed-field counterpart to Set.
//
// SetField does NOT also write Preview[KeyField] — strategies that have
// migrated to the typed field stop emitting the Preview entry. Callers
// that want both forms (transitional code) call Set(c.Preview, key)
// alongside SetField.
func SetField(c lateral.Candidate, key string) lateral.Candidate {
	c.IdentityKey = key
	return c
}

// HostBackedIDParts returns stable identity-key id segments derived
// from rawURL's host + path. Useful when a strategy doesn't have a
// better canonical identifier (e.g. unknown CMS, generic search-result
// URLs): the resolver still gets a deterministic dedup key from the
// URL structure.
//
// Returned segments are: [host] when path is empty, or
// [host, pathPart1, pathPart2, ...] when path has segments. The host is
// stripped of "www." and lowercased; path segments are lowercased.
//
// Returns nil when rawURL is unparseable or has no host. Callers MUST
// check for nil — passing it through Build always yields "" by Build's
// own contract, but writing the check at the call site lets the strategy
// decline to emit the candidate entirely (preferred) rather than emit
// one without an identity_key.
//
// Recommended usage:
//
//	parts := identitykey.HostBackedIDParts(url)
//	if parts == nil {
//	    return // skip this candidate
//	}
//	key := identitykey.Build("google", identitykey.EntityArticle, parts...)
func HostBackedIDParts(rawURL string) []string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	path := strings.TrimPrefix(strings.ToLower(u.Path), "/")
	if path == "" {
		return []string{host}
	}
	out := []string{host}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" {
			continue
		}
		out = append(out, seg)
	}
	return out
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func escape(s string) string {
	return strings.ReplaceAll(s, "/", "_")
}
