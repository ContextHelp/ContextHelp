package cmd

import (
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/migrate"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// handleEmbeddingsMigrate registers the embedding migration job handler
// (`ctxt embeddings migrate --to <model_id>` enqueues the job). Progress
// goes to the daemon's upgrade status, so `ctxt upgrade status` and the
// CLI banner show it. Without a readable model registry the handler is
// not registered and migration jobs fail with no handler.
func handleEmbeddingsMigrate(pool *jobs.WorkerPool, driver storage.StorageDriver, mgr *upgrade.Manager, bus events.Bus) {
	reg, err := registry.ForDriver(driver)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: embedding model registry unavailable; embedding migrations will not run: %v\n", err)
		return
	}
	runner := &migrate.Runner{
		Models:   reg,
		Objects:  driver.Objects(),
		Store:    driver.Embeddings(),
		Resolver: embeddings.NewProviderResolver(newEmbeddingResolver()),
		Progress: mgr,
		Bus:      bus,
	}
	pool.Handle(migrate.JobType, runner.Handle)
}
