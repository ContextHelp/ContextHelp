package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// NewRouter creates the HTTP router with all routes and middleware.
func NewRouter(svc *service.Service) chi.Router {
	r := chi.NewRouter()

	r.Use(RequestID)
	r.Use(Recoverer)

	r.Get("/health", Health(svc))

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	})

	r.Route("/api/v1", func(r chi.Router) {
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

		// Entities
		r.Get("/entities", ListEntities(svc))
		r.Get("/entities/{slug}", GetEntity(svc))
		r.Get("/entities/{slug}/backlinks", EntityBacklinks(svc))
	})

	return r
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
