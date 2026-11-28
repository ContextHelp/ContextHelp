package rederive

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// failure is an object that failed in this run in a way that says nothing
// certain about it.
type failure struct {
	target string
	out    outcome
	cause  error
}

// failedTry counts a failure that says nothing certain about the object
// (a provider that is down, a rate limit, a timeout, an error the job does
// not know). It is a try: the object goes behind the ones tried less, and
// stays stale. failureStreak of them in a row stop the group (the run goes
// on with the next one); the tries stand, so the next start goes on with
// the objects tried less.
// Whether it also is a strike towards a skip waits for the end of the run.
func (x *run) failedTry(ctx context.Context, id, target string, out outcome, cause error) error {
	if err := x.Store.RecordRederiveTry(context.WithoutCancel(ctx), id, target); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			x.res.Gone++ // deleted since it was listed
			return nil
		}
		return fmt.Errorf("record try %s: %w", id, err)
	}
	x.failures[id] = failure{target: target, out: out, cause: cause}
	x.res.Transient++
	// An unusable reply joins the streak only while the run has not shown
	// the provider working (with the same evidence a strike takes, see
	// proven): a model answering nothing to everyone must stop the group,
	// one bad object among healthy ones must not.
	providerShownWorking := out.unusable && x.proven(out)
	if !providerShownWorking {
		x.streak++
	}
	slog.Info("rederive: try failed; object left stale", "object", id, "reason", out.reason, "err", detail(cause.Error()))
	if x.streak >= failureStreak {
		// What failed and the last cause, so the status and the log say why
		// the group stopped.
		return &groupStop{kind: out.kind, cause: detail(cause.Error()), why: out.outage}
	}
	return nil
}

// groupStop ends a group: failureStreak tries in a row. The run goes on
// with the next group.
type groupStop struct {
	kind  string
	cause string
	why   error
}

func (e *groupStop) Error() string { return fmt.Sprintf("%v (%s): %s", e.why, e.kind, e.cause) }
func (e *groupStop) Unwrap() error { return e.why }

// stopGroup records a stopped group: its remaining rows stay stale and the
// next start takes them, fewest tries first.
func (x *run) stopGroup(g *group, stop *groupStop) {
	name := family(g.Pipeline)
	// One entry per pipeline: the first group's kind and cause, and how many
	// groups of it stopped when more than one (two stored stamps).
	if i := slices.IndexFunc(x.res.Stopped, func(st upgrade.Stopped) bool { return st.Pipeline == name }); i >= 0 {
		x.res.Stopped[i].Groups = max(x.res.Stopped[i].Groups, 1) + 1
	} else {
		x.res.Stopped = append(x.res.Stopped, upgrade.Stopped{Pipeline: name, Kind: stop.kind, Cause: stop.cause})
	}
	slog.Warn("rederive: group stopped after consecutive failures; the rest waits for a later start",
		"pipeline", name, "kind", stop.kind, "cause", stop.cause)
}

// proven reports whether the run showed that a failure is the object's: its
// own evidence (the outcome's proof), and, in a group that fetches, an
// accepted refresh as well. This is the one place that says so, for every
// kind of outcome: a provider failure or an unusable reply on a page a
// captive portal served is not the page's, however the model fares on
// stored notes.
func (x *run) proven(out outcome) bool {
	if !x.proved[out.proof] {
		return false
	}
	return !out.network || x.proved[proofFetched]
}

// settleStrikes runs when the run ends, however it ends. A failure is a
// strike only if the run proved what it blames was working (see proven):
// a provider failure when an object was re-derived through a step that
// called that provider kind; a fetch failure when a fetch to the same host
// was accepted (for a source that is gone, or a refresh the guard rejected,
// when any fetch was); any other failure when any object was re-derived;
// and, in a group that fetches, always an accepted refresh as well. Without
// that proof (the provider or the network was down for the whole run, or
// nothing else ran) it stays a try. The failure after maxStrikes strikes
// becomes a durable skip, and the object's counters are forgotten. Deciding
// at the end lets a success late in the run count for the failures before
// it.
func (x *run) settleStrikes(ctx context.Context) {
	done := context.WithoutCancel(ctx)
	for id, f := range x.failures {
		if !x.proven(f.out) {
			continue
		}
		n, err := x.Store.RecordRederiveStrike(done, id, f.target)
		if errors.Is(err, storage.ErrNotFound) {
			x.res.Transient--
			x.res.Gone++
			continue
		}
		if err != nil {
			slog.Warn("rederive: strike not recorded", "object", id, "err", err)
			continue
		}
		if n <= maxStrikes {
			continue
		}
		x.res.Transient-- // it was counted as a try; it is a skip now
		text := fmt.Sprintf("after %d strikes: %s", maxStrikes, f.cause.Error())
		_ = x.recordSkip(done, id, f.target, f.out.reason, text)
	}
}

func (x *run) recordSkip(ctx context.Context, id, target, reason, text string) error {
	return x.storeSkip(ctx, storage.RederiveSkip{
		ObjectID: id, TargetSig: target, Reason: reason, Detail: detail(text),
	})
}

func (x *run) storeSkip(ctx context.Context, k storage.RederiveSkip) error {
	k.At = time.Now()
	if err := x.Store.RecordRederiveSkip(context.WithoutCancel(ctx), k); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			x.res.Gone++ // deleted since it was listed: nothing to skip
			return nil
		}
		slog.Warn("rederive: skip not recorded", "object", k.ObjectID, "reason", k.Reason, "err", err)
	}
	if x.res.Skipped == nil {
		x.res.Skipped = map[string]int{}
	}
	_ = x.Store.ClearRederiveAttempts(context.WithoutCancel(ctx), k.ObjectID, k.TargetSig)
	x.res.Skipped[k.Reason]++
	if slices.Contains(failureReasons, k.Reason) {
		x.res.Failed++
	}
	slog.Info("rederive: object left as stored", "object", k.ObjectID, "reason", k.Reason)
	return nil
}

// finish ends a run that handled every object: summary, status, event.
func (x *run) finish(ctx context.Context) {
	x.settleStrikes(ctx)
	if x.Progress != nil && x.res.Total > 0 {
		_ = x.Progress.CompleteRun(x.summary())
		_ = x.Progress.SetSkipped(x.res.Skipped)
		_ = x.Progress.SetStopped(x.res.Stopped)
	}
	x.owns = false
	x.log("rederive: run complete")
	x.publish(ctx, events.TopicDpkmsUpgradeRederiveCompleted, "")
}

func (x *run) summary() upgrade.RunSummary {
	return upgrade.RunSummary{
		Processed: x.res.Processed + x.res.Stamped,
		Failed:    x.res.Failed,
		Skipped:   x.res.SkippedTotal() - x.res.Failed,
	}
}

// stop ends a run early. A shutdown is not a failure: the status goes back
// to idle with what was reached, as nothing is wrong and the next start
// resumes. A run that broke (storage unusable, say) is reported failed, with
// the cause; a stopped group is not that, see stopGroup.
func (x *run) stop(ctx context.Context, cause error) (Result, error) {
	x.res.Interrupted = true
	x.settleStrikes(ctx)
	defer func() { x.owns = false }()
	if x.Progress != nil {
		if ctx.Err() != nil {
			_ = x.Progress.CompleteRun(x.summary())
		} else {
			_ = x.Progress.Fail(fmt.Errorf("re-derive to %s stopped after %d of %d objects: %w",
				x.res.Target, x.res.Done(), x.res.Total, cause))
		}
		_ = x.Progress.SetSkipped(x.res.Skipped)
		_ = x.Progress.SetStopped(x.res.Stopped)
	}
	x.log("rederive: run interrupted", "cause", cause.Error())
	x.publish(context.WithoutCancel(ctx), events.TopicDpkmsUpgradeRederiveInterrupted, cause.Error())
	return x.res, cause
}

func (x *run) log(msg string, extra ...any) {
	skipped := make([]string, 0, len(x.res.Skipped))
	for r, n := range x.res.Skipped {
		skipped = append(skipped, fmt.Sprintf("%s=%d", r, n))
	}
	slices.Sort(skipped)
	deferred := make([]string, 0, len(x.res.Deferred))
	for _, d := range x.res.Deferred {
		deferred = append(deferred, fmt.Sprintf("%s=%d", d.Pipeline, d.Objects))
	}
	stopped := make([]string, 0, len(x.res.Stopped))
	for _, st := range x.res.Stopped {
		stopped = append(stopped, fmt.Sprintf("%s=%s", st.Pipeline, st.Kind))
	}
	args := append([]any{
		"target", x.res.Target, "total", x.res.Total, "processed", x.res.Processed,
		"stamped", x.res.Stamped, "failed", x.res.Failed, "retried", x.res.Transient, "gone", x.res.Gone, "skipped", strings.Join(skipped, ","), "deferred", strings.Join(deferred, ","), "stopped", strings.Join(stopped, ","),
		"duration", time.Since(x.start).Round(time.Millisecond).String(),
	}, extra...)
	slog.Info(msg, args...)
}

func (x *run) publish(ctx context.Context, topic bus.Topic, reason string) {
	p := events.RederivePayload{
		JobID: x.jobID, Target: x.res.Target, Total: x.res.Total,
		Processed: x.res.Processed, Stamped: x.res.Stamped, Failed: x.res.Failed, Transient: x.res.Transient, Gone: x.res.Gone, Skipped: x.res.Skipped,
		DurationMs: time.Since(x.start).Milliseconds(), Reason: reason,
	}
	for _, d := range x.res.Deferred {
		p.Deferred = append(p.Deferred, events.RederiveDeferred{Pipeline: d.Pipeline, Objects: d.Objects, Reason: d.Reason})
	}
	for _, st := range x.res.Stopped {
		p.Stopped = append(p.Stopped, events.RederiveStopped{Pipeline: st.Pipeline, Kind: st.Kind, Cause: st.Cause, Groups: st.Groups})
	}
	publish(ctx, x.Bus, topic, p)
}

func publish(ctx context.Context, b bus.Bus, topic bus.Topic, p events.RederivePayload) {
	if b == nil {
		return
	}
	if err := b.Publish(ctx, bus.NewEvent(topic, eventSource, p)); err != nil {
		slog.Warn("rederive: publish event", "topic", topic, "err", err)
	}
}

// urlText matches a URL inside error text; ] is part of an IPv6 host.
var urlText = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]*://[^\s"'<>)]+`)

// secretText matches what error text may carry as a credential: a bearer
// token, or a name that holds a secret followed by its value.
var (
	bearerText = regexp.MustCompile(`(?i)(bearer\s+)\S+`)
	secretText = regexp.MustCompile(`(?i)\b((?:access[_-]?|api[_-]?|auth[_-]?)?(?:token|key|secret|password|passwd|authorization))(\s*[=:]\s*)\S+`)
)

// redactURL keeps the scheme and the host of a URL. Credentials, path,
// query and fragment can all carry secrets (session ids, webhook paths,
// tokens), so none is kept; what cannot be parsed goes too.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<url>"
	}
	return u.Scheme + "://" + u.Host
}

// detail is error text fit to keep with a skip: secrets and credentials
// removed, every URL reduced to scheme and host, one line, at most
// maxDetail bytes.
func detail(s string) string {
	s = urlText.ReplaceAllStringFunc(s, redactURL)
	s = bearerText.ReplaceAllString(s, "${1}<redacted>")
	s = secretText.ReplaceAllString(s, "${1}${2}<redacted>")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxDetail {
		s = s[:maxDetail]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

// interruptCause is the error a run stopped by its context returns: the
// context's cause when there is one (shutdown, cancel, lost lease).
func interruptCause(ctx context.Context) error {
	if c := context.Cause(ctx); c != nil && !errors.Is(c, ctx.Err()) {
		return fmt.Errorf("%w: %w", ctx.Err(), c)
	}
	return ctx.Err()
}
