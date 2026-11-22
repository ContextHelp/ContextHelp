package pipeline

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestNonTextFieldCoversEveryStoredText(t *testing.T) {
	bad := "a\x00b"
	cases := map[string]struct {
		obj  storage.KnowledgeObject
		want string
	}{
		"type":         {storage.KnowledgeObject{Type: bad}, "type"},
		"subtype":      {storage.KnowledgeObject{Subtype: bad}, "subtype"},
		"content type": {storage.KnowledgeObject{ContentType: "\xff"}, "content_type"},
		"profile":      {storage.KnowledgeObject{ProfileID: bad}, "profile_id"},
		"inbox note":   {storage.KnowledgeObject{InboxNote: bad}, "inbox_note"},
		"classes":      {storage.KnowledgeObject{Classes: []string{bad}}, "classes[0]"},
		"decision":     {storage.KnowledgeObject{Decisions: []storage.Decision{{Title: bad}}}, "decisions[0]"},
		"task":         {storage.KnowledgeObject{Tasks: []storage.Task{{Status: bad}}}, "tasks[0]"},
		"node label": {storage.KnowledgeObject{Graph: &storage.ObjectGraph{
			Nodes: []storage.GraphNode{{ID: "n", Label: bad}}}}, "graph.nodes[0]"},
		"node metadata": {storage.KnowledgeObject{Graph: &storage.ObjectGraph{
			Nodes: []storage.GraphNode{{ID: "n", Metadata: map[string]any{"k": []any{bad}}}}}}, "graph.nodes[0].metadata.k[0] (string)"},
		"edge": {storage.KnowledgeObject{Graph: &storage.ObjectGraph{
			Edges: []storage.GraphEdge{{ID: "e", FromID: bad}}}}, "graph.edges[0]"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := NonTextField(&tc.obj, false, true); got != tc.want {
				t.Fatalf("NonTextField = %q, want %q", got, tc.want)
			}
		})
	}
	if got := NonTextField(&storage.KnowledgeObject{Type: "note", Summaries: []string{"é"}}, false, true); got != "" {
		t.Fatalf("clean object flagged: %q", got)
	}
}

func TestReinforceNonTextFieldIgnoresUnwrittenFields(t *testing.T) {
	obj := &storage.KnowledgeObject{
		Summaries: []string{"\xff"}, InboxNote: "\x00", Type: "\xff",
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{{ID: "n", Label: "\xff"}}},
	}
	if got := ReinforceNonTextField(obj); got != "" {
		t.Fatalf("flagged unwritten field %q", got)
	}
	obj.Tags = []storage.Tag{{Label: "ok"}, {Label: "\xff"}}
	if got := ReinforceNonTextField(obj); got != "tags[1]" {
		t.Fatalf("got %q, want tags[1]", got)
	}
}
