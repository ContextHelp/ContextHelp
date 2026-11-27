package service

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	psteps "github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
)

// ReanalysisPlan is what a reanalysis through a pipeline would run, and
// where its provider calls would go.
type ReanalysisPlan struct {
	pipeline.LocalityReport
	// Pipeline is the versioned name that would be stamped.
	Pipeline string
	// Steps are the names of the steps that would run, in order.
	Steps []string
}

// ReanalysisLocality classifies the pipeline ReanalyzeObjectWith would run
// for the pipeline family name under opts: the same steps (the network
// ones included only with opts.AllowNetwork), judged by the providers they
// hold, the steps that cannot be judged and the steps that fetch. A
// pipeline left out of the registry for a missing provider is unavailable
// with its reason; a pipeline that cannot be re-derived from stored
// content (ErrCannotReanalyze) or is not registered is an error, and so is
// never automatic. It does not look at any object: a step that reads the
// object's source file is refused per object by ReanalyzeObject.
func (s *Service) ReanalysisLocality(ctx context.Context, name string, opts ReanalyzeOptions) (ReanalysisPlan, error) {
	family := name
	if parsed, _, ok := pipeline.ParseVersionedName(name); ok && parsed != "" {
		family = parsed
	}
	pipe, versioned, err := s.runnablePipeline(fmt.Sprintf("pipeline %q", name), family, opts.AllowNetwork)
	if err != nil {
		if rep, ok := pipeline.UnavailableReport(s.Pipes, family); ok {
			return ReanalysisPlan{LocalityReport: rep, Pipeline: name}, nil
		}
		return ReanalysisPlan{}, err
	}
	plan := ReanalysisPlan{
		LocalityReport: pipeline.ClassifyPipeline(ctx, pipe, psteps.IsBuiltin),
		Pipeline:       versioned,
	}
	for _, st := range pipe.Steps {
		plan.Steps = append(plan.Steps, st.Name())
	}
	return plan, nil
}
