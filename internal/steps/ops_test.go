//go:build unix

package steps_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func labels(tags []storage.Tag) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Label)
	}
	return out
}

func tagged(labels ...string) *storage.KnowledgeObject {
	o := draft()
	for _, l := range labels {
		o.Tags = append(o.Tags, storage.Tag{Label: l, Weight: 0.5, Source: "user"})
	}
	return o
}

// Two installed steps adding tags in sequence keep both sets, and the
// tags that were there before.
func TestExternalStepOpsAddTagsInSequence(t *testing.T) {
	f := newFixture(t)
	f.install(t, "one", skill("one"), emit(`{"ops":[{"op":"add_tags","value":["alpha","beta"]}]}`))
	f.install(t, "two", skill("two"), emit(`{"ops":[{"op":"add_tags","value":[{"label":"gamma","weight":0.9,"source":"two"}]}]}`))

	in := tagged("hint")
	mid, err := f.step(t, config.StepInsert{Step: "one"}).Run(context.Background(), in)
	require.NoError(t, err)
	out, err := f.step(t, config.StepInsert{Step: "two"}).Run(context.Background(), mid)
	require.NoError(t, err)

	assert.Equal(t, []string{"hint", "alpha", "beta", "gamma"}, labels(out.Tags))
	assert.Equal(t, storage.Tag{Label: "gamma", Weight: 0.9, Source: "two"}, out.Tags[3])
	assert.Equal(t, []string{"hint"}, labels(in.Tags), "the input object is not mutated")
	assert.Equal(t, []string{"hint", "alpha", "beta"}, labels(mid.Tags), "the first step's result is not mutated")
}

// Tag handling is the tagger's: labels are trimmed, empty labels drop,
// and a label already present (any case) is kept as it was.
func TestExternalStepOpsAddTagsNormalises(t *testing.T) {
	f := newFixture(t)
	f.install(t, "n", skill("n"),
		emit(`{"ops":[{"op":"add_tags","value":["  Go  ","","go","HINT","new","NEW"]}]}`))
	out, err := f.step(t, config.StepInsert{Step: "n"}).Run(context.Background(), tagged("hint"))
	require.NoError(t, err)
	assert.Equal(t, []string{"hint", "Go", "new"}, labels(out.Tags))
	assert.Equal(t, 0.5, out.Tags[0].Weight, "an existing tag wins on collision")
}

func TestExternalStepOpsRemoveTags(t *testing.T) {
	f := newFixture(t)
	f.install(t, "r", skill("r"), emit(`{"ops":[{"op":"remove_tags","value":[" Beta ","absent"]}]}`))
	in := tagged("alpha", "beta", "gamma")
	out, err := f.step(t, config.StepInsert{Step: "r"}).Run(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "gamma"}, labels(out.Tags))
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, labels(in.Tags), "the input object is not mutated")
}

// Ops run in order: an add then a remove of the same label nets out.
func TestExternalStepOpsOrder(t *testing.T) {
	f := newFixture(t)
	f.install(t, "o", skill("o"), emit(`{"ops":[{"op":"add_tags","value":["x","y"]},{"op":"remove_tags","value":["x"]}]}`))
	out, err := f.step(t, config.StepInsert{Step: "o"}).Run(context.Background(), tagged("a"))
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "y"}, labels(out.Tags))
}

// merge_metadata is a deep merge (JSON Merge Patch): objects merge
// recursively, arrays and scalars replace, null deletes the key.
func TestExternalStepOpsMergeMetadataDeep(t *testing.T) {
	f := newFixture(t)
	f.install(t, "m", skill("m"), emit(
		`{"ops":[{"op":"merge_metadata","value":{"repo":{"stars":10,"topics":["b"]},"gone":null,"new":1}}]}`))
	in := draft()
	in.Metadata = map[string]any{
		"repo":   map[string]any{"owner": "o", "stars": float64(1), "topics": []any{"a"}},
		"gone":   "x",
		"keep":   "k",
		"nested": map[string]any{"a": float64(1)},
	}
	out, err := f.step(t, config.StepInsert{Step: "m"}).Run(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"repo":   map[string]any{"owner": "o", "stars": float64(10), "topics": []any{"b"}},
		"keep":   "k",
		"nested": map[string]any{"a": float64(1)},
		"new":    float64(1),
	}, out.Metadata)
	assert.Equal(t, "x", in.Metadata["gone"], "the input object is not mutated")
	assert.Equal(t, float64(1), in.Metadata["repo"].(map[string]any)["stars"], "nested input not mutated")
}

func TestExternalStepOpsMergeMetadataOnNil(t *testing.T) {
	f := newFixture(t)
	f.install(t, "m", skill("m"), emit(`{"ops":[{"op":"merge_metadata","value":{"a":{"b":1,"c":null}}}]}`))
	out, err := f.step(t, config.StepInsert{Step: "m"}).Run(context.Background(), draft())
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": map[string]any{"b": float64(1)}}, out.Metadata)
}

// object and ops together: object (replace) applies first, then ops on
// the result. The replace form alone is unchanged.
func TestExternalStepOpsAfterObject(t *testing.T) {
	f := newFixture(t)
	f.install(t, "b", skill("b"), emit(
		`{"object":{"tags":[{"label":"replaced"}],"raw_content":"x"},"ops":[{"op":"add_tags","value":["added"]}]}`))
	out, err := f.step(t, config.StepInsert{Step: "b"}).Run(context.Background(), tagged("old"))
	require.NoError(t, err)
	assert.Equal(t, []string{"replaced", "added"}, labels(out.Tags))
	assert.Equal(t, "x", out.RawContent)
}

func TestExternalStepOpsEmptyIsNoChange(t *testing.T) {
	f := newFixture(t)
	f.install(t, "e", skill("e"), emit(`{"ops":[]}`))
	in := tagged("keep")
	out, err := f.step(t, config.StepInsert{Step: "e"}).Run(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, []string{"keep"}, labels(out.Tags), "empty ops = no change")
}

func TestExternalStepReplaceFormUnchanged(t *testing.T) {
	f := newFixture(t)
	f.install(t, "p", skill("p"), emit(`{"object":{"tags":[{"label":"only"}]}}`))
	out, err := f.step(t, config.StepInsert{Step: "p"}).Run(context.Background(), tagged("a", "b"))
	require.NoError(t, err)
	assert.Equal(t, []string{"only"}, labels(out.Tags), "object tags still replace")
}

// An invalid op fails the object, permanently, naming the step, the op
// and its position, and never carrying content.
func TestExternalStepOpsInvalid(t *testing.T) {
	cases := []struct{ name, out, want string }{
		{"unknown op", `{"ops":[{"op":"explode","value":[]}]}`, `ops[0]: unknown op "explode"`},
		{"missing op", `{"ops":[{"value":[]}]}`, `ops[0]: missing "op"`},
		{"second op bad", `{"ops":[{"op":"add_tags","value":["a"]},{"op":"nope"}]}`, `ops[1]: unknown op "nope"`},
		{"add_tags not array", `{"ops":[{"op":"add_tags","value":"a"}]}`, `ops[0]: add_tags: value must be an array`},
		{"add_tags bad element", `{"ops":[{"op":"add_tags","value":[3]}]}`, `ops[0]: add_tags: element 0`},
		{"add_tags missing value", `{"ops":[{"op":"add_tags"}]}`, `ops[0]: add_tags: value must be an array`},
		{"remove_tags object element", `{"ops":[{"op":"remove_tags","value":[{"label":"a"}]}]}`, `ops[0]: remove_tags: element 0`},
		{"merge_metadata not object", `{"ops":[{"op":"merge_metadata","value":[1]}]}`, `ops[0]: merge_metadata: value must be an object`},
		{"unknown op field", `{"ops":[{"op":"add_tags","value":["a"],"extra":1}]}`, `ops[0]`},
		{"ops not array", `{"ops":{"op":"add_tags"}}`, `invalid output`},
		{"op not object", `{"ops":["add_tags"]}`, `ops[0]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.install(t, "bad", skill("bad"), emit(tc.out))
			out, err := f.step(t, config.StepInsert{Step: "bad"}).Run(context.Background(), draft())
			require.Error(t, err)
			assert.Nil(t, out)
			assert.Contains(t, err.Error(), tc.want)
			assert.Contains(t, err.Error(), "bad")
			assert.NotContains(t, err.Error(), "jane@example.com")
			assert.True(t, pipeline.IsPermanent(err), "permanent: %v", err)
		})
	}
}

// A failing op fails closed even when object was valid.
func TestExternalStepOpsInvalidAfterValidObject(t *testing.T) {
	f := newFixture(t)
	f.install(t, "bad", skill("bad"), emit(`{"object":{"raw_content":"x"},"ops":[{"op":"zap"}]}`))
	out, err := f.step(t, config.StepInsert{Step: "bad"}).Run(context.Background(), draft())
	require.Error(t, err)
	assert.Nil(t, out)
}

// A read-only object field in the replace form is still rejected when
// ops ride along.
func TestExternalStepOpsDoNotRelaxReadOnly(t *testing.T) {
	f := newFixture(t)
	f.install(t, "ro", skill("ro"), emit(`{"object":{"id":"ko-2"},"ops":[{"op":"add_tags","value":["a"]}]}`))
	_, err := f.step(t, config.StepInsert{Step: "ro"}).Run(context.Background(), draft())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `field "id" is read-only`)
}
