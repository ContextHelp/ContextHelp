package rederive

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// groupKind is what a run does with the objects of one (pipeline, stored
// signature) group.
type groupKind int

const (
	// kindRun: re-derive each object.
	kindRun groupKind = iota
	// kindStamp: the change applies nothing (the pipeline only lost
	// steps): set the signature of the rows, run nothing.
	kindStamp
	// kindDefer: leave the objects alone and unrecorded; a later start
	// with another configuration, registry or recorded entries takes them.
	kindDefer
)

// group is one decided group of stale objects.
type group struct {
	storage.StaleGroup
	kind groupKind
	// target is the signature of the pipeline the plan ran, which the
	// objects are stamped with.
	target string
	// network: the change can only be applied by fetching again.
	network bool
	// fetchStep is the network step of the plan, which chooses the refresh
	// guard's rule.
	fetchStep string
	// reason says why a group is deferred.
	reason string
}

func (g *group) deferred(reason string) *group {
	g.kind, g.reason = kindDefer, reason
	return g
}

// family is the pipeline name an object's stored stamp ("text.short@v1")
// belongs to.
func family(stamp string) string {
	if name, _, ok := pipeline.ParseVersionedName(stamp); ok && name != "" {
		return name
	}
	return stamp
}

// decide plans one group, once for all its objects. In order:
//
//   - the pipeline must be registered and the stored signature must have
//     recorded entries, else the group is deferred (it becomes eligible
//     by itself when either changes);
//   - a diff that only removed steps applies nothing: stamp;
//   - every added or re-versioned step must run in the chosen mode:
//     stored content when the plan lists them all, else the network mode;
//     a step neither lists (pruned, left out) defers the group;
//   - the chosen plan must stay on this machine, else the group is deferred.
func (j *Job) decide(ctx context.Context, sg storage.StaleGroup) *group {
	g := &group{StaleGroup: sg}
	fam := family(sg.Pipeline)
	if fam == "" {
		return g.deferred("the objects name no pipeline")
	}
	stored, errStored := j.Svc.ReanalysisLocality(ctx, fam, service.ReanalyzeOptions{})
	if errStored != nil && !errors.Is(errStored, upgrade.ErrCannotReanalyze) {
		return g.deferred(fmt.Sprintf("pipeline %q is not registered", fam))
	}
	var net service.ReanalysisPlan
	var errNet error
	netTried := false
	network := func() {
		if !netTried {
			net, errNet = j.Svc.ReanalysisLocality(ctx, fam, service.ReanalyzeOptions{AllowNetwork: true})
			netTried = true
		}
	}
	ran := stored
	if errStored != nil {
		network()
		if errNet != nil {
			return g.deferred("cannot be re-derived: " + errNet.Error())
		}
		ran = net
	}
	// The signature of the pipeline the plan runs, not of another
	// registration of the same family.
	rec, ok := pipeline.CurrentRecord(j.Registry, ran.Pipeline)
	if !ok {
		if rec, ok = pipeline.CurrentRecord(j.Registry, fam); !ok {
			return g.deferred(fmt.Sprintf("pipeline %q is not registered", fam))
		}
	}
	g.target = rec.Sig
	diff, why := j.diffOf(ctx, sg.Sig, rec.Entries)
	if why != "" {
		return g.deferred(why)
	}
	if !diff.Applies() {
		g.kind = kindStamp
		return g
	}
	required := j.runNames(rec.Pipeline, diff.Required())
	use := stored
	if errStored != nil || (stored.Locality != pipeline.LocalityUnavailable && !subset(required, stored.Steps)) {
		network()
		if errNet != nil {
			return g.deferred("cannot be re-derived: " + errNet.Error())
		}
		use, g.network, g.fetchStep = net, true, networkStepOf(net.Steps)
		if missing := missingStep(required, net.Steps); missing != "" && net.Locality != pipeline.LocalityUnavailable {
			return g.deferred(fmt.Sprintf("step %s is not run by the pipeline in any mode (left out or pruned)", missing))
		}
	}
	if !use.Locality.Automatic() {
		return g.deferred(deferReason(use))
	}
	return g
}

// diffOf compares the entries recorded for the stored signature with the
// current ones. When the stored signature has none, or they cannot be read,
// it says why the group cannot be judged.
func (j *Job) diffOf(ctx context.Context, storedSig string, current []string) (pipeline.EntryDiff, string) {
	old, ok, err := j.Store.DerivationSignatureEntries(ctx, storedSig)
	switch {
	case err != nil:
		return pipeline.EntryDiff{}, "read recorded signature: " + err.Error()
	case !ok:
		return pipeline.EntryDiff{}, "the stored signature has no recorded entries"
	}
	return pipeline.DiffEntries(old, current), ""
}

// networkStepOf is the first step of a plan that fetches, which chooses the
// refresh guard's rule; "" when none does.
func networkStepOf(steps []string) string {
	for _, st := range steps {
		if service.IsNetworkStep(st) {
			return st
		}
	}
	return ""
}

// runNames maps the entry names of changed steps to the names the built
// steps report (a definition names "textcleaner", the step is
// "text_cleaner"), which is what a plan lists.
func (j *Job) runNames(name string, names []string) []string {
	p, err := j.Registry.Get(name)
	if err != nil || len(p.StepAliases) == 0 {
		return names
	}
	out := make([]string, len(names))
	for i, n := range names {
		if a, ok := p.StepAliases[n]; ok {
			n = a
		}
		out[i] = n
	}
	return out
}

func subset(names, of []string) bool { return missingStep(names, of) == "" }

// missingStep is the first of names not in of.
func missingStep(names, of []string) string {
	for _, n := range names {
		if !slices.Contains(of, n) {
			return n
		}
	}
	return ""
}

// deferReason says why a plan may not run unasked: the plan's own reason
// for an unavailable pipeline, else each provider use that is not known to
// be local. Endpoints are left out: they can hold credentials.
func deferReason(plan service.ReanalysisPlan) string {
	var uses []string
	for _, u := range plan.Uses {
		if u.Resource == "network" {
			continue
		}
		what := u.Step + " (" + u.Resource
		if u.Backend != "" {
			what += ", " + u.Backend
		}
		uses = append(uses, what+")")
	}
	reason := "work would not stay on this machine: " + string(plan.Locality)
	if plan.Reason != "" {
		reason = string(plan.Locality) + ": " + plan.Reason
	}
	if len(uses) > 0 {
		reason += "; " + strings.Join(uses, ", ")
	}
	return reason
}
