package pipeline_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type sigStep struct {
	pipeline.BaseContract
	name string
}

func (s *sigStep) Name() string { return s.name }
func (s *sigStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return d, nil
}

func step(name string, version int) pipeline.PipelineStep {
	return &sigStep{BaseContract: pipeline.NewBaseContract(pipeline.StepContract{Version: version}), name: name}
}

func TestSignature_Shape(t *testing.T) {
	got := pipeline.Signature("text", []pipeline.PipelineStep{step("a", 0), step("b", 1)})
	if !regexp.MustCompile(`^text#[0-9a-f]{16}$`).MatchString(got) {
		t.Fatalf("signature %q, want text#<16 hex>", got)
	}
}

func TestSignature_StableAcrossCalls(t *testing.T) {
	steps := []pipeline.PipelineStep{step("a", 1), step("b", 1)}
	if a, b := pipeline.Signature("p", steps), pipeline.Signature("p", steps); a != b {
		t.Fatalf("signature changed between calls: %q vs %q", a, b)
	}
}

func TestSignature_ZeroVersionReadsAsOne(t *testing.T) {
	a := pipeline.Signature("p", []pipeline.PipelineStep{step("a", 0)})
	b := pipeline.Signature("p", []pipeline.PipelineStep{step("a", 1)})
	if a != b {
		t.Fatalf("version 0 and 1 differ: %q vs %q", a, b)
	}
}

func TestSignature_ChangesWithStepVersion(t *testing.T) {
	v1 := pipeline.Signature("p", []pipeline.PipelineStep{step("a", 1), step("b", 1)})
	v2 := pipeline.Signature("p", []pipeline.PipelineStep{step("a", 1), step("b", 2)})
	if v1 == v2 {
		t.Fatalf("a step version bump kept the signature %q", v1)
	}
}

func TestSignature_ChangesWithStepSet(t *testing.T) {
	two := pipeline.Signature("p", []pipeline.PipelineStep{step("a", 1), step("b", 1)})
	one := pipeline.Signature("p", []pipeline.PipelineStep{step("a", 1)})
	if one == two {
		t.Fatal("dropping a step kept the signature")
	}
}

func TestSignature_OrderSensitive(t *testing.T) {
	ab := pipeline.Signature("p", []pipeline.PipelineStep{step("a", 1), step("b", 1)})
	ba := pipeline.Signature("p", []pipeline.PipelineStep{step("b", 1), step("a", 1)})
	if ab == ba {
		t.Fatal("step order did not change the signature")
	}
}

func TestSignature_VersionSuffixIgnored(t *testing.T) {
	steps := []pipeline.PipelineStep{step("a", 1)}
	bare := pipeline.Signature("text", steps)
	if v0 := pipeline.Signature("text@v0", steps); v0 != bare {
		t.Fatalf("text@v0 = %q, want %q", v0, bare)
	}
	if v3 := pipeline.Signature("text@v3", steps); v3 != bare {
		t.Fatalf("text@v3 = %q, want %q", v3, bare)
	}
	// Only a numeric @vN suffix is a version; other names keep their text.
	if other := pipeline.Signature("text@vx", steps); other == bare {
		t.Fatal("a non-numeric suffix was stripped")
	}
}

func TestBaselineSignature_PinsEveryStepToVersionOne(t *testing.T) {
	bumped := []pipeline.PipelineStep{step("a", 3), step("b", 2)}
	ones := []pipeline.PipelineStep{step("a", 1), step("b", 1)}
	if got, want := pipeline.BaselineSignature("p", bumped), pipeline.Signature("p", ones); got != want {
		t.Fatalf("baseline = %q, want the all-version-1 signature %q", got, want)
	}
	if pipeline.BaselineSignature("p", bumped) == pipeline.Signature("p", bumped) {
		t.Fatal("baseline equals the bumped signature")
	}
}

// Entries are quoted before hashing, so a model string holding the
// separator cannot make two configs sign alike.
func TestSignatureOf_ModelStringsCannotForgeEntries(t *testing.T) {
	steps := []pipeline.StepRef{{Name: "a", Version: 1}}
	a := pipeline.SignatureOf("p", steps, "llm=ollama/x\nvision=ollama/y", "vision=ollama/z")
	b := pipeline.SignatureOf("p", steps, "llm=ollama/x", "vision=ollama/y\nvision=ollama/z")
	if a == b {
		t.Fatalf("different configs signed alike: %q", a)
	}
	if x, y := pipeline.BaselineSignatureOf("p", []string{"a"}, "llm=ollama/x\nvision=ollama/y", "vision=ollama/z"),
		pipeline.BaselineSignatureOf("p", []string{"a"}, "llm=ollama/x", "vision=ollama/y\nvision=ollama/z"); x == y {
		t.Fatalf("baselines signed alike: %q", x)
	}
}
