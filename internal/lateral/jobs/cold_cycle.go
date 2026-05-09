package jobs

import (
	"context"

	"hop.top/kit/go/runtime/domain"
	"hop.top/kit/go/runtime/job"
)

// ScanFunc is one batch of cold-cycle work the handler performs. It must
// call svc.Heartbeat(ctx, jobID) periodically if its work can run longer
// than the configured ClaimTTL. The factory below passes a closure that
// already knows the job ID; this signature only takes ctx so the daemon
// can wire the same scan into other entrypoints (CLI, tests).
type ScanFunc func(ctx context.Context) error

// ColdCycleHandler returns a job handler that runs scan once and, on
// success, publishes ctxt.lateral.reaper.cycle.completed via pub. On scan
// error the handler returns the error so the poller routes it to
// job.Service.Fail (which applies the configured Backoff).
//
// scan is responsible for calling svc.Heartbeat(ctx, jobID) on its own
// cadence when the work can run longer than ClaimTTL — the closure
// capturing svc lives in the daemon wiring (see T15).
func ColdCycleHandler(svc job.Service, pub domain.EventPublisher, scan ScanFunc) func(context.Context, job.Job) error {
	_ = svc // referenced in godoc; daemon-side closures capture it directly
	return func(ctx context.Context, j job.Job) error {
		if err := scan(ctx); err != nil {
			return err
		}
		if pub != nil {
			_ = pub.Publish(
				ctx, "ctxt.lateral.reaper.cycle.completed",
				"lateral.reaper",
				map[string]any{
					"job_id":   j.ID,
					"severity": "info",
				},
			)
		}
		return nil
	}
}
