package http

import (
	"context"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/mcp"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/ui"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

// RouterConfig bundles optional router wiring so the constructor
// signature stops growing per feature.
type RouterConfig struct {
	// DevCORS enables CORS for http://localhost:5173 (Vite dev server).
	DevCORS bool
	// Watcher is optional (nil-safe); nil disables live watch management
	// but keeps CRUD.
	Watcher *watcher.Manager
	// Probes injects runtime healthcheck signals into /healthz.
	Probes HealthzProbes
	// Auth guards the /api/v1 route table (MCP mount included) behind
	// the configured provider. nil = no inbound auth (private instance).
	// Health endpoints and static UI assets stay open for probes.
	Auth authn.Provider
	// Security receives auth-failure and ACL-denial events. nil = no
	// security event recording.
	Security *security.Emitter
	// Entitlements gates the entity-serving surface behind per-principal
	// namespace grants and metering quotas. nil = no inbound
	// entitlement enforcement (private instance).
	Entitlements *registry.InboundGate
	// RequireFederationCredential makes the federation.token credential
	// mandatory on the push route (non-private instances): without a
	// configured token the route refuses requests instead of accepting
	// any authenticated principal.
	RequireFederationCredential bool
	// RedactHealthz hides the verbose /healthz envelope (version, queue
	// depths, upgrade progress) from unauthenticated callers — public
	// instances set it. Bare /health stays open for LB probes either way.
	RedactHealthz bool
}

// NewRouter creates the HTTP router with all routes and middleware.
// devCORS enables CORS for http://localhost:5173 (Vite dev server).
// mgr is optional (nil-safe); nil disables live watch management but keeps CRUD.
//
// The /healthz endpoint is registered with zero-valued HealthzProbes;
// callers (dpkms serve) that want richer signals (version, gRPC probe,
// watcher introspection) should use NewRouterWithConfig instead.
func NewRouter(svc *service.Service, devCORS bool, mgr *watcher.Manager) chi.Router {
	return NewRouterWithConfig(svc, RouterConfig{DevCORS: devCORS, Watcher: mgr})
}

// NewRouterWithProbes keeps the pre-RouterConfig constructor shape for
// callers that only inject healthcheck probes.
func NewRouterWithProbes(svc *service.Service, devCORS bool, mgr *watcher.Manager, probes HealthzProbes) chi.Router {
	return NewRouterWithConfig(svc, RouterConfig{DevCORS: devCORS, Watcher: mgr, Probes: probes})
}

// NewRouterWithConfig is the full constructor used by dpkms serve.
func NewRouterWithConfig(svc *service.Service, rc RouterConfig) chi.Router {
	mgr := rc.Watcher
	probes := rc.Probes

	r := chi.NewRouter()

	r.Use(RequestID)
	r.Use(Recoverer)
	r.Use(CORS(rc.DevCORS))
	if rc.Security != nil {
		r.Use(WithSecurityEvents(rc.Security))
	}

	r.Get("/health", Health(svc))
	if rc.RedactHealthz {
		r.Get("/healthz", RedactedHealthz(svc, probes, rc.Auth))
	} else {
		r.Get("/healthz", Healthz(svc, probes))
	}
	r.Get("/manifest.json", ManifestJSON())

	// GET /ui → redirect to /ui/
	r.Get("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
	})

	// GET /ui/* → serve embedded SPA assets
	distFS, err := fs.Sub(ui.FS, "dist")
	if err != nil {
		panic("ui: failed to sub embedded FS: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(distFS))
	r.Handle("/ui/*", http.StripPrefix("/ui", fileServer))

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/ui/") {
			http.ServeFileFS(w, req, distFS, "index.html")
			return
		}
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	})

	r.Route("/api/v1", func(r chi.Router) {
		// Inbound authentication ahead of the whole route table
		// (REST, SSE, federation push, and the MCP mount below all
		// inherit it). Provider-agnostic by construction: the
		// middleware consumes authn.Provider, never a scheme.
		if rc.Auth != nil {
			r.Use(RequireAuth(rc.Auth, rc.Security))
		}

		// Objects
		r.Get("/objects", ListObjects(svc))
		r.Get("/objects/{id}", GetObject(svc))
		r.Patch("/objects/{id}", UpdateObject(svc))
		r.Delete("/objects/{id}", DeleteObject(svc))

		// Analyze
		r.Post("/analyze", Analyze(svc))

		// Jobs
		r.Get("/jobs", ListJobs(svc))
		r.Get("/jobs/{id}", GetJob(svc))
		r.Post("/jobs/{id}/retry", RetryJob(svc))

		// Search
		r.Get("/search", Search(svc))

		// Entities. The entity-serving surface is where inbound
		// entitlements and metering bite: resolves and pulls are
		// charged per principal when a gate is wired.
		r.Get("/entities", ListEntities(svc, rc.Entitlements))
		r.Get("/entities/{slug}", GetEntity(svc, rc.Entitlements))
		r.Get("/entities/{slug}/backlinks", EntityBacklinks(svc, rc.Entitlements))
		// Thin sync: promote a thin entity to full on demand.
		r.Post("/entities/{slug}/pull", PullEntity(svc, rc.Entitlements))
		// Thin sync: trigger entity index sync for a registry.
		r.Post("/entities/registry-sync", SyncRegistryEntities(svc))

		// Pipelines
		r.Post("/pipelines", CreatePipeline(svc))
		r.Get("/pipelines", ListPipelines(svc))
		r.Get("/pipelines/{name}", GetPipeline(svc))
		r.Delete("/pipelines/{name}", DeletePipeline(svc))
		r.Post("/pipelines/{name}/archive", ArchivePipeline(svc))
		r.Post("/pipelines/{name}/unarchive", UnarchivePipeline(svc))
		r.Post("/pipelines/enqueue", Enqueue(svc))

		// Steps
		r.Get("/steps", ListSteps(svc))
		r.Get("/steps/{name}", GetStep(svc))
		r.Post("/steps/install", InstallStep(svc))
		r.Delete("/steps/{name}", UninstallStep(svc))

		// Registries
		r.Post("/steps/registries/fetch", FetchRegistry(svc))
		r.Post("/steps/registries/{url}/update", UpdateRegistry(svc))
		r.Get("/steps/registries", ListRegistries(svc))

		// Feeds
		r.Post("/feeds", CreateFeed(svc))
		r.Get("/feeds", ListFeeds(svc))
		r.Post("/feeds/sync", SyncAllFeeds(svc))
		r.Post("/feeds/{id}/sync", SyncFeed(svc))
		r.Delete("/feeds/{id}", DeleteFeed(svc))

		// Import
		r.Post("/import", CreateImport(svc))
		r.Get("/import/{id}", GetImport(svc))

		// Importers
		r.Post("/importers/dropbox/run", RunDropboxImport(svc))
		r.Post("/importers/slack/run", RunSlackImport(svc))
		r.Post("/importers/discord/run", RunDiscordImport(svc))
		r.Get("/importers/runs/{id}", GetImporterRun(svc))

		// System
		r.Get("/system/reminders", ListReminders(svc))
		r.Post("/system/reminders/{id}/dismiss", DismissReminder(svc))

		// Watches
		r.Post("/watches", CreateWatch(svc, mgr))
		r.Get("/watches", ListWatches(svc))
		r.Get("/watches/{id}", GetWatch(svc))
		r.Patch("/watches/{id}", UpdateWatch(svc, mgr))
		r.Delete("/watches/{id}", DeleteWatch(svc, mgr))
		r.Post("/watches/{id}/pause", PauseWatch(mgr))
		r.Post("/watches/{id}/resume", ResumeWatch(mgr))
		r.Get("/watches/{id}/files", ListWatchFiles(svc))

		// Inbox
		r.Post("/inbox", CaptureInbox(svc))
		r.Get("/inbox", ListInbox(svc))
		r.Post("/inbox/{id}/triage", TriageInbox(svc))
		r.Post("/inbox/{id}/discard", DiscardInbox(svc))

		// SSE event stream (real-time updates)
		r.Get("/events", HandleSSE(svc))

		// Suggestions (autosuggest plugin)
		r.Get("/suggestions", ListSuggestions(svc))
		r.Post("/suggestions/{id}/approve", ApproveSuggestion(svc))
		r.Post("/suggestions/{id}/reject", RejectSuggestion(svc))

		// Aliases (aliasing plugin)
		r.Post("/aliases", CreateAlias(svc))
		r.Get("/aliases", ListAliases(svc))
		r.Get("/aliases/{alias}", ResolveAlias(svc))
		r.Delete("/aliases/{alias}", DeleteAlias(svc))

		// Capture (browser extension)
		r.Post("/capture/page", CapturePage(svc))
		r.Post("/capture/selection", CaptureSelection(svc))
		r.Post("/capture/element", CaptureElement(svc))
		r.Get("/capture/recent", ListRecentCaptures(svc))

		// Saved searches (US-0054)
		r.Post("/saved-searches", CreateSavedSearch(svc))
		r.Get("/saved-searches", ListSavedSearches(svc))
		r.Get("/saved-searches/{name}", GetSavedSearch(svc))
		r.Delete("/saved-searches/{name}", DeleteSavedSearch(svc))

		// Search history (US-0055)
		r.Get("/search-history", ListSearchHistory(svc))
		r.Delete("/search-history", ClearSearchHistory(svc))

		// Audit log (US-0405)
		r.Get("/audit-log", ListAuditLog(svc))

		// Federation push (Phase 2). On non-private instances the
		// federation credential is mandatory, not just any principal.
		r.Post("/federation/push", FederationPush(svc, rc.RequireFederationCredential))

		// MCP read-surface (per ADR-068).
		// Mounted at /api/v1/mcp/ as a sibling of REST routes. JSON-RPC 2.0
		// over POST per MCP spec 2025-03-26 (streamable-HTTP transport).
		// Handler is a single endpoint; tool dispatch happens inside the
		// handler based on the JSON-RPC method/params.
		r.Handle("/mcp/", mountMCP(svc))
		r.Handle("/mcp", mountMCP(svc))
	})

	return r
}

// mountMCP constructs the MCP server with handlers wired to the supplied
// service.Service. Per ADR-068 §Implementation Notes, the MCP package
// owns the JSON-RPC dispatch + tool registry; this function is the
// dpkms-side wiring (the single place service.Service connects to MCP).
func mountMCP(svc *service.Service) http.Handler {
	server := mcp.New(mcp.ToolContext{
		SearchHandler: func(ctx context.Context, query string, topK int) ([]any, error) {
			objs, _, err := svc.SearchObjects(ctx, query, topK, 0)
			if err != nil {
				return nil, err
			}
			out := make([]any, 0, len(objs))
			for _, o := range objs {
				out = append(out, map[string]any{
					"id":      o.ID,
					"type":    o.Type,
					"subtype": o.Subtype,
				})
			}
			return out, nil
		},
		SchemaHandler: func(_ context.Context) (map[string]any, error) {
			return map[string]any{
				"object_kinds": []string{"text", "url", "image", "audio", "video", "file", "meeting"},
				"edge_types":   []string{"mentions", "supersedes", "references"},
				"pipelines":    []string{"text.short", "text.long", "url.generic", "url.repo", "image.ocr", "audio.transcribe", "video.full"},
			}, nil
		},
	})
	return server.Handler()
}

// Health returns the health check handler.
func Health(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := svc.Store.Health(r.Context()); err != nil {
			WriteError(w, http.StatusServiceUnavailable, "UNHEALTHY", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
