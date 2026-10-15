package storage

import (
	"reflect"
	"testing"
	"time"

	uri "hop.top/cite/scheme"
)

func TestMentionSlug(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"ctxt://entity/project/alpha", "project/alpha"},
		{"ctxt://entity/person/alice", "person/alice"},
		{"ctxt://entity/stripe/api/checkout", "stripe/api/checkout"},
		{"ctxt://object/foo", ""},
		{"other://entity/bar", ""},
	}
	for _, tc := range cases {
		u, err := uri.Parse(tc.in)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.in, err)
		}
		if got := MentionSlug(*u); got != tc.want {
			t.Errorf("MentionSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got := MentionTarget(*u); got != tc.in {
			t.Errorf("MentionTarget(%q) = %q, want the canonical URI string", tc.in, got)
		}
	}
}

func TestMentionEdges(t *testing.T) {
	alice := uri.URI{Scheme: "ctxt", Namespace: "entity", ID: "person/alice"}
	acme := uri.URI{Scheme: "ctxt", Namespace: "entity", ID: "org/acme"}
	nodeID := NewNodeID("obj-1", NodeTypeEntityMention, 0)
	obj := &KnowledgeObject{ID: "obj-1", Graph: &ObjectGraph{Nodes: []GraphNode{{
		ID: nodeID, NodeType: NodeTypeEntityMention, Label: alice.String(),
		Metadata: map[string]any{ProvenanceSourceField: MentionSourceRawContent},
	}}}}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	aliceProv := map[string]any{ProvenanceSourceNode: nodeID, ProvenanceSourceField: MentionSourceRawContent}
	acmeProv := map[string]any{ProvenanceSourceNode: "", ProvenanceSourceField: MentionSourceMentions}

	if got := MentionEdges(nil, []uri.URI{alice}, true, now); got != nil {
		t.Errorf("MentionEdges(nil obj) = %v, want nil", got)
	}
	if got := MentionEdges(&KnowledgeObject{}, []uri.URI{alice}, true, now); got != nil {
		t.Errorf("MentionEdges(obj without ID) = %v, want nil", got)
	}
	if got := MentionEdges(obj, nil, true, now); len(got) != 0 {
		t.Errorf("MentionEdges(no targets) = %v, want none", got)
	}

	// alice twice and acme, both directions: the repeat adds nothing.
	got := MentionEdges(obj, []uri.URI{alice, acme, alice}, true, now)
	want := []struct {
		fromType, from, toType, to, typ string
		meta                            map[string]any
	}{
		{"object", "obj-1", "entity", "ctxt://entity/person/alice", "mentions", aliceProv},
		{"entity", "ctxt://entity/person/alice", "object", "obj-1", "mentioned_in", aliceProv},
		{"object", "obj-1", "entity", "ctxt://entity/org/acme", "mentions", acmeProv},
		{"entity", "ctxt://entity/org/acme", "object", "obj-1", "mentioned_in", acmeProv},
	}
	if len(got) != len(want) {
		t.Fatalf("MentionEdges = %d edges, want %d", len(got), len(want))
	}
	ids := make(map[string]bool, len(got))
	for i, e := range got {
		w := want[i]
		if e.FromType != w.fromType || e.FromID != w.from || e.ToType != w.toType || e.ToID != w.to || e.EdgeType != w.typ {
			t.Errorf("edge[%d] = %s/%s -> %s/%s %s, want %s/%s -> %s/%s %s", i,
				e.FromType, e.FromID, e.ToType, e.ToID, e.EdgeType, w.fromType, w.from, w.toType, w.to, w.typ)
		}
		if !reflect.DeepEqual(e.Metadata, w.meta) {
			t.Errorf("edge[%d] metadata = %v, want %v", i, e.Metadata, w.meta)
		}
		if e.Weight != 1 || !e.CreatedAt.Equal(now) {
			t.Errorf("edge[%d] weight %v at %v, want 1 at %v", i, e.Weight, e.CreatedAt, now)
		}
		if e.ID == "" || ids[e.ID] {
			t.Errorf("edge[%d] ID %q: want a fresh unique ID", i, e.ID)
		}
		ids[e.ID] = true
	}

	fwd := MentionEdges(obj, []uri.URI{alice, acme}, false, now)
	if len(fwd) != 2 {
		t.Fatalf("MentionEdges forward only = %d edges, want 2", len(fwd))
	}
	for i, e := range fwd {
		if e.EdgeType != EdgeTypeMentions || e.FromID != "obj-1" {
			t.Errorf("forward edge[%d] = %s %s -> %s, want a mentions edge from obj-1", i, e.EdgeType, e.FromID, e.ToID)
		}
	}
}
