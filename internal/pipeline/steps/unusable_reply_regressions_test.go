package steps

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// replyLLM answers every prompt with one fixed reply.
type replyLLM struct{ reply string }

func (r replyLLM) Name() string                                     { return "reply" }
func (r replyLLM) Generate(context.Context, string) (string, error) { return r.reply, nil }

const unusableBody = "The numbat project tracks sightings near the dryandra woodland and records each one in a ledger kept weekly by the rangers. " +
	"We decided to keep the ledger in SQLite because the rangers work offline for most of the season and need a single file."

func unusableDraft() *storage.KnowledgeObject {
	return &storage.KnowledgeObject{ID: "u-1", Type: "text", RawContent: unusableBody}
}

// A reply the step cannot use is reported to the context's recorder; a
// well-formed reply that holds nothing is not. With no recorder the step
// returns exactly what it returns with one: the report only observes.
func TestUnusableReplyRegressions_ReportedOnlyToARecorder(t *testing.T) {
	cases := []struct {
		name     string
		step     func(llm providers.LLMProvider) pipeline.PipelineStep
		unusable []string // replies the step cannot use
		valid    []string // well-formed replies holding nothing
	}{
		{"sectioner", func(l providers.LLMProvider) pipeline.PipelineStep { return NewSectionerWithLLM(l) }, []string{"", "  \n"}, []string{"None."}},
		{"tagger", func(l providers.LLMProvider) pipeline.PipelineStep { return NewTaggerWithLLM(l) }, []string{"", " \t"}, []string{",", " , "}},
		{"entity_extractor", func(l providers.LLMProvider) pipeline.PipelineStep { return NewEntityExtractorWithLLM(l) }, []string{"", "\n"}, []string{"none", "@@@"}},
		{"graph_extractor", func(l providers.LLMProvider) pipeline.PipelineStep { return NewGraphExtractorWithLLM(l) }, []string{"", "not json at all", `{"summary":`}, []string{`{"summary":"s","topics":[],"decisions":[]}`}},
		{"structured_metadata", func(l providers.LLMProvider) pipeline.PipelineStep { return NewStructuredMetadataExtractorWithLLM(l) }, []string{"", "not json at all"}, nil},
	}
	for _, c := range cases {
		for _, reply := range c.unusable {
			t.Run(c.name+"/unusable/"+strings.TrimSpace(reply), func(t *testing.T) {
				step := c.step(replyLLM{reply})
				plain, err := step.Run(context.Background(), unusableDraft())
				if err != nil {
					t.Fatal(err)
				}
				ctx, rec := providers.WithCallRecorder(context.Background())
				recorded, err := c.step(replyLLM{reply}).Run(ctx, unusableDraft())
				if err != nil {
					t.Fatal(err)
				}
				if rec.Err() == nil || !strings.Contains(rec.Err().Error(), c.name) {
					t.Errorf("recorder = %v, want a report naming %s", rec.Err(), c.name)
				}
				if !reflect.DeepEqual(plain, recorded) {
					t.Errorf("the draft differs with a recorder installed:\n%+v\n%+v", plain, recorded)
				}
			})
		}
		for _, reply := range c.valid {
			t.Run(c.name+"/valid/"+strings.TrimSpace(reply), func(t *testing.T) {
				ctx, rec := providers.WithCallRecorder(context.Background())
				if _, err := c.step(replyLLM{reply}).Run(ctx, unusableDraft()); err != nil {
					t.Fatal(err)
				}
				if err := rec.Err(); err != nil {
					t.Errorf("a well-formed empty reply was reported: %v", err)
				}
			})
		}
	}
}
