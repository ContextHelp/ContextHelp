package sharing_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const (
	renderEmail   = "sarah.lee@acme.example"
	renderPhone   = "+1 555 0100 200"
	renderAddress = "12 Harbor St, Boston"
	renderNote    = "prefers mornings, allergic to peanuts"
	renderSummary = "Sarah Lee, Head of Ops at Acme, reachable at " + renderEmail
)

// sarah is a contact as cardamum ingests it: details in metadata, the
// card text, a card node and a personal note node, plus a summary a
// step derived from the whole card.
func sarah() *storage.KnowledgeObject {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	card := "Sarah Lee | Head of Ops @ Acme\nBoston"
	o := &storage.KnowledgeObject{
		ID: "obj-sarah", Type: projection.TypeContact, Status: storage.ObjectStatusActive,
		RawContent: card, ContentHash: "sha256:sarah", ProfileID: "work", Source: "cardamum:default/sarah",
		Metadata: map[string]any{
			"name": "Sarah Lee", "org": "Acme", "role": "Head of Ops", "city": "Boston",
			"email": renderEmail, "phone": renderPhone, "address": renderAddress, "addressbook": "default",
		},
		Summaries: []string{renderSummary},
		Tags:      []storage.Tag{{Label: "work"}},
		CreatedAt: now, UpdatedAt: now,
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
			{ID: "obj-sarah/section/0", NodeType: storage.NodeTypeSection, Label: projection.ContactCardLabel, Content: card},
			{ID: "obj-sarah/section/1", NodeType: storage.NodeTypeSection, Label: projection.ContactNoteLabel, Content: renderNote, Order: 1},
		}},
	}
	storage.SetNodeClasses(&o.Graph.Nodes[1], []string{storage.ClassPersonal})
	return o
}

// viewerFor compiles a viewer over one grant of p, with ceiling deny.
func viewerFor(t *testing.T, p sharing.Policy, deny ...string) sharing.Viewer {
	t.Helper()
	v, err := sharing.Compile(sharing.Input{
		PrincipalID: "prn_render", DenyClasses: deny,
		Grants: []sharing.Grant{{ID: "g1", Audience: p.Name, Policy: p}},
	})
	require.NoError(t, err)
	return v
}

func withheldValues(r sharing.Rendered) []string {
	out := make([]string, 0, len(r.Withheld))
	for _, w := range r.Withheld {
		out = append(out, w.Value)
	}
	return out
}

func TestRenderUnrestrictedIsTheStoredObject(t *testing.T) {
	obj := sarah()
	r := sharing.Render(sharing.Unrestricted, obj, nil)
	assert.Same(t, obj, r.Object)
	assert.Empty(t, r.Withheld)
}

func TestRenderZeroViewerKeepsOnlyIdentity(t *testing.T) {
	obj := sarah()
	r := sharing.Render(sharing.Viewer{}, obj, nil)
	require.NotNil(t, r.Object)
	assert.Equal(t, obj.ID, r.Object.ID)
	assert.Equal(t, obj.Type, r.Object.Type)
	assert.Equal(t, obj.CreatedAt, r.Object.CreatedAt)
	assert.Empty(t, r.Object.RawContent)
	assert.Empty(t, r.Object.Metadata)
	assert.Empty(t, r.Object.Summaries)
	assert.Nil(t, r.Object.Graph)
	assert.Equal(t, "", obj.ID[:0], "the stored object is untouched")
	assert.Equal(t, renderEmail, obj.Metadata["email"])
}

// The colleague sees the card rebuilt from title, org, role, tags and
// city: no email, phone, address, note or summary anywhere, and the
// withheld values are what the backstop must search for.
func TestRenderContactForColleague(t *testing.T) {
	tpl, _ := sharing.Template(sharing.TemplateColleagues)
	v := viewerFor(t, tpl)
	obj := sarah()
	r := sharing.Render(v, obj, nil)
	out := r.Object

	assert.Equal(t, "Sarah Lee | Head of Ops @ Acme\nBoston\nTags: work", out.RawContent)
	assert.Equal(t, map[string]any{"name": "Sarah Lee", "org": "Acme", "role": "Head of Ops", "city": "Boston"}, out.Metadata)
	assert.Equal(t, obj.Tags, out.Tags)
	assert.Empty(t, out.Summaries, "a summary derives from the whole card")
	assert.Empty(t, out.Source)
	assert.Empty(t, out.ContentHash)
	require.NotNil(t, out.Graph)
	require.Len(t, out.Graph.Nodes, 1, "the personal note node is gone")
	assert.Equal(t, "Sarah Lee | Head of Ops @ Acme\nBoston", out.Graph.Nodes[0].Content)

	vals := withheldValues(r)
	assert.Contains(t, vals, renderEmail)
	assert.Contains(t, vals, renderPhone)
	assert.Contains(t, vals, renderAddress)
	assert.Contains(t, vals, renderNote)
	assert.Contains(t, vals, renderSummary)
	assert.NotContains(t, vals, "Sarah Lee | Head of Ops @ Acme\nBoston", "the stored card is inside the rendered text")
	assert.NotContains(t, vals, "default", "metadata labels are not canaries")
	for _, w := range r.Withheld {
		assert.GreaterOrEqual(t, len([]rune(w.Value)), sharing.CanaryMinRunes)
	}
	// Categories name the field.
	cats := map[string]string{}
	for _, w := range r.Withheld {
		cats[w.Value] = w.Category
	}
	assert.Equal(t, projection.ContactEmail, cats[renderEmail])
	assert.Equal(t, projection.ContactNote, cats[renderNote])
	assert.Equal(t, sharing.FieldSummary, cats[renderSummary])

	// The stored object is untouched.
	assert.Len(t, obj.Graph.Nodes, 2)
	assert.Equal(t, renderEmail, obj.Metadata["email"])
	assert.Len(t, obj.Summaries, 1)
}

// The partner sees email and phone but no address and no note.
func TestRenderContactForPartner(t *testing.T) {
	tpl, _ := sharing.Template(sharing.TemplatePartners)
	r := sharing.Render(viewerFor(t, tpl), sarah(), nil)
	out := r.Object
	assert.Equal(t, "Sarah Lee | Head of Ops @ Acme\nBoston\nEmail: "+renderEmail+"\nPhone: "+renderPhone+"\nTags: work", out.RawContent)
	assert.Equal(t, renderEmail, out.Metadata["email"])
	assert.Equal(t, renderPhone, out.Metadata["phone"])
	assert.NotContains(t, out.Metadata, "address")
	vals := withheldValues(r)
	assert.NotContains(t, vals, renderEmail)
	assert.Contains(t, vals, renderAddress)
	assert.Contains(t, vals, renderNote)
}

// A policy that projects the note still never shows it when the
// ceiling denies the note's class; without the ceiling it does.
func TestRenderContactNoteUnderCeiling(t *testing.T) {
	open := sharing.Policy{
		Name: "open-contacts", Data: "type==contact",
		Fields:       map[string][]string{"contact": {"title", "note"}},
		Capabilities: sharing.Capabilities{Search: []string{sharing.SearchFTS}},
	}
	shown := sharing.Render(viewerFor(t, open), sarah(), nil)
	assert.Equal(t, "Sarah Lee\n"+renderNote, shown.Object.RawContent)
	require.NotNil(t, shown.Object.Graph)
	assert.Len(t, shown.Object.Graph.Nodes, 2)

	denied := sharing.Render(viewerFor(t, open, storage.ClassPersonal), sarah(), nil)
	assert.Equal(t, "Sarah Lee", denied.Object.RawContent)
	require.NotNil(t, denied.Object.Graph)
	assert.Len(t, denied.Object.Graph.Nodes, 1)
	assert.Contains(t, withheldValues(denied), renderNote)
}

// A short withheld value never becomes a canary.
func TestRenderShortValuesAreNotCanaries(t *testing.T) {
	narrow := sharing.Policy{Name: "narrow", Data: "type==contact", Fields: map[string][]string{"contact": {"title", "org"}}}
	obj := sarah()
	obj.Metadata["city"] = "Lee"
	r := sharing.Render(viewerFor(t, narrow), obj, nil)
	assert.Equal(t, "Sarah Lee @ Acme", r.Object.RawContent)
	assert.NotContains(t, withheldValues(r), "Lee")
	assert.Contains(t, withheldValues(r), "Head of Ops")
}

// generic objects

func noteObject() *storage.KnowledgeObject {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	o := &storage.KnowledgeObject{
		ID: "obj-note", Type: "note", Status: storage.ObjectStatusActive, Pipeline: "text.short",
		RawContent: "quarterly planning: hire two engineers before December",
		Metadata:   map[string]any{"title": "Quarterly planning", "topic": "hiring"},
		Summaries:  []string{"Plan to hire two engineers by December"},
		Sections:   []storage.Section{{Title: "Hiring", Content: "two engineers before December"}},
		Tags:       []storage.Tag{{Label: "work"}},
		CreatedAt:  now, UpdatedAt: now,
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
			{ID: "obj-note/summary/0", NodeType: storage.NodeTypeSummary, Content: "Plan to hire two engineers by December"},
			{ID: "obj-note/section/1", NodeType: storage.NodeTypeSection, Content: "salary band is confidential", Order: 1},
		}},
	}
	storage.SetNodeClasses(&o.Graph.Nodes[1], []string{"financial"})
	return o
}

func notePolicy(fields ...string) sharing.Policy {
	return sharing.Policy{Name: "notes", Data: "type==note", Fields: map[string][]string{"*": fields}}
}

// textShort is the provenance of a note: a summarizer and a tagger,
// both reading the content.
func textShort(name string) ([]sharing.StepContract, bool) {
	if name != "text.short" {
		return nil, false
	}
	return []sharing.StepContract{
		{Name: "summarizer", Requires: []string{"RawContent"}, Produces: []string{"Summaries", "Graph"}},
		{Name: "tagger", Requires: []string{"text"}, Produces: []string{"tags"}},
		{Name: "sectioner", Requires: []string{"RawContent"}, Produces: []string{"Sections"}},
	}, true
}

// A summary, sections and tags derived from hidden content are
// withheld; with the content projected they show.
func TestRenderDerivedFieldsFollowTheirInputs(t *testing.T) {
	hidden := sharing.Render(viewerFor(t, notePolicy("title", "summary", "tags", "sections", "created_at")), noteObject(), textShort)
	assert.Empty(t, hidden.Object.RawContent)
	assert.Empty(t, hidden.Object.Summaries, "the summary derives from the hidden content")
	assert.Empty(t, hidden.Object.Sections)
	assert.Empty(t, hidden.Object.Tags, "the tagger read the hidden content")
	assert.Equal(t, map[string]any{"title": "Quarterly planning"}, hidden.Object.Metadata)
	vals := withheldValues(hidden)
	assert.Contains(t, vals, "quarterly planning: hire two engineers before December")
	assert.Contains(t, vals, "Plan to hire two engineers by December")
	assert.NotContains(t, vals, "hiring", "metadata labels are not canaries")

	shown := sharing.Render(viewerFor(t, notePolicy("title", "summary", "tags", "sections", "content")), noteObject(), textShort)
	assert.Equal(t, "quarterly planning: hire two engineers before December", shown.Object.RawContent)
	assert.Equal(t, []string{"Plan to hire two engineers by December"}, shown.Object.Summaries)
	assert.Len(t, shown.Object.Sections, 1)
	assert.Len(t, shown.Object.Tags, 1)
	// The graph is not projected: its classed node is the one canary;
	// its summary node repeats the kept summary and is no canary.
	assert.Equal(t, []string{"salary band is confidential"}, withheldValues(shown))
}

// Without provenance a derived-only field is withheld even when the
// policy projects it; a primary field (tags) shows.
func TestRenderUnknownProvenanceWithholdsDerivedOnly(t *testing.T) {
	obj := noteObject()
	obj.Pipeline = ""
	r := sharing.Render(viewerFor(t, notePolicy("summary", "tags", "content")), obj, textShort)
	assert.Empty(t, r.Object.Summaries)
	assert.Len(t, r.Object.Tags, 1)
	assert.Equal(t, obj.RawContent, r.Object.RawContent)
	assert.Contains(t, withheldValues(r), "Plan to hire two engineers by December")
}

// A graph is projected without its class-excluded nodes and with the
// edges that touched them gone.
func TestRenderGraphDropsExcludedParts(t *testing.T) {
	obj := noteObject()
	obj.Graph.Edges = []storage.GraphEdge{{ID: "e1", FromID: "obj-note/summary/0", ToID: "obj-note/section/1", EdgeType: "contains"}}
	p := notePolicy("content", "graph")
	p.ExcludeClasses = []string{"financial"}
	r := sharing.Render(viewerFor(t, p), obj, textShort)
	require.NotNil(t, r.Object.Graph)
	require.Len(t, r.Object.Graph.Nodes, 1)
	assert.Equal(t, "obj-note/summary/0", r.Object.Graph.Nodes[0].ID)
	assert.Empty(t, r.Object.Graph.Edges)
	assert.Contains(t, withheldValues(r), "salary band is confidential")
}

// Metadata a step wrote from an input the viewer never sees is
// withheld whole, title included.
func TestRenderMetadataIsCoarse(t *testing.T) {
	dedup := func(string) ([]sharing.StepContract, bool) {
		return []sharing.StepContract{{Name: "dedup", Requires: []string{"Vectors"}, Produces: []string{"Metadata"}}}, true
	}
	r := sharing.Render(viewerFor(t, notePolicy("title", "metadata", "content")), noteObject(), dedup)
	assert.Empty(t, r.Object.Metadata)
	assert.Contains(t, withheldValues(r), "Quarterly planning")
}

// A policy projecting every field renders the stored object whole and
// withholds nothing, whatever the provenance.
func TestRenderEveryFieldWithholdsNothing(t *testing.T) {
	obj := noteObject()
	r := sharing.Render(viewerFor(t, notePolicy(sharing.FieldAll)), obj, nil)
	assert.Equal(t, obj.RawContent, r.Object.RawContent)
	assert.Equal(t, obj.Summaries, r.Object.Summaries)
	assert.Equal(t, obj.Metadata, r.Object.Metadata)
	assert.Equal(t, obj.Tags, r.Object.Tags)
	require.NotNil(t, r.Object.Graph)
	assert.Len(t, r.Object.Graph.Nodes, 2)
	assert.Empty(t, r.Withheld)
}

// The ceiling applies to parts whatever the policy says.
func TestRenderCeilingDropsParts(t *testing.T) {
	r := sharing.Render(viewerFor(t, notePolicy(sharing.FieldAll), "financial"), noteObject(), nil)
	require.NotNil(t, r.Object.Graph)
	assert.Len(t, r.Object.Graph.Nodes, 1)
	assert.Contains(t, withheldValues(r), "salary band is confidential")
}

// A policy may say "*" only alone.
func TestPolicyFieldAllMustBeAlone(t *testing.T) {
	ok := sharing.Policy{Name: "all", Data: "type==note", Fields: map[string][]string{"*": {"*"}}}
	assert.NoError(t, ok.Validate())
	bad := sharing.Policy{Name: "all", Data: "type==note", Fields: map[string][]string{"*": {"*", "title"}}}
	assert.Error(t, bad.Validate())
}
