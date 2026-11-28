package rederive

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Schedule is run at start. It decides every group of stale objects (one
// grouped query, one decision per group), publishes the deferred ones to
// the status, and enqueues the job only when some group is runnable (or
// only needs stamping). A job already pending or running (a resumed run)
// is returned instead of a second one. A nil job means nothing to do.
// With the job disabled nothing is written and one line says how many
// objects are stale.
func (j *Job) Schedule(ctx context.Context, q *jobs.Queue) (job *storage.Job, err error) {
	// Start-up must survive a decision that panics (a registry or an entry
	// list the code did not expect): it is logged and reported as an error,
	// never allowed to stop the daemon from starting.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("rederive: scheduling panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			job, err = nil, fmt.Errorf("schedule re-derive: panicked: %v", r)
		}
	}()
	groups, err := j.survey(ctx)
	if err != nil {
		return nil, fmt.Errorf("schedule re-derive: %w", err)
	}
	stale, runnable := 0, 0
	for _, g := range groups {
		stale += g.Objects
		if g.kind != kindDefer {
			runnable += g.Objects
		}
	}
	if !j.Enabled {
		if stale > 0 {
			slog.Info("rederive: disabled by upgrade.rederive.enabled; stale objects stay as stored", "stale", stale)
		}
		return nil, nil
	}
	// Tries, strikes and skips of a target that is no longer produced are
	// dead: a target that comes back later starts clean.
	keep, unbuilt := signaturesToKeep(j.Registry)
	if n, err := j.Store.PruneRederiveRows(ctx, keep, unbuilt); err != nil {
		slog.Warn("rederive: prune dead rows", "err", err)
	} else if n > 0 {
		slog.Info("rederive: pruned rows of signatures no longer current", "rows", n)
	}
	if j.Progress != nil {
		_ = j.Progress.SetDeferred(deferredOf(groups))
	}
	if runnable == 0 {
		return nil, nil
	}
	for _, status := range []storage.JobStatus{storage.JobRunning, storage.JobPending} {
		found, _, err := q.List(ctx, storage.JobFilter{Type: JobType, Status: status, Limit: 1})
		if err != nil {
			return nil, fmt.Errorf("schedule re-derive: list jobs: %w", err)
		}
		if len(found) > 0 {
			return found[0], nil
		}
	}
	job, err = q.EnqueueTask(ctx, JobType, "{}", maxRetries)
	if err != nil {
		return nil, fmt.Errorf("schedule re-derive: %w", err)
	}
	publish(ctx, j.Bus, events.TopicDpkmsUpgradeRederiveScheduled, events.RederivePayload{
		JobID: job.ID, Target: Target(pipeline.CurrentSignatures(j.Registry)), Total: runnable,
	})
	return job, nil
}

// signaturesToKeep are the targets a row may still have: every signature
// the registry can produce (each definition's current and baseline one),
// and the pipelines that cannot be built at this start (a provider is not
// up yet): their current signature is unknown, so what they once produced
// stays. A start where a provider is late must not reset the strikes and
// the skips of its pipelines.
func signaturesToKeep(reg pipeline.Registry) (sigs, unbuilt []string) {
	seen := map[string]bool{}
	for _, rec := range pipeline.SignatureRecords(reg) {
		seen[rec.Sig] = true
	}
	for _, sig := range pipeline.CurrentSignatures(reg) {
		seen[sig] = true
	}
	for sig := range seen {
		sigs = append(sigs, sig)
	}
	registered := map[string]bool{}
	for _, name := range reg.List() {
		registered[name] = true
	}
	for name := range pipeline.Baselines(reg) {
		if !registered[name] {
			// derivation_signatures.pipeline holds the base name.
			if base := pipeline.BaseName(name); !slices.Contains(unbuilt, base) {
				unbuilt = append(unbuilt, base)
			}
		}
	}
	slices.Sort(sigs)
	slices.Sort(unbuilt)
	return sigs, unbuilt
}
