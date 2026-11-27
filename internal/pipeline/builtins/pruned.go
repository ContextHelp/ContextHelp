package builtins

import "github.com/ideacrafterslabs/ctxt/internal/pipeline"

// prunedStep describes step, left out of its pipeline for an unmet
// capability, for the registry to record.
func prunedStep(step pipeline.PipelineStep) pipeline.PrunedStep {
	return pipeline.PrunedStep{Name: step.Name(), Resources: step.Contract().Resources}
}
