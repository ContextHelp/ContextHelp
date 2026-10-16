package sharing

import (
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// StepContract is what the render layer knows of a pipeline step: the
// KnowledgeObject fields it reads and writes, as pipeline.StepContract
// declares them. The pipeline package is not imported here; the
// service adapts its registry through Provenance.
type StepContract struct {
	Name     string
	Requires []string
	Produces []string
}

// Provenance returns the step contracts of the pipeline named name, in
// order, and whether the pipeline is known at all. A nil Provenance
// knows no pipeline: every derived-only field is then withheld from a
// restricted viewer, since nothing proves its inputs were projected.
type Provenance func(name string) (steps []StepContract, known bool)

// CanaryMinRunes is the shortest withheld value the backstop searches
// a response for (ADR-080 §8.2, decision D20): shorter values (a first
// name, an area code) would blank legitimate text.
const CanaryMinRunes = 6

// Withheld is one raw value the render layer kept out of a viewer's
// view: the field (or the class of the part) it was withheld under,
// and the value itself. The backstop searches responses for these.
// Content is withheld this way (text, summaries, sections, decisions,
// tasks, node contents, a contact's details and note, a title);
// labels are not (tags, metadata keys and the labels under them),
// since a label is a short word that recurs in ids and text and would
// blank legitimate output.
type Withheld struct {
	Category string
	Value    string
}

// Rendered is a viewer's view of an object (ADR-080 §5) and what the
// view left out.
type Rendered struct {
	// Object is the rendered copy: only the projected fields, the
	// structured types rebuilt from them, derived fields only when
	// their inputs are projected, class-excluded parts gone. For an
	// unrestricted viewer it is the stored object itself.
	Object *storage.KnowledgeObject
	// Withheld lists the raw values projected out, each at least
	// CanaryMinRunes long and not part of any value the view kept.
	Withheld []Withheld
}

// StructuredRenderer rebuilds a structured type from its visible
// fields (ADR-080 §5.2). It fills out from r.obj using only the fields
// r projects, and records what it withholds.
type structuredRenderer func(r *renderer, out *storage.KnowledgeObject)

// structuredRenderers holds the renderer of each structured type.
var structuredRenderers = map[string]structuredRenderer{
	projection.TypeContact: renderContact,
}

// Render returns obj as viewer v sees it (ADR-080 §5): field
// projection with IDs, type and created_at always kept, structured
// types rebuilt from their visible fields, class-excluded parts
// dropped, and derived fields dropped unless every input of the step
// that produced them is projected. obj is never modified; an
// unrestricted viewer gets obj itself. prov supplies the step
// contracts of obj's pipeline; nil withholds every derived-only field.
func Render(v Viewer, obj *storage.KnowledgeObject, prov Provenance) Rendered {
	if obj == nil {
		return Rendered{}
	}
	if v.IsUnrestricted() {
		return Rendered{Object: obj}
	}
	plan := v.Plan()
	fs := newFieldSet(plan.FieldsFor(obj.Type))
	var steps []StepContract
	if prov != nil && obj.Pipeline != "" {
		steps, _ = prov(obj.Pipeline)
	}
	r := &renderer{obj: obj, plan: plan, fs: fs, vis: visibleKO(fs), steps: steps}
	out := alwaysKeptCopy(obj)
	if fn, ok := structuredRenderers[obj.Type]; ok {
		fn(r, out)
	} else {
		r.generic(out)
	}
	return Rendered{Object: out, Withheld: r.finish(out)}
}

// renderer carries one object's rendering.
type renderer struct {
	obj   *storage.KnowledgeObject
	plan  RenderPlan
	fs    fieldSet
	vis   map[string]bool
	steps []StepContract
	// withheld collects the raw values projected out, before the rune
	// floor and the kept-value guard of finish.
	withheld []Withheld
}

// alwaysKeptCopy returns a new object carrying only the fields every
// rendered object keeps (alwaysKept).
func alwaysKeptCopy(obj *storage.KnowledgeObject) *storage.KnowledgeObject {
	return &storage.KnowledgeObject{
		ID: obj.ID, Type: obj.Type, Subtype: obj.Subtype, Status: obj.Status,
		Pipeline: obj.Pipeline, ProfileID: obj.ProfileID,
		CreatedAt: obj.CreatedAt, UpdatedAt: obj.UpdatedAt,
	}
}

// ok reports whether the KnowledgeObject field ko may be rendered
// under the derived rule.
func (r *renderer) ok(ko string) bool { return derivedOK(ko, r.fs, r.vis, r.steps) }

// excludes reports whether any of classes is excluded from this
// viewer's view of parts.
func (r *renderer) excludes(classes []string) bool { return r.plan.ExcludesAny(classes) }

// withhold records values projected out under category.
func (r *renderer) withhold(category string, values ...string) {
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			continue
		}
		r.withheld = append(r.withheld, Withheld{Category: category, Value: v})
	}
}

// generic renders an object without a structured renderer: each
// projected field is copied when the derived rule allows it.
func (r *renderer) generic(out *storage.KnowledgeObject) {
	obj := r.obj
	if r.fs.has(FieldContent) {
		out.RawContent, out.TextContent, out.ContentType = obj.RawContent, obj.TextContent, obj.ContentType
	} else {
		r.withhold(FieldContent, obj.RawContent, obj.TextContent)
	}
	if r.fs.has(FieldSummary) && r.ok(koSummaries) {
		out.Summaries = slices.Clone(obj.Summaries)
	} else {
		r.withhold(FieldSummary, obj.Summaries...)
	}
	if r.fs.has(FieldSections) && r.ok(koSections) {
		out.Sections = slices.Clone(obj.Sections)
	} else {
		for _, s := range obj.Sections {
			r.withhold(FieldSections, s.Title, s.Content)
		}
	}
	if r.fs.has(FieldDecisions) && r.ok(koDecisions) {
		out.Decisions = slices.Clone(obj.Decisions)
	} else {
		for _, d := range obj.Decisions {
			r.withhold(FieldDecisions, d.Title, d.Impact)
		}
	}
	if r.fs.has(FieldTasks) && r.ok(koTasks) {
		out.Tasks = slices.Clone(obj.Tasks)
	} else {
		for _, t := range obj.Tasks {
			r.withhold(FieldTasks, t.Title)
		}
	}
	if r.fs.has(FieldMentions) && r.ok(koMentions) {
		out.Mentions = slices.Clone(obj.Mentions)
	}
	if r.fs.has(FieldTags) && r.ok(koTags) {
		out.Tags = slices.Clone(obj.Tags)
	}
	if r.fs.has(FieldSource) {
		out.Source = obj.Source
	}
	r.genericMetadata(out)
	r.genericGraph(out)
}

// genericMetadata copies the metadata map when projected, or its
// title alone when only the title is. A withheld title is a canary;
// other metadata leaves are labels and keys, not content, and are not.
func (r *renderer) genericMetadata(out *storage.KnowledgeObject) {
	obj := r.obj
	switch {
	case r.fs.has(FieldMetadata) && r.ok(koMetadata):
		if obj.Metadata != nil {
			out.Metadata = maps.Clone(obj.Metadata)
		}
	case r.fs.has(FieldTitle) && r.ok(koMetadata):
		if t, ok := obj.Metadata["title"].(string); ok && t != "" {
			out.Metadata = map[string]any{"title": t}
		}
	default:
		if t, ok := obj.Metadata["title"].(string); ok {
			r.withhold(FieldTitle, t)
		}
	}
}

// genericGraph copies the graph when projected, without the nodes
// whose classes are excluded and the edges that touched them.
func (r *renderer) genericGraph(out *storage.KnowledgeObject) {
	g := r.obj.Graph
	if g == nil {
		return
	}
	if !r.fs.has(FieldGraph) || !r.ok(koGraph) {
		for _, n := range g.Nodes {
			r.withhold(FieldGraph, n.Content)
		}
		return
	}
	out.Graph = r.filterGraph(g, nil)
}

// filterGraph returns a copy of g without the nodes whose classes are
// excluded (their content withheld under the class) and without the
// edges touching a dropped node. rewrite, when set, may replace a kept
// node: it returns the replacement and true, or false to keep the node
// as stored.
func (r *renderer) filterGraph(g *storage.ObjectGraph, rewrite func(storage.GraphNode) (storage.GraphNode, bool)) *storage.ObjectGraph {
	out := &storage.ObjectGraph{}
	kept := map[string]bool{}
	for _, n := range g.Nodes {
		if classes := storage.NodeClasses(n); r.excludes(classes) {
			r.withhold(strings.Join(classes, ","), n.Content)
			continue
		}
		if rewrite != nil {
			if rn, ok := rewrite(n); ok {
				n = rn
			}
		}
		n.Metadata = maps.Clone(n.Metadata)
		out.Nodes = append(out.Nodes, n)
		kept[n.ID] = true
	}
	for _, e := range g.Edges {
		if kept[e.FromID] && kept[e.ToID] {
			out.Edges = append(out.Edges, e)
		}
	}
	if len(out.Nodes) == 0 {
		return nil
	}
	return out
}

// renderContact rebuilds a contact from its visible fields (ADR-080
// §5.2): the text from projection.RenderContact over the projected
// fields, the metadata keys of those fields, the tags when projected,
// and the graph as the card node rebuilt from the projected card
// fields plus the note node when the note is projected and its class
// allowed. A field whose declared class the viewer excludes is
// withheld whatever the policy projects. Everything else a contact
// carries (summaries, sections, other nodes, other metadata) is derived
// from the stored card and withheld.
func renderContact(r *renderer, out *storage.KnowledgeObject) {
	obj := r.obj
	visible := r.contactFields()
	out.RawContent = projection.RenderContact(obj, visible)
	out.Metadata = contactMetadata(obj, visible)
	if slices.Contains(visible, projection.ContactTags) {
		out.Tags = slices.Clone(obj.Tags)
	}
	r.withhold(FieldContent, obj.RawContent, obj.TextContent)
	r.withhold(FieldSummary, obj.Summaries...)
	for _, s := range obj.Sections {
		r.withhold(FieldSections, s.Title, s.Content)
	}
	out.Graph = r.contactGraph(visible)
}

// contactFields returns the contact fields the viewer sees, in card
// order: projected, and not of a class the viewer excludes. Every
// other field's value is withheld under the field's name, except the
// tags: labels, not content, and short words that recur in ids and
// text (a "client" tag inside a client contact's id).
func (r *renderer) contactFields() []string {
	visible := make([]string, 0, len(projection.ContactFields))
	for _, f := range projection.ContactFields {
		shown := r.fs.has(f)
		if class, ok := projection.ContactFieldClasses[f]; ok && r.excludes([]string{class}) {
			shown = false
		}
		if shown {
			visible = append(visible, f)
			continue
		}
		if f != projection.ContactTags {
			r.withhold(f, projection.ContactField(r.obj, f))
		}
	}
	return visible
}

// contactMetadata returns the metadata keys of the visible scalar
// fields (title is the name key); nil when none has a value.
func contactMetadata(obj *storage.KnowledgeObject, visible []string) map[string]any {
	var out map[string]any
	for _, f := range visible {
		key := f
		switch f {
		case projection.ContactTitle:
			key = "name"
		case projection.ContactOrg, projection.ContactRole, projection.ContactCity,
			projection.ContactEmail, projection.ContactPhone, projection.ContactAddress:
		default:
			continue
		}
		if v, ok := obj.Metadata[key].(string); ok && v != "" {
			if out == nil {
				out = map[string]any{}
			}
			out[key] = v
		}
	}
	return out
}

// contactGraph rebuilds the contact's graph: the card node with its
// content rebuilt from the visible card fields (dropped when none is
// visible), the note node when the note is visible, nothing else. The
// contents of the other nodes are withheld; a class-excluded node is
// withheld by filterGraph.
func (r *renderer) contactGraph(visible []string) *storage.ObjectGraph {
	if r.obj.Graph == nil {
		return nil
	}
	note := slices.Contains(visible, projection.ContactNote)
	card := projection.RenderContact(r.obj, intersectStrings(visible, projection.ContactCardFields))
	isCard := func(n storage.GraphNode) bool {
		return n.NodeType == storage.NodeTypeSection && n.Label == projection.ContactCardLabel
	}
	filtered := r.filterGraph(r.obj.Graph, func(n storage.GraphNode) (storage.GraphNode, bool) {
		if isCard(n) {
			n.Content = card
			return n, true
		}
		return n, false
	})
	if filtered == nil {
		return nil
	}
	g := &storage.ObjectGraph{}
	kept := map[string]bool{}
	for _, n := range filtered.Nodes {
		switch {
		case isCard(n) && card != "":
		case n.NodeType == storage.NodeTypeSection && n.Label == projection.ContactNoteLabel && note:
		default:
			if !isCard(n) {
				r.withhold(FieldGraph, n.Content)
			}
			continue
		}
		g.Nodes = append(g.Nodes, n)
		kept[n.ID] = true
	}
	for _, e := range filtered.Edges {
		if kept[e.FromID] && kept[e.ToID] {
			g.Edges = append(g.Edges, e)
		}
	}
	if len(g.Nodes) == 0 {
		return nil
	}
	return g
}

// intersectStrings keeps the entries of a that b lists, in a's order.
func intersectStrings(a, b []string) []string {
	var out []string
	for _, x := range a {
		if slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

// finish applies the canary rules to what was withheld: values under
// CanaryMinRunes are dropped, so are values contained in any value the
// view kept (a stored card the rebuilt text repeats, a title the entity
// also carries), and repeats are folded.
func (r *renderer) finish(out *storage.KnowledgeObject) []Withheld {
	if len(r.withheld) == 0 {
		return nil
	}
	kept := keptStrings(out)
	seen := map[string]bool{}
	result := make([]Withheld, 0, len(r.withheld))
	for _, w := range r.withheld {
		if utf8.RuneCountInString(w.Value) < CanaryMinRunes || seen[w.Value] {
			continue
		}
		if slices.ContainsFunc(kept, func(k string) bool { return strings.Contains(k, w.Value) }) {
			continue
		}
		seen[w.Value] = true
		result = append(result, w)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// keptStrings lists the content strings a rendered object carries.
func keptStrings(o *storage.KnowledgeObject) []string {
	out := []string{o.RawContent, o.TextContent, o.Source}
	out = append(out, o.Summaries...)
	for _, s := range o.Sections {
		out = append(out, s.Title, s.Content)
	}
	for _, t := range o.Tags {
		out = append(out, t.Label)
	}
	for _, m := range o.Mentions {
		out = append(out, m.String())
	}
	for _, d := range o.Decisions {
		out = append(out, d.Title, d.Impact)
	}
	for _, t := range o.Tasks {
		out = append(out, t.Title)
	}
	out = append(out, metadataStrings(o.Metadata)...)
	if o.Graph != nil {
		for _, n := range o.Graph.Nodes {
			out = append(out, n.Label, n.Content)
		}
	}
	return out
}

// metadataStrings lists the string leaves of a metadata value.
func metadataStrings(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case map[string]any:
		var out []string
		for _, e := range x {
			out = append(out, metadataStrings(e)...)
		}
		return out
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, metadataStrings(e)...)
		}
		return out
	}
	return nil
}
