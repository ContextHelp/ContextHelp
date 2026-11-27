package steps_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	psteps "github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
)

// An installed step runs an arbitrary process and reports no provider
// use: a pipeline holding one is never judged none or local, wrapped by a
// contract override or not, and is not a built-in.
func TestInstalledStepMakesLocalityUnknown(t *testing.T) {
	ext := &steps.ExternalStep{}
	for name, st := range map[string]pipeline.PipelineStep{
		"bare":    ext,
		"wrapped": pipeline.ApplyOverride(ext, pipeline.StepOverride{}),
	} {
		if psteps.IsBuiltin(st) {
			t.Errorf("%s: an installed step is a built-in", name)
		}
		reg := pipeline.NewRegistry()
		if err := reg.Register("p", &pipeline.Pipeline{PipelineName: "p", Steps: []pipeline.PipelineStep{st}}); err != nil {
			t.Fatal(err)
		}
		rep, err := pipeline.ProviderLocality(context.Background(), reg, "p", psteps.IsBuiltin)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Locality != pipeline.LocalityUnknown || rep.Locality.Automatic() {
			t.Errorf("%s: locality %q, want unknown", name, rep.Locality)
		}
		if len(rep.Uses) != 1 || !rep.Uses[0].Opaque {
			t.Errorf("%s: uses %+v, want one opaque step", name, rep.Uses)
		}
	}
}
