package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// handleReproject registers the re-projection job handler (ADR-070
// reindex_auto for a projection change). Progress goes to the daemon's
// upgrade status, so `ctxt upgrade status` and the CLI banner show it.
func handleReproject(pool *jobs.WorkerPool, driver storage.StorageDriver, mgr *upgrade.Manager, bus events.Bus) {
	r, err := jobs.NewReprojector(driver, mgr, bus)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: re-projection unavailable; a projection change will not be applied: %v\n", err)
		return
	}
	pool.Handle(jobs.ReprojectJobType, r.Handle)
}

// scheduleReproject enqueues the re-projection when startup verification
// found the FTS signature stale. It runs after crash recovery, so a run
// interrupted by the previous shutdown is found pending and resumed
// rather than duplicated. The daemon does not wait for it: search serves
// throughout, each object switching to its new body as it is re-projected.
func scheduleReproject(ctx context.Context, w io.Writer, q *jobs.Queue, verify *indexsig.VerifyResult, bus events.Bus) {
	job, err := jobs.ScheduleReprojection(ctx, q, verify, bus)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: schedule re-projection: %v\n", err)
		return
	}
	if job != nil {
		fmt.Fprintf(w, "FTS re-projection to %s: job %s (follow with: ctxt upgrade status)\n",
			jobs.ReprojectTarget(), job.ID)
	}
}

// shortHash abbreviates a signature hash for log lines; a cleared hash
// (a migration invalidated it) prints as "none".
func shortHash(h string) string {
	switch {
	case h == "":
		return "none"
	case len(h) > 12:
		return h[:12]
	default:
		return h
	}
}
