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
// Empty or whitespace-only id segments are dropped — pass at least one
// non-empty segment to produce a usable key.
//
// Examples:
//
//	Build("github", "repo", "samber", "lo")     → "github/repo/samber/lo"
//	Build("substack", "publication", "anthropic-research")
//	                                            → "substack/publication/anthropic-research"
//	Build("youtube", "channel", "uc12345", "uploads")
//	                                            → "youtube/channel/uc12345/uploads"
//	Build("github", "repo", "owner/with/slash") → "github/repo/owner_with_slash"
func Build(platform, entityType string, id ...string) string {
	out := []string{normalize(platform), normalize(entityType)}
	for _, part := range id {
		n := normalize(part)
		if n == "" {
			continue
		}
		out = append(out, escape(n))
	}
	return strings.Join(out, "/")
}

// BuildLocalised is Build with an interleaved locale segment, used for
// platforms whose canonical entity is locale-scoped (Wikipedia language
// editions). Locale precedes id and is itself normalised + escaped.
func BuildLocalised(platform, entityType, locale string, id ...string) string {
	out := []string{normalize(platform), normalize(entityType), escape(normalize(locale))}
	for _, part := range id {
		n := normalize(part)
		if n == "" {
			continue
		}
		out = append(out, escape(n))
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
func Set(preview map[string]any, key string) map[string]any {
	if preview == nil {
		preview = map[string]any{}
	}
	preview[KeyField] = key
	return preview
}

// HostBackedID returns a stable identity-key id segment derived from
// rawURL's host + path. Useful when a strategy doesn't have a better
// canonical identifier (e.g. unknown CMS): the resolver still gets a
// deterministic dedup key. The host is stripped of "www." and the path
// is lowercased + leading-slash-trimmed.
//
// HostBackedID returns "" when rawURL has no host — callers MUST guard
// against this (Copilot pointed out that downstream strategies were
// emitting non-unique keys like "q|" because they passed "" through to
// Build unchecked).
func HostBackedID(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	path := strings.TrimPrefix(strings.ToLower(u.Path), "/")
	if path == "" {
		return host
	}
	return host + "/" + path
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func escape(s string) string {
	return strings.ReplaceAll(s, "/", "_")
}
