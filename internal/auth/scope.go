package auth

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Scope is one ADR-023 capability in its `verb:resource` grammar. Every
// /api/v1 route and every gRPC method requires exactly one scope; a
// principal reaches the route only when its effective scope set holds
// it. Roles never reach the transports: they expand to scope bundles
// at authentication time (see ScopesForRoles).
type Scope string

// Scopes. A scope lands together with the first route that requires
// it, with one exception: the principal-registry scopes of ADR-079 §5
// (admin:principals, admin:shares, write:self) are here ahead of their
// routes, so keys minted before those routes ship already carry them.
const (
	// ScopeReadObjects reads knowledge: objects, search and the search
	// graph, entities, aliases, saved searches, search history,
	// suggestions and the event stream.
	ScopeReadObjects Scope = "read:objects"
	// ScopeWriteObjects creates and updates knowledge: analyze, capture,
	// imports, enqueues, object edits, links, aliases, saved searches,
	// suggestion review and entity pulls.
	ScopeWriteObjects Scope = "write:objects"
	// ScopeDeleteObjects deletes objects.
	ScopeDeleteObjects Scope = "delete:objects"
	// ScopeDeleteEdges deletes typed links between objects. Split from
	// delete:objects, which a web UI session holds: a session never
	// deletes links.
	ScopeDeleteEdges Scope = "delete:edges"
	// ScopeDeleteAliases deletes aliases.
	ScopeDeleteAliases Scope = "delete:aliases"
	// ScopeDeleteSearches deletes saved searches and clears the search
	// history.
	ScopeDeleteSearches Scope = "delete:searches"
	// ScopeReadMCP reads knowledge through the MCP mount, the agent
	// surface.
	ScopeReadMCP Scope = "read:mcp"

	// ScopeReadInbox lists inbox items and the inbox queue.
	ScopeReadInbox Scope = "read:inbox"
	// ScopeWriteInbox adds items to the inbox.
	ScopeWriteInbox Scope = "write:inbox"
	// ScopeProcessInbox triages, discards and clears inbox items.
	ScopeProcessInbox Scope = "process:inbox"

	// ScopeReadFeeds lists feed subscriptions.
	ScopeReadFeeds Scope = "read:feeds"
	// ScopeWriteFeeds adds and syncs feed subscriptions.
	ScopeWriteFeeds Scope = "write:feeds"
	// ScopeDeleteFeeds removes feed subscriptions.
	ScopeDeleteFeeds Scope = "delete:feeds"

	// ScopeReadJobs reads jobs, import batches and importer runs.
	ScopeReadJobs Scope = "read:jobs"
	// ScopeWriteJobs retries jobs.
	ScopeWriteJobs Scope = "write:jobs"
	// ScopeCancelJobs cancels pending and running jobs. Split from
	// write:jobs: a cancel abandons work in flight, so like the other
	// destructive verbs only the admin role holds it, and a web UI
	// session never does.
	ScopeCancelJobs Scope = "cancel:jobs"

	// ScopeReadRegistries lists subscribed knowledge registries.
	ScopeReadRegistries Scope = "read:registries"
	// ScopeSyncRegistries fetches, updates and syncs registries, and
	// lists, approves and rejects the entities untrusted registries
	// served (the approval queue).
	ScopeSyncRegistries Scope = "sync:registries"

	// ScopeReadSystem reads system reminders and the caller's own
	// identity (whoami).
	ScopeReadSystem Scope = "read:system"
	// ScopeWriteSystem dismisses system reminders.
	ScopeWriteSystem Scope = "write:system"

	// ScopeWriteSelf lets a principal manage its own credentials and
	// sessions: its own API keys, passkeys and browser sessions, never
	// another principal's. Every role bundle and every browser session
	// holds it; a route under it checks that the target belongs to the
	// caller (Principal.IsSelf).
	ScopeWriteSelf Scope = "write:self"

	// ScopeReadPipelines reads pipelines and installed steps.
	ScopeReadPipelines Scope = "read:pipelines"
	// ScopeAdminPipelines creates, deletes, archives and unarchives
	// pipelines.
	ScopeAdminPipelines Scope = "admin:pipelines"
	// ScopeAdminSteps installs and uninstalls pipeline steps.
	ScopeAdminSteps Scope = "admin:steps"

	// ScopeReadWatches reads server-side watches and their files: the
	// server's filesystem layout.
	ScopeReadWatches Scope = "read:watches"
	// ScopeAdminWatches creates, changes, pauses, resumes and deletes
	// server-side watches, which read the server's filesystem.
	ScopeAdminWatches Scope = "admin:watches"

	// ScopeAdminAudit reads the audit log.
	ScopeAdminAudit Scope = "admin:audit"
	// ScopeAdminPrincipals manages principals, their identities, keys
	// and invites (ADR-079 §5).
	ScopeAdminPrincipals Scope = "admin:principals"
	// ScopeAdminShares manages audiences, share policies and ceilings
	// (ADR-080).
	ScopeAdminShares Scope = "admin:shares"

	// ScopeReadUI mints a web UI login code: it signs a browser in as
	// the caller, never with more than the caller holds. A read scope,
	// so every role holds it; a browser session never does
	// (UISessionScopes), so a session cannot mint another.
	ScopeReadUI Scope = "read:ui"
	// ScopeSignoutUI ends the caller's own browser session. Only a
	// browser session holds it (UISessionScopes); no role bundle does.
	ScopeSignoutUI Scope = "signout:ui"
)

// AllScopes lists every scope in a stable order: grouped by resource,
// read before write before the rest.
var AllScopes = []Scope{
	ScopeReadObjects, ScopeWriteObjects, ScopeDeleteObjects, ScopeDeleteEdges,
	ScopeDeleteAliases, ScopeDeleteSearches, ScopeReadMCP,
	ScopeReadInbox, ScopeWriteInbox, ScopeProcessInbox,
	ScopeReadFeeds, ScopeWriteFeeds, ScopeDeleteFeeds,
	ScopeReadJobs, ScopeWriteJobs, ScopeCancelJobs,
	ScopeReadRegistries, ScopeSyncRegistries,
	ScopeReadSystem, ScopeWriteSystem,
	ScopeWriteSelf,
	ScopeReadPipelines, ScopeAdminPipelines, ScopeAdminSteps,
	ScopeReadWatches, ScopeAdminWatches,
	ScopeAdminAudit, ScopeAdminPrincipals, ScopeAdminShares,
	ScopeReadUI, ScopeSignoutUI,
}

// sessionOnlyScopes exist only in a browser session's own set: no role
// bundle grants them, and a session holds them whatever its minting
// principal holds.
var sessionOnlyScopes = []Scope{ScopeSignoutUI}

// selfScopes are in every role bundle whatever its verbs: a principal
// of any role manages its own credentials (ADR-079 §5).
var selfScopes = []Scope{ScopeWriteSelf}

// UISessionScopes is the fixed scope set of a web UI browser session:
// the reads the web UI makes, its own writes (delete an object, retry a
// job, manage its own credentials, sign out), the Sharing page
// (admin:shares, ADR-080 §11: audiences, policies, grants, ceilings and
// invites, which only an admin's session reaches) and nothing else. No
// MCP, pipelines, steps, watches, audit log, principal registry or
// login codes (read:ui); no other write. A session's effective scopes
// are these intersected with its minting principal's scopes, plus
// signout:ui, which only sessions hold (SessionScopesFor), so a session
// never holds a scope its principal lacks.
var UISessionScopes = []Scope{
	ScopeReadObjects, ScopeDeleteObjects,
	ScopeReadInbox,
	ScopeReadFeeds,
	ScopeReadJobs, ScopeWriteJobs,
	ScopeReadRegistries,
	ScopeReadSystem,
	ScopeWriteSelf,
	ScopeAdminShares,
	ScopeSignoutUI,
}

// Scope verbs the role bundles are built from.
const (
	verbRead  = "read"
	verbWrite = "write"
)

// Verb returns the part of the scope before the colon (read, write,
// delete, cancel, process, sync, admin, signout).
func (s Scope) Verb() string {
	verb, _, _ := strings.Cut(string(s), ":")
	return verb
}

// Roles the static provider issues. Each is a fixed scope bundle.
const (
	// RoleAdmin holds every scope but the session-only ones and
	// bypasses the inbound entitlement and metering gate on entity
	// reads.
	RoleAdmin = "admin"
	// RoleWriter holds every read:* and write:* scope: capture-only
	// devices. No delete, inbox processing, sync or admin scopes.
	RoleWriter = "writer"
	// RoleReader holds every read:* scope.
	RoleReader = "reader"
)

// Roles lists the known roles, most to least privileged.
var Roles = []string{RoleAdmin, RoleWriter, RoleReader}

// Bundle returns the scopes a role grants, in AllScopes order: the
// role's verbs plus selfScopes, minus the session-only scopes. ok is
// false for an unknown role.
func Bundle(role string) (scopes []Scope, ok bool) {
	var keep func(Scope) bool
	switch role {
	case RoleAdmin:
		keep = func(Scope) bool { return true }
	case RoleWriter:
		keep = func(s Scope) bool { return s.Verb() == verbRead || s.Verb() == verbWrite }
	case RoleReader:
		keep = func(s Scope) bool { return s.Verb() == verbRead }
	default:
		return nil, false
	}
	for _, s := range AllScopes {
		if (keep(s) || slices.Contains(selfScopes, s)) && !slices.Contains(sessionOnlyScopes, s) {
			scopes = append(scopes, s)
		}
	}
	return scopes, true
}

// ValidateRoles rejects an empty role list and any unknown role. A
// token without a known role could reach nothing, so it is a config
// error rather than a silent lockout.
func ValidateRoles(roles []string) error {
	if len(roles) == 0 {
		return fmt.Errorf("at least one role is required (valid: %s)", strings.Join(Roles, ", "))
	}
	for _, r := range roles {
		if _, ok := Bundle(r); !ok {
			return fmt.Errorf("unknown role %q (valid: %s)", r, strings.Join(Roles, ", "))
		}
	}
	return nil
}

// ScopesForRoles returns the union of the roles' bundles in AllScopes
// order. Unknown roles contribute nothing.
func ScopesForRoles(roles []string) []Scope {
	granted := map[Scope]bool{}
	for _, r := range roles {
		b, _ := Bundle(r)
		for _, s := range b {
			granted[s] = true
		}
	}
	out := make([]Scope, 0, len(granted))
	for _, s := range AllScopes {
		if granted[s] {
			out = append(out, s)
		}
	}
	return out
}

// SessionScopesFor returns a session's effective scopes: the kind's
// fixed set intersected with the minting principal's scopes, plus the
// kind's session-only scopes (signout:ui), in AllScopes order. An
// unknown kind has no scopes.
func SessionScopesFor(principal []Scope, kind string) []Scope {
	set := SessionScopes(kind)
	out := []Scope{}
	for _, s := range AllScopes {
		if slices.Contains(set, s) && (slices.Contains(principal, s) || slices.Contains(sessionOnlyScopes, s)) {
			out = append(out, s)
		}
	}
	return out
}

// HasScope reports whether the principal's effective scope set holds s.
func (p *Principal) HasScope(s Scope) bool {
	return p != nil && slices.Contains(p.Scopes, s)
}

// IsSelf reports whether principalID names the caller itself: the
// owner-of-target check every route under write:self makes before it
// touches a credential or session, so a principal only ever manages its
// own. An empty id is never the caller.
func (p *Principal) IsSelf(principalID string) bool {
	return p != nil && principalID != "" && p.ID == principalID
}

// BypassesEntityGate reports whether the principal skips the inbound
// entitlement and metering gate on entity reads. The gate exists for
// third-party consumers; an admin principal is the instance owner.
func (p *Principal) BypassesEntityGate() bool {
	return p.HasRole(RoleAdmin)
}

// LocalPrincipal is the identity a private instance (no auth chain,
// loopback only) reports for its callers: every request there is the
// owner (ADR-079 §1). Private instances grant every scope; the
// principal is never attached to request contexts, so audit and policy
// records keep treating those callers as anonymous.
func LocalPrincipal() *Principal {
	return &Principal{
		ID:       storage.OwnerPrincipal,
		Name:     storage.OwnerPrincipal,
		Kind:     storage.PrincipalKindOwner,
		Provider: "none",
		Roles:    []string{RoleAdmin},
		Scopes:   slices.Clone(AllScopes),
	}
}
