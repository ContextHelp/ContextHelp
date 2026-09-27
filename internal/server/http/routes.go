package http

import (
	"net/http"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// apiRoute is one /api/v1 route and the scope it requires. The table
// in apiRoutes is the only place /api/v1 routes are declared: the
// router mounts every entry through RequireScope, and a test walks the
// router so a route added any other way fails.
type apiRoute struct {
	// Method is the HTTP method; "" mounts the handler for every method.
	Method string
	// Pattern is the chi pattern relative to /api/v1.
	Pattern string
	// Scope is the scope a principal needs to reach the route.
	Scope   authn.Scope
	Handler http.Handler
}

// apiRoutes is the /api/v1 route-to-scope table.
func apiRoutes(svc *service.Service, rc RouterConfig) []apiRoute {
	mgr := rc.Watcher
	get, post, patch, del := http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete
	return []apiRoute{
		// Identity
		{Method: get, Pattern: "/whoami", Scope: authn.ScopeReadSystem, Handler: Whoami(rc.Auth != nil)},

		// Objects
		{Method: get, Pattern: "/objects", Scope: authn.ScopeReadObjects, Handler: ListObjects(svc)},
		{Method: get, Pattern: "/objects/facets", Scope: authn.ScopeReadObjects, Handler: ObjectFacets(svc)},
		{Method: get, Pattern: "/objects/{id}", Scope: authn.ScopeReadObjects, Handler: GetObject(svc)},
		{Method: get, Pattern: "/objects/{id}/related", Scope: authn.ScopeReadObjects, Handler: RelatedObjects(svc)},
		{Method: patch, Pattern: "/objects/{id}", Scope: authn.ScopeWriteObjects, Handler: UpdateObject(svc)},
		{Method: del, Pattern: "/objects/{id}", Scope: authn.ScopeDeleteObjects, Handler: DeleteObject(svc)},

		// Analyze
		{Method: post, Pattern: "/analyze", Scope: authn.ScopeWriteObjects, Handler: Analyze(svc)},

		// Jobs
		{Method: get, Pattern: "/jobs", Scope: authn.ScopeReadJobs, Handler: ListJobs(svc)},
		{Method: get, Pattern: "/jobs/{id}", Scope: authn.ScopeReadJobs, Handler: GetJob(svc)},
		{Method: post, Pattern: "/jobs/{id}/retry", Scope: authn.ScopeWriteJobs, Handler: RetryJob(svc)},

		// Search
		{Method: get, Pattern: "/search", Scope: authn.ScopeReadObjects, Handler: Search(svc)},
		{Method: post, Pattern: "/find", Scope: authn.ScopeReadObjects, Handler: Find(svc, rc.Semantic)},

		// Entities. The entity-serving surface is where inbound
		// entitlements and metering bite for non-admin principals.
		{Method: get, Pattern: "/entities", Scope: authn.ScopeReadObjects, Handler: ListEntities(svc, rc.Entitlements)},
		{Method: get, Pattern: "/entities/{slug}", Scope: authn.ScopeReadObjects, Handler: GetEntity(svc, rc.Entitlements)},
		{Method: get, Pattern: "/entities/{slug}/backlinks", Scope: authn.ScopeReadObjects, Handler: EntityBacklinks(svc, rc.Entitlements)},
		// Thin sync: promote a thin entity to full on demand.
		{Method: post, Pattern: "/entities/{slug}/pull", Scope: authn.ScopeWriteObjects, Handler: PullEntity(svc, rc.Entitlements)},
		// Thin sync: trigger entity index sync for a registry.
		{Method: post, Pattern: "/entities/registry-sync", Scope: authn.ScopeSyncRegistries, Handler: SyncRegistryEntities(svc)},

		// Pipelines
		{Method: post, Pattern: "/pipelines", Scope: authn.ScopeAdminPipelines, Handler: CreatePipeline(svc)},
		{Method: get, Pattern: "/pipelines", Scope: authn.ScopeReadSystem, Handler: ListPipelines(svc)},
		{Method: get, Pattern: "/pipelines/{name}", Scope: authn.ScopeReadSystem, Handler: GetPipeline(svc)},
		{Method: del, Pattern: "/pipelines/{name}", Scope: authn.ScopeAdminPipelines, Handler: DeletePipeline(svc)},
		{Method: post, Pattern: "/pipelines/{name}/archive", Scope: authn.ScopeAdminPipelines, Handler: ArchivePipeline(svc)},
		{Method: post, Pattern: "/pipelines/{name}/unarchive", Scope: authn.ScopeAdminPipelines, Handler: UnarchivePipeline(svc)},
		{Method: post, Pattern: "/pipelines/enqueue", Scope: authn.ScopeWriteObjects, Handler: Enqueue(svc)},

		// Steps
		{Method: get, Pattern: "/steps", Scope: authn.ScopeReadSystem, Handler: ListSteps(svc)},
		{Method: get, Pattern: "/steps/{name}", Scope: authn.ScopeReadSystem, Handler: GetStep(svc)},
		{Method: post, Pattern: "/steps/install", Scope: authn.ScopeAdminPlugins, Handler: InstallStep(svc)},
		{Method: del, Pattern: "/steps/{name}", Scope: authn.ScopeAdminPlugins, Handler: UninstallStep(svc)},

		// Registries
		{Method: post, Pattern: "/steps/registries/fetch", Scope: authn.ScopeSyncRegistries, Handler: FetchRegistry(svc)},
		{Method: post, Pattern: "/steps/registries/{url}/update", Scope: authn.ScopeSyncRegistries, Handler: UpdateRegistry(svc)},
		{Method: get, Pattern: "/steps/registries", Scope: authn.ScopeReadRegistries, Handler: ListRegistries(svc)},

		// Feeds
		{Method: post, Pattern: "/feeds", Scope: authn.ScopeWriteFeeds, Handler: CreateFeed(svc)},
		{Method: get, Pattern: "/feeds", Scope: authn.ScopeReadFeeds, Handler: ListFeeds(svc)},
		{Method: post, Pattern: "/feeds/sync", Scope: authn.ScopeWriteFeeds, Handler: SyncAllFeeds(svc)},
		{Method: post, Pattern: "/feeds/{id}/sync", Scope: authn.ScopeWriteFeeds, Handler: SyncFeed(svc)},
		{Method: del, Pattern: "/feeds/{id}", Scope: authn.ScopeDeleteFeeds, Handler: DeleteFeed(svc)},

		// Import
		{Method: post, Pattern: "/import", Scope: authn.ScopeWriteObjects, Handler: CreateImport(svc)},
		{Method: get, Pattern: "/import/{id}", Scope: authn.ScopeReadJobs, Handler: GetImport(svc)},

		// Importers
		{Method: post, Pattern: "/importers/dropbox/run", Scope: authn.ScopeWriteObjects, Handler: RunDropboxImport(svc)},
		{Method: post, Pattern: "/importers/slack/run", Scope: authn.ScopeWriteObjects, Handler: RunSlackImport(svc)},
		{Method: post, Pattern: "/importers/discord/run", Scope: authn.ScopeWriteObjects, Handler: RunDiscordImport(svc)},
		{Method: get, Pattern: "/importers/runs/{id}", Scope: authn.ScopeReadJobs, Handler: GetImporterRun(svc)},

		// System
		{Method: get, Pattern: "/system/reminders", Scope: authn.ScopeReadSystem, Handler: ListReminders(svc)},
		{Method: post, Pattern: "/system/reminders/{id}/dismiss", Scope: authn.ScopeWriteSystem, Handler: DismissReminder(svc)},

		// Watches. Mutations are admin-only: a server-side watch reads
		// the server's filesystem.
		{Method: post, Pattern: "/watches", Scope: authn.ScopeAdminWatches, Handler: CreateWatch(svc, mgr)},
		{Method: get, Pattern: "/watches", Scope: authn.ScopeReadSystem, Handler: ListWatches(svc)},
		{Method: get, Pattern: "/watches/{id}", Scope: authn.ScopeReadSystem, Handler: GetWatch(svc)},
		{Method: patch, Pattern: "/watches/{id}", Scope: authn.ScopeAdminWatches, Handler: UpdateWatch(svc, mgr)},
		{Method: del, Pattern: "/watches/{id}", Scope: authn.ScopeAdminWatches, Handler: DeleteWatch(svc, mgr)},
		{Method: post, Pattern: "/watches/{id}/pause", Scope: authn.ScopeAdminWatches, Handler: PauseWatch(mgr)},
		{Method: post, Pattern: "/watches/{id}/resume", Scope: authn.ScopeAdminWatches, Handler: ResumeWatch(mgr)},
		{Method: get, Pattern: "/watches/{id}/files", Scope: authn.ScopeReadSystem, Handler: ListWatchFiles(svc)},

		// Inbox
		{Method: post, Pattern: "/inbox", Scope: authn.ScopeWriteInbox, Handler: CaptureInbox(svc)},
		{Method: get, Pattern: "/inbox", Scope: authn.ScopeReadInbox, Handler: ListInbox(svc)},
		{Method: post, Pattern: "/inbox/{id}/triage", Scope: authn.ScopeProcessInbox, Handler: TriageInbox(svc)},
		{Method: post, Pattern: "/inbox/{id}/discard", Scope: authn.ScopeProcessInbox, Handler: DiscardInbox(svc)},

		// SSE event stream (real-time updates)
		{Method: get, Pattern: "/events", Scope: authn.ScopeReadObjects, Handler: HandleSSE(svc)},

		// Suggestions (autosuggest plugin)
		{Method: get, Pattern: "/suggestions", Scope: authn.ScopeReadObjects, Handler: ListSuggestions(svc)},
		{Method: post, Pattern: "/suggestions/{id}/approve", Scope: authn.ScopeWriteObjects, Handler: ApproveSuggestion(svc)},
		{Method: post, Pattern: "/suggestions/{id}/reject", Scope: authn.ScopeWriteObjects, Handler: RejectSuggestion(svc)},

		// Aliases (aliasing plugin)
		{Method: post, Pattern: "/aliases", Scope: authn.ScopeWriteObjects, Handler: CreateAlias(svc)},
		{Method: get, Pattern: "/aliases", Scope: authn.ScopeReadObjects, Handler: ListAliases(svc)},
		{Method: get, Pattern: "/aliases/{alias}", Scope: authn.ScopeReadObjects, Handler: ResolveAlias(svc)},
		{Method: del, Pattern: "/aliases/{alias}", Scope: authn.ScopeDeleteObjects, Handler: DeleteAlias(svc)},

		// Capture (browser extension)
		{Method: post, Pattern: "/capture/page", Scope: authn.ScopeWriteObjects, Handler: CapturePage(svc)},
		{Method: post, Pattern: "/capture/selection", Scope: authn.ScopeWriteObjects, Handler: CaptureSelection(svc)},
		{Method: post, Pattern: "/capture/element", Scope: authn.ScopeWriteObjects, Handler: CaptureElement(svc)},
		{Method: get, Pattern: "/capture/recent", Scope: authn.ScopeReadJobs, Handler: ListRecentCaptures(svc)},

		// Saved searches
		{Method: post, Pattern: "/saved-searches", Scope: authn.ScopeWriteObjects, Handler: CreateSavedSearch(svc)},
		{Method: get, Pattern: "/saved-searches", Scope: authn.ScopeReadObjects, Handler: ListSavedSearches(svc)},
		{Method: get, Pattern: "/saved-searches/{name}", Scope: authn.ScopeReadObjects, Handler: GetSavedSearch(svc)},
		{Method: del, Pattern: "/saved-searches/{name}", Scope: authn.ScopeDeleteObjects, Handler: DeleteSavedSearch(svc)},

		// Search history
		{Method: get, Pattern: "/search-history", Scope: authn.ScopeReadObjects, Handler: ListSearchHistory(svc)},
		{Method: del, Pattern: "/search-history", Scope: authn.ScopeDeleteObjects, Handler: ClearSearchHistory(svc)},

		// Audit log
		{Method: get, Pattern: "/audit-log", Scope: authn.ScopeAdminAudit, Handler: ListAuditLog(svc)},

		// Federation push needs write:objects AND the federation
		// credential: on non-private instances federation.token is
		// mandatory and the handler checks it itself.
		{Method: post, Pattern: "/federation/push", Scope: authn.ScopeWriteObjects,
			Handler: FederationPush(svc, rc.RequireFederationCredential)},

		// MCP read surface (ADR-068): JSON-RPC 2.0 over POST, mounted
		// as a sibling of the REST routes. Tool dispatch happens inside
		// the handler; every tool is a read.
		{Pattern: "/mcp/", Scope: authn.ScopeReadObjects, Handler: mountMCP(svc)},
		{Pattern: "/mcp", Scope: authn.ScopeReadObjects, Handler: mountMCP(svc)},
	}
}
