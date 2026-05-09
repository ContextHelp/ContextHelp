package jobs

import (
	"context"

	"hop.top/kit/go/runtime/domain"
	"hop.top/kit/go/runtime/job"
)

// Bus topic + source for the cold-cycle handler's emitted events.
// Naming follows the extended [Source].[Category].[Object][modifier].[Action]
// notation (ctxt.lateral.reaper[cycle].<action>). The schema for this event
// lives in schemas/lateral_events.json under the "reaper[cycle].completed" key.
const (
	TopicReaperCycleCompleted = "ctxt.lateral.reaper[cycle].completed"
	SourceReaperCycle         = "lateral.reaper[cycle]"
)

// CycleStats summarizes one cold-cycle scan for observability.
// Emitted as the payload of TopicReaperCycleCompleted (alongside the
// handler-side job_id + severity envelope fields).
type CycleStats struct {
	Expired    int `json:"expired"`
	DurationMS int `json:"duration_ms"`
}

// ScanFunc is one batch of cold-cycle work the handler performs. It must
// call svc.Heartbeat(ctx, jobID) periodically if its work can run longer
// than the configured ClaimTTL. The factory below passes a closure that
// already knows the job ID; this signature only takes ctx so the daemon
// can wire the same scan into other entrypoints (CLI, tests). It returns
// CycleStats so the handler can attach observability counters to the
// emitted event payload.
type ScanFunc func(ctx context.Context) (CycleStats, error)

// ColdCycleHandler returns a job handler that runs scan once and, on
// success, publishes TopicReaperCycleCompleted via pub. On scan
// error the handler returns the error so the poller routes it to
// job.Service.Fail (which applies the configured Backoff).
//
// scan is responsible for calling svc.Heartbeat(ctx, jobID) on its own
// cadence when the work can run longer than ClaimTTL — the closure
// capturing svc lives in the daemon wiring (see T15).
func ColdCycleHandler(svc job.Service, pub domain.EventPublisher, scan ScanFunc) func(context.Context, job.Job) error {
	_ = svc // referenced in godoc; daemon-side closures capture it directly
	return func(ctx context.Context, j job.Job) error {
		stats, err := scan(ctx)
		if err != nil {
			return err
		}
		if pub != nil {
			_ = pub.Publish(
				ctx, TopicReaperCycleCompleted,
				SourceReaperCycle,
				map[string]any{
					"job_id":      j.ID,
					"severity":    "info",
					"expired":     stats.Expired,
					"duration_ms": stats.DurationMS,
				},
			)
		}
		return nil
	}
}
