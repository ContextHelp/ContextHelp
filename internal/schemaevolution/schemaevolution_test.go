package schemaevolution

import (
	"reflect"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// stepOutput stands in for the structured_metadata step's struct, which
// objects carry in-process.
type stepOutput struct {
	Type       string   `json:"type"`
	Topics     []string `json:"topics"`
	Confidence float64  `json:"confidence"`
}

func obj(meta any) *storage.KnowledgeObject {
	return &storage.KnowledgeObject{Metadata: map[string]any{metadataKey: meta}}
}

func TestSuggest(t *testing.T) {
	objects := []*storage.KnowledgeObject{
		// As decoded from the API.
		obj(map[string]any{"type": "incident", "topics": []any{"k8s", "auth"}}),
		obj(map[string]any{"type": "incident", "topics": []any{"k8s"}}),
		// As stored in-process.
		obj(&stepOutput{Type: "runbook", Topics: []string{"k8s", "auth"}}),
		obj(stepOutput{Type: "runbook", Topics: []string{"billing"}}),
		// Known to the schema, below MinCount, or without metadata.
		obj(map[string]any{"type": "task", "topics": []any{"ops"}}),
		obj(map[string]any{"type": "task", "topics": []any{"ops"}}),
		obj(map[string]any{"type": "question"}),
		obj(nil),
		{Metadata: map[string]any{"type": "incident"}},
	}
	schema := config.ProfileSchema{EntityTypes: []string{"task"}, TopicVocabulary: []string{"ops"}}

	got := Suggest("ops", schema, objects)
	want := &Result{
		Profile:   "ops",
		NewTypes:  []Suggestion{{Value: "incident", Count: 2}, {Value: "runbook", Count: 2}},
		NewTopics: []Suggestion{{Value: "k8s", Count: 3}, {Value: "auth", Count: 2}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Suggest =\n %+v\nwant\n %+v", got, want)
	}
}
