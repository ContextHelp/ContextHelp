package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// probeStep records the scratch dirs earlier steps registered and, when
// fail is set, fails the run.
type probeStep struct {
	pipeline.BaseContract
	fail bool
	dirs *[]string
}

func newProbe(fail bool, dirs *[]string) probeStep {
	return probeStep{pipeline.NewBaseContract(pipeline.StepContract{Binary: true}), fail, dirs}
}

func (probeStep) Name() string { return "probe" }
func (s probeStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if paths, _ := d.Metadata["frame_paths"].([]string); len(paths) > 0 {
		*s.dirs = append(*s.dirs, filepath.Dir(paths[0]))
	}
	if s.fail {
		return nil, errors.New("boom")
	}
	return d, nil
}

// Frames a step extracts go when the run ends, on success and on failure.
func TestRunStepsRemovesScratchDirs(t *testing.T) {
	for name, fail := range map[string]bool{"success": false, "failure": true} {
		t.Run(name, func(t *testing.T) {
			var seen []string // the sampler's frame dir, seen mid-run
			pipe := &pipeline.Pipeline{Steps: []pipeline.PipelineStep{
				steps.NewFrameSampler(steps.WithFrameOptions(providers.FrameOptions{MaxFPS: 0.02})),
				newProbe(fail, &seen),
			}}
			draft := &storage.KnowledgeObject{Source: filepath.Join(t.TempDir(), "v.mp4")}
			out, err := (&WorkerPool{}).runSteps(context.Background(), pipe, draft)
			if (err != nil) != fail {
				t.Fatalf("err = %v", err)
			}
			if len(seen) != 1 {
				t.Fatalf("the probe saw %d frame dirs, want 1", len(seen))
			}
			if _, err := os.Stat(seen[0]); err == nil {
				t.Errorf("scratch dir %s survived the run", seen[0])
			}
			if out != nil {
				if _, ok := out.Metadata["frame_paths"]; ok {
					t.Error("frame_paths outlived the files")
				}
			}
		})
	}
}
