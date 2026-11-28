package rederive

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

func (x *run) batch() int {
	if x.Batch > 0 {
		return x.Batch
	}
	return defaultBatch
}

// begin claims the upgrade status, waiting while another run holds it.
// The queue has no delayed requeue, and an error return would burn the
// job's retries at once, so the wait is a poll in the worker; the status is
// held for a re-projection's length, not for ever.
func (x *run) begin(ctx context.Context) error {
	if x.Progress == nil {
		return nil
	}
	poll := x.BusyPoll
	if poll <= 0 {
		poll = defaultBusyPoll
	}
	for {
		err := x.Progress.StartTarget(upgrade.BucketReingestSelective, x.res.Target, x.res.Total)
		if err == nil {
			x.owns = true
			// The previous run's stopped groups are not this run's: they are
			// not shown while later groups run.
			_ = x.Progress.SetStopped(nil)
			return nil
		}
		if !errors.Is(err, upgrade.ErrBusy) {
			return fmt.Errorf("upgrade status: %w", err)
		}
		slog.Info("rederive: waiting for another upgrade run to finish", "err", err)
		if err := x.wait(ctx, poll); err != nil {
			return interruptCause(ctx)
		}
	}
}

func (x *run) wait(ctx context.Context, d time.Duration) error {
	if x.Wait != nil {
		if err := x.Wait(ctx, d); err != nil {
			return err
		}
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// process handles the groups in order. It returns an error only when the
// run must stop early.
func (x *run) process(ctx context.Context) error {
	for _, g := range x.groups {
		var err error
		switch g.kind {
		case kindStamp:
			err = x.stampGroup(ctx, g)
		case kindRun:
			err = x.processGroup(ctx, g)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// stampGroup sets the signature of a group that needs no change.
func (x *run) stampGroup(ctx context.Context, g *group) error {
	n, err := x.Store.StampGroup(context.WithoutCancel(ctx), g.Pipeline, g.Sig, g.target)
	if err != nil {
		return fmt.Errorf("stamp group %s: %w", g.Pipeline, err)
	}
	x.res.Stamped += n
	slog.Info("rederive: pipeline lost steps only; objects stamped current without running", "pipeline", g.Pipeline, "objects", n)
	return x.tick()
}

// processGroup pages a group's rows, fewest failed attempts first, so
// objects never tried come before the ones that failed before.
func (x *run) processGroup(ctx context.Context, g *group) error {
	// Five tries in a row stop this group, not the run: a dead domain or a
	// model that answers nothing must not starve the groups after it.
	x.streak = 0
	cursor := storage.RederiveCursor{}
	for {
		page, err := x.Store.ListRederivable(ctx, storage.RederiveQuery{
			Pipeline: g.Pipeline, Sig: g.Sig, Target: g.target, After: cursor, Limit: x.batch()})
		if err != nil {
			if ctx.Err() != nil {
				return interruptCause(ctx)
			}
			return fmt.Errorf("list stale objects: %w", err)
		}
		if len(page) == 0 {
			return nil
		}
		for _, o := range page {
			cursor = storage.RederiveCursor{Tries: o.Tries, ID: o.ID}
			if _, failed := x.failures[o.ID]; failed {
				continue
			}
			if err := ctx.Err(); err != nil {
				return interruptCause(ctx)
			}
			if err := x.one(ctx, o, g); err != nil {
				var stop *groupStop
				if errors.As(err, &stop) {
					x.stopGroup(g, stop)
					return nil
				}
				return err
			}
			if err := x.tick(); err != nil {
				return err
			}
		}
	}
}

// tick reports progress; done never exceeds total, which was counted
// before rows that arrived since.
func (x *run) tick() error {
	if x.Progress == nil {
		return nil
	}
	done := min(x.res.Done(), x.res.Total)
	if err := x.Progress.TickFailed(done, min(x.res.Failed+x.res.Transient, done)); err != nil {
		return fmt.Errorf("upgrade status: %w", err)
	}
	return nil
}

// one re-derives a single object and settles its outcome.
func (x *run) one(ctx context.Context, o storage.StaleObject, g *group) error {
	// A pause between two re-derives.
	if x.started && x.Pause > 0 {
		if err := x.wait(ctx, x.Pause); err != nil {
			return interruptCause(ctx)
		}
	}
	x.started = true
	opts := service.ReanalyzeOptions{AllowNetwork: g.network}
	if g.network {
		opts.Accept = RefreshGuard(g.fetchStep)
		opts.Before = RefreshPrecheck(g.fetchStep)
	}
	var calls map[string]int
	var fetched []string
	opts.Calls = func(c map[string]int) { calls = c }
	opts.Fetched = func(h []string) { fetched = h }
	_, _, err := x.Svc.ReanalyzeObjectWith(ctx, o.ID, opts)
	if err == nil {
		// A shutdown that arrives now must not undo a finished re-derive:
		// the stamp is one short write.
		done := context.WithoutCancel(ctx)
		if err := x.Store.StampDerivation(done, o.ID, g.target); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				x.res.Gone++ // deleted since the re-derive
				return nil
			}
			return fmt.Errorf("stamp %s: %w", o.ID, err)
		}
		_ = x.Store.ClearRederiveAttempts(done, o.ID, g.target)
		x.res.Processed++
		x.streak = 0
		x.proved["any"] = true
		// A source answered and the guard accepted what it served: the
		// network reaches sources, and this host in particular.
		for _, h := range fetched {
			x.proved[proofFetched] = true
			x.proved["host:"+h] = true
		}
		// A provider is shown working only by a call that was made and
		// whose reply the object could use: not by a plan that lists it.
		for kind, n := range calls {
			if n > 0 {
				x.proved["provider:"+kind] = true
			}
		}
		return nil
	}
	if ctx.Err() != nil {
		// Cut off, not failed: the object is still stale.
		return interruptCause(ctx)
	}
	if errors.Is(err, upgrade.ErrObjectGone) {
		// Deleted since it was listed: dropped, no try, no strike, no skip.
		// Only the object lookup says so; any other not-found (a hidden
		// blob, say) is classified like any other error.
		x.res.Gone++
		return nil
	}
	out := classify(err)
	// In a group that fetches, the model and every step after the fetch ran
	// on what the network returned: no try can strike without an accepted
	// refresh in the run, besides its own evidence (see run.proven).
	out.network = g.network
	if out.permanent {
		// A local determination (the object or its pipeline cannot be
		// re-derived, or its stored text cannot be checked against a
		// refresh), not a network outcome: recorded at once.
		text := err.Error()
		if out.detail != "" {
			text = out.detail
		}
		return x.recordSkip(ctx, o.ID, g.target, out.reason, text)
	}
	return x.failedTry(ctx, o.ID, g.target, out, err)
}

// proofFetched is shown by any successful fetch: a source answered and its
// content was accepted. It is the proof for a source that is gone (404, 410)
// and for a refresh the guard rejected.
const proofFetched = "fetched"

// proofNever is a proof no run ever has: a failure that is never about an
// object, so it can only be a try.
const proofNever = "never"

// outcome is what a failed re-derive means.
type outcome struct {
	// permanent: decided locally (the object or its pipeline cannot be
	// re-derived, or its stored text cannot be checked against a refresh): a
	// skip at once. Nothing that comes from the network is. detail, when
	// set, is the skip's text instead of the error's.
	permanent bool
	detail    string
	reason    string
	// proof names what must have worked in the run for the failure to be
	// the object's (see settleStrikes); outage is what five in a row end
	// the run with.
	proof  string
	outage error
	// network: the group fetches (set by the caller, not by classify).
	network bool
	// kind names what failed (llm, vision, embedding, fetch, unknown), for
	// the outage error the status and the log show.
	kind string
	// unusable: the provider answered and the reply was no use. Once the
	// run has shown the provider working (proof), it is no part of the
	// outage streak: the provider is up and the reply is this object's.
	unusable bool
}

// classify says what a failed re-derive means. Only what is decided locally
// is a fact about the object at once: a refusal, and a stored text too short
// to be checked against a refresh. Everything else is a try, with the proof
// it needs to become a strike: a rejected refresh or a failed fetch (see
// fetchOutcome), a provider failure or an unusable reply (a call of that
// provider kind succeeded), anything else (some object succeeded).
func classify(err error) outcome {
	var stepErr *pipeline.StepError
	var callErr *providers.CallError
	switch {
	case errors.Is(err, errTooShortToCompare):
		// Known before any request, about the stored object alone.
		return outcome{permanent: true, reason: storage.SkipRefreshRejected, detail: errTooShortToCompare.Error()}
	case errors.Is(err, upgrade.ErrRefreshRejected):
		// The source answered with something else than the stored page: a
		// login wall, a portal, or a page that changed. A strike once some
		// other refresh was fetched and accepted.
		return outcome{reason: storage.SkipRefreshRejected, proof: proofFetched, outage: ErrFetchOutage, kind: "refresh rejected"}
	case errors.Is(err, upgrade.ErrCannotReanalyze):
		return outcome{permanent: true, reason: storage.SkipRefused}
	case errors.Is(err, upgrade.ErrProviderFailed):
		var unusable *providers.UnusableReplyError
		if !errors.As(err, &callErr) && errors.As(err, &unusable) {
			// A reply the provider gave but no step could use. It is a try, and
			// a strike under the same proof as a failure of that provider: an
			// object re-derived through a step that called it.
			kind := unusable.Provider
			if kind == "" {
				kind = "llm"
			}
			return outcome{reason: storage.SkipUnusableReply, unusable: true, proof: "provider:" + kind,
				outage: ErrProviderOutage, kind: kind + " unusable reply"}
		}
		kind := "provider"
		if errors.As(err, &callErr) {
			if f := strings.Fields(callErr.Provider); len(f) > 0 {
				kind = f[0]
			}
		}
		out := outcome{reason: storage.SkipProviderFailed, proof: "provider:" + kind, outage: ErrProviderOutage, kind: kind}
		if callErr != nil && strings.HasPrefix(callErr.Provider, "embedding registry") {
			// Not about any object: nothing can show it is the object's.
			out.proof, out.kind = proofNever, "embedding registry"
		}
		return out
	case errors.As(err, &stepErr) && service.IsNetworkStep(stepErr.Step):
		return fetchOutcome(err)
	}
	return outcome{reason: storage.SkipFailed, proof: "any", outage: ErrUnclassifiedOutage, kind: "unknown"}
}

// fetchOutcome says what a failed fetch is evidence of. A source that is
// gone (404, 410) is the object's only if some other fetch was accepted in
// the run; anything else (another status, DNS, TLS, a refused or timed-out
// connection, a blocked destination) only if a fetch to the same host was.
func fetchOutcome(err error) outcome {
	out := outcome{reason: storage.SkipFetchFailed, proof: proofFetched, outage: ErrFetchOutage, kind: "fetch"}
	var fe *pipeline.FetchError
	if !errors.As(err, &fe) || fe.Host == "" {
		return out
	}
	switch {
	case fe.RateLimited || fe.Status == http.StatusTooManyRequests:
		// A rate limit is about the host's patience, never the object: a try,
		// whatever else succeeded on that host.
		out.proof = proofNever
	case fe.Status != http.StatusNotFound && fe.Status != http.StatusGone:
		out.proof = "host:" + fe.Host
	}
	return out
}
