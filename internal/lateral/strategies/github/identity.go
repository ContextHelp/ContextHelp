package github

import (
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// Identity-key namespace prefixes. The substrate's identity resolver
// looks up canonicals by these keys when a Candidate's URL doesn't
// match a known graph node.
//
// Format: @github.<scope>.<value>
//
// scope ∈ {repo, user, org}.
//
//   - @github.repo.<owner>/<name>   — a single repository
//   - @github.user.<login>          — a github user account
//   - @github.org.<login>           — a github organization
//
// User vs. org disambiguation: the strategy emits @github.user.* by
// default. When a UserSummary or OrgSummary surfaces with Type =
// "Organization", the strategy promotes the key to @github.org.* via
// userCandidate / orgCandidate. For URL-only candidates where the
// strategy can't tell user from org without an API call, @github.user.*
// is emitted; the resolver has a follow-on rule (out of github-pkg
// scope) to reconcile the two when necessary.
const (
	IdentityKeyPrefixRepo = "@github.repo."
	IdentityKeyPrefixUser = "@github.user."
	IdentityKeyPrefixOrg  = "@github.org."
)

// ExtractIdentityKey lifts the identity key from a Candidate. Reads
// the typed Candidate.IdentityKey field first, falling back to the
// legacy Preview[PreviewKeyIdentityKey] entry for backward compat
// during the T-0309 migration window. Returns "" when neither is set.
//
// Deprecated: prefer reading lateral.Candidate.IdentityKey directly,
// or use identitykey.Of(c) for the same dual-read semantics applied
// across all platforms (not just github). This helper survives one
// minor cycle so external callers keep compiling.
func ExtractIdentityKey(c lateral.Candidate) string {
	if c.IdentityKey != "" {
		return c.IdentityKey
	}
	if c.Preview == nil {
		return ""
	}
	v, ok := c.Preview[PreviewKeyIdentityKey]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// IdentityKeyKind tags the scope of an identity key.
type IdentityKeyKind int

const (
	// IdentityKeyKindUnknown is returned for unrecognized keys (or "").
	IdentityKeyKindUnknown IdentityKeyKind = iota
	IdentityKeyKindRepo
	IdentityKeyKindUser
	IdentityKeyKindOrg
)

// String returns the lowercase kind name.
func (k IdentityKeyKind) String() string {
	switch k {
	case IdentityKeyKindRepo:
		return "repo"
	case IdentityKeyKindUser:
		return "user"
	case IdentityKeyKindOrg:
		return "org"
	default:
		return "unknown"
	}
}

// ParsedIdentityKey is the structured form of an identity key.
type ParsedIdentityKey struct {
	Kind  IdentityKeyKind
	Owner string // for repo: the owner; for user/org: empty
	Name  string // for repo: the repo name; for user/org: the login
	Login string // alias for Name when Kind ∈ {user, org}; empty for repo
}

// ParseIdentityKey decomposes a github identity key into its parts.
// Returns Kind=Unknown for keys outside the @github.* namespace.
//
// Examples:
//   - "@github.repo.samber/lo" → {Repo, "samber", "lo", ""}
//   - "@github.user.jadb"      → {User, "", "jadb", "jadb"}
//   - "@github.org.acme"       → {Org,  "", "acme", "acme"}
func ParseIdentityKey(key string) ParsedIdentityKey {
	switch {
	case strings.HasPrefix(key, IdentityKeyPrefixRepo):
		rest := strings.TrimPrefix(key, IdentityKeyPrefixRepo)
		owner, name, ok := strings.Cut(rest, "/")
		if !ok {
			return ParsedIdentityKey{Kind: IdentityKeyKindUnknown}
		}
		return ParsedIdentityKey{Kind: IdentityKeyKindRepo, Owner: owner, Name: name}
	case strings.HasPrefix(key, IdentityKeyPrefixUser):
		login := strings.TrimPrefix(key, IdentityKeyPrefixUser)
		if login == "" {
			return ParsedIdentityKey{Kind: IdentityKeyKindUnknown}
		}
		return ParsedIdentityKey{Kind: IdentityKeyKindUser, Name: login, Login: login}
	case strings.HasPrefix(key, IdentityKeyPrefixOrg):
		login := strings.TrimPrefix(key, IdentityKeyPrefixOrg)
		if login == "" {
			return ParsedIdentityKey{Kind: IdentityKeyKindUnknown}
		}
		return ParsedIdentityKey{Kind: IdentityKeyKindOrg, Name: login, Login: login}
	default:
		return ParsedIdentityKey{Kind: IdentityKeyKindUnknown}
	}
}
