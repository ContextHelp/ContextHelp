package auth

import (
	"fmt"
	"slices"
	"strings"
)

// Scope is one ADR-023 capability in its `verb:resource` grammar. Every
// /api/v1 route and every gRPC method requires exactly one scope; a
// principal reaches the route only when its effective scope set holds
// it. Roles never reach the transports: they expand to scope bundles
// at authentication time (see ScopesForRoles).
type Scope string

// Scopes. Only scopes that guard a route exist here: a new scope lands
// together with the first route that requires it.
const (
	// ScopeReadObjects reads knowledge: objects, search and the search
	// graph, entities, aliases, saved searches, search history,
	// suggestions and the event stream.
	ScopeReadObjects Scope = "read:objects"
	// ScopeWriteObjects creates and updates knowledge: analyze, capture,
	// imports, enqueues, object edits, aliases, saved searches,
	// suggestion review and entity pulls.
	ScopeWriteObjects Scope = "write:objects"
	// ScopeDeleteObjects deletes objects.
	ScopeDeleteObjects Scope = "delete:objects"
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

	// ScopeReadRegistries lists subscribed knowledge registries.
	ScopeReadRegistries Scope = "read:registries"
	// ScopeSyncRegistries fetches, updates and syncs registries.
	ScopeSyncRegistries Scope = "sync:registries"

	// ScopeReadSystem reads system reminders and the caller's own
	// identity (whoami).
	ScopeReadSystem Scope = "read:system"
	// ScopeWriteSystem dismisses system reminders.
	ScopeWriteSystem Scope = "write:system"

	// ScopeReadPipelines reads pipelines and installed steps.
	ScopeReadPipelines Scope = "read:pipelines"
	// ScopeAdminPipelines creates, deletes, archives and unarchives
	// pipelines.
	ScopeAdminPipelines Scope = "admin:pipelines"
	// ScopeAdminPlugins installs and uninstalls pipeline steps.
	ScopeAdminPlugins Scope = "admin:plugins"

	// ScopeReadWatches reads server-side watches and their files: the
	// server's filesystem layout.
	ScopeReadWatches Scope = "read:watches"
	// ScopeAdminWatches creates, changes, pauses, resumes and deletes
	// server-side watches, which read the server's filesystem.
	ScopeAdminWatches Scope = "admin:watches"

	// ScopeAdminAudit reads the audit log.
	ScopeAdminAudit Scope = "admin:audit"

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
	ScopeReadObjects, ScopeWriteObjects, ScopeDeleteObjects,
	ScopeDeleteAliases, ScopeDeleteSearches, ScopeReadMCP,
	ScopeReadInbox, ScopeWriteInbox, ScopeProcessInbox,
	ScopeReadFeeds, ScopeWriteFeeds, ScopeDeleteFeeds,
	ScopeReadJobs, ScopeWriteJobs,
	ScopeReadRegistries, ScopeSyncRegistries,
	ScopeReadSystem, ScopeWriteSystem,
	ScopeReadPipelines, ScopeAdminPipelines, ScopeAdminPlugins,
	ScopeReadWatches, ScopeAdminWatches,
	ScopeAdminAudit,
	ScopeReadUI, ScopeSignoutUI,
}

// sessionOnlyScopes exist only in a browser session's own set: no role
// bundle grants them, and a session holds them whatever its minting
// principal holds.
var sessionOnlyScopes = []Scope{ScopeSignoutUI}

// UISessionScopes is the fixed scope set of a web UI browser session:
// the reads the web UI makes, its own writes (delete an object, retry a
// job, sign out) and nothing else. No MCP, pipelines, steps, watches,
// audit log or login codes (read:ui); no other write. A session's
// effective scopes are these intersected with its minting principal's
// scopes, plus signout:ui, which only sessions hold (SessionScopesFor),
// so a session never holds a scope its token lacks.
var UISessionScopes = []Scope{
	ScopeReadObjects, ScopeDeleteObjects,
	ScopeReadInbox,
	ScopeReadFeeds,
	ScopeReadJobs, ScopeWriteJobs,
	ScopeReadRegistries,
	ScopeReadSystem,
	ScopeSignoutUI,
}

// Scope verbs the role bundles are built from.
const (
	verbRead  = "read"
	verbWrite = "write"
)

// Verb returns the part of the scope before the colon (read, write,
// delete, process, sync, admin, signout).
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

// Bundle returns the scopes a role grants, in AllScopes order. ok is
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
		if keep(s) && !slices.Contains(sessionOnlyScopes, s) {
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

// BypassesEntityGate reports whether the principal skips the inbound
// entitlement and metering gate on entity reads. The gate exists for
// third-party consumers; an admin principal is the instance owner.
func (p *Principal) BypassesEntityGate() bool {
	return p.HasRole(RoleAdmin)
}

// LocalPrincipal is the identity a private instance (no auth provider,
// loopback only) reports for its callers. Private instances grant every
// scope; the principal is never attached to request contexts, so audit
// and policy records keep treating those callers as anonymous.
func LocalPrincipal() *Principal {
	return &Principal{
		ID:       "local",
		Name:     "local",
		Provider: "none",
		Roles:    []string{RoleAdmin},
		Scopes:   slices.Clone(AllScopes),
	}
}
