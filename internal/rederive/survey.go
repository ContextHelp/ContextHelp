package rederive

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// survey groups the stale objects with one query and decides each group:
// the cost is one grouped query plus one decision per group, whatever the
// number of stale objects.
func (j *Job) survey(ctx context.Context) ([]*group, error) {
	stale, err := j.Store.ListStaleGroups(ctx, pipeline.CurrentSignatures(j.Registry))
	if err != nil {
		return nil, fmt.Errorf("list stale groups: %w", err)
	}
	groups := make([]*group, 0, len(stale))
	for _, sg := range stale {
		groups = append(groups, j.decide(ctx, sg))
	}
	return groups, nil
}

// deferredOf aggregates the deferred groups by pipeline, with the
// distinct reasons.
func deferredOf(groups []*group) []upgrade.Deferred {
	byPipeline := map[string]*upgrade.Deferred{}
	for _, g := range groups {
		if g.kind != kindDefer {
			continue
		}
		name := family(g.Pipeline)
		if name == "" {
			name = "(none)"
		}
		d := byPipeline[name]
		if d == nil {
			d = &upgrade.Deferred{Pipeline: name}
			byPipeline[name] = d
		}
		d.Objects += g.Objects
		if !strings.Contains(d.Reason, g.reason) {
			if d.Reason != "" {
				d.Reason += " | "
			}
			d.Reason += g.reason
		}
	}
	out := make([]upgrade.Deferred, 0, len(byPipeline))
	for _, d := range byPipeline {
		out = append(out, *d)
	}
	slices.SortFunc(out, func(a, b upgrade.Deferred) int { return strings.Compare(a.Pipeline, b.Pipeline) })
	return out
}
