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
