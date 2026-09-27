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
		// (REST, SSE, federation push, and the MCP mount all inherit
		// it). Provider-agnostic by construction: the middleware
		// consumes authn.Provider, never a scheme.
		if rc.Auth != nil {
			r.Use(RequireAuth(rc.Auth, rc.Security))
		}
		mountAPIRoutes(r, apiRoutes(svc, rc), rc)
	})

	return r
}

// mountAPIRoutes mounts the route-to-scope table. With an auth
// provider, every route except the federation push (its own credential
// rule) sits behind RequireScope. A private instance (no provider)
// grants every scope, so its routes are mounted bare.
func mountAPIRoutes(r chi.Router, routes []apiRoute, rc RouterConfig) {
	for _, rt := range routes {
		h := rt.Handler
		if rc.Auth != nil {
			h = RequireScope(rt.Scope, rc.Security)(h)
		}
		if rt.Method == "" {
			r.Handle(rt.Pattern, h)
			continue
		}
		r.Method(rt.Method, rt.Pattern, h)
	}
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
