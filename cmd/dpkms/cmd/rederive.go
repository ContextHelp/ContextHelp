package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	kitbus "hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/rederive"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// rederiveStore is the system view of the object store the re-derive job
// reads and writes, or false when the driver cannot hold its tables.
func rederiveStore(driver storage.StorageDriver) (rederive.Store, bool) {
	s, ok := storage.System(driver).Objects().(rederive.Store)
	return s, ok
}

// recordDerivationSignatures stores the entries behind every signature
// the registry can produce, so the re-derive job can tell which step or
// model changed. It runs after the baseline stamp, on every start.
func recordDerivationSignatures(ctx context.Context, driver storage.StorageDriver, reg pipeline.Registry) {
	store, ok := rederiveStore(driver)
	if !ok {
		return
	}
	if _, err := rederive.RecordSignatures(ctx, store, reg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: record derivation signatures: %v\n", err)
	}
}

// newRederiveJob builds the job for the daemon: the same one schedules at
// start and handles the task. It reports false when the driver cannot hold
// the job's tables.
func newRederiveJob(driver storage.StorageDriver, svc *service.Service, reg pipeline.Registry, mgr *upgrade.Manager, bus kitbus.Bus, cfg config.RederiveConfig) (*rederive.Job, bool) {
	store, ok := rederiveStore(driver)
	if !ok {
		fmt.Fprintf(os.Stderr, "warning: automatic re-derive unavailable: %T cannot hold its tables\n", driver.Objects())
		return nil, false
	}
	return &rederive.Job{
		Store: store, Svc: svc, Registry: reg, Progress: mgr, Bus: bus,
		Enabled: cfg.Enabled, Pause: cfg.Pause,
		// The service reads and writes every row, whoever owns it.
		Scope: asSystem,
	}, true
}

// handleRederive registers the automatic re-derive job handler. Progress
// goes to the daemon's upgrade status, shared with the re-projection.
func handleRederive(pool *jobs.WorkerPool, j *rederive.Job) {
	pool.Handle(rederive.JobType, j.Handle)
}

// scheduleRederive decides the groups of stale objects and enqueues the
// re-derive job when some group can run. It runs after crash recovery, so
// an interrupted run is found pending and resumed rather than duplicated.
// The daemon does not wait for it.
func scheduleRederive(ctx context.Context, w io.Writer, q *jobs.Queue, j *rederive.Job) {
	job, err := j.Schedule(ctx, q)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: schedule re-derive: %v\n", err)
		return
	}
	if job != nil {
		fmt.Fprintf(w, "Re-derive of stale objects: job %s (follow with: ctxt upgrade status)\n", job.ID)
	}
}
