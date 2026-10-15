package storage

import (
	"reflect"
	"testing"
)

func TestMentionProvenance(t *testing.T) {
	const obj = "obj-1"
	const alice = "ctxt://entity/person/alice"
	const acme = "ctxt://entity/org/acme"
	g := &ObjectGraph{Nodes: []GraphNode{
		// A non-mention node with the same label must not be picked.
		{ID: NewNodeID(obj, NodeTypeSection, 0), NodeType: NodeTypeSection, Label: alice},
		{
			ID: NewNodeID(obj, NodeTypeEntityMention, 0), NodeType: NodeTypeEntityMention, Label: alice,
			Metadata: map[string]any{ProvenanceSourceField: MentionSourceRawContent},
		},
		{ID: NewNodeID(obj, NodeTypeEntityMention, 1), NodeType: NodeTypeEntityMention, Label: acme},
	}}

	tests := []struct {
		name    string
		g       *ObjectGraph
		mention string
		want    map[string]any
	}{
		{
			name: "nil graph", g: nil, mention: alice,
			want: map[string]any{ProvenanceSourceNode: "", ProvenanceSourceField: MentionSourceMentions},
		},
		{
			name: "node records its field", g: g, mention: alice,
			want: map[string]any{ProvenanceSourceNode: "obj-1/entity_mention/0", ProvenanceSourceField: MentionSourceRawContent},
		},
		{
			name: "node without a field", g: g, mention: acme,
			want: map[string]any{ProvenanceSourceNode: "obj-1/entity_mention/1", ProvenanceSourceField: MentionSourceMentions},
		},
		{
			name: "no node for the mention", g: g, mention: "ctxt://entity/project/x",
			want: map[string]any{ProvenanceSourceNode: "", ProvenanceSourceField: MentionSourceMentions},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MentionProvenance(tt.g, tt.mention); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MentionProvenance(%q) = %v, want %v", tt.mention, got, tt.want)
			}
		})
	}
	if n := len(g.Nodes); n != 3 {
		t.Errorf("MentionProvenance added nodes: %d, want 3", n)
	}
}
