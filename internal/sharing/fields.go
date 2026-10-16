package sharing

import (
	"slices"
	"strings"
)

// Policy field names (ADR-080 §5.1): what a share policy's fields list
// may name for any type. A structured type (contact) has its own
// vocabulary, declared by its renderer; these cover every other type
// and the "*" entry.
const (
	// FieldTitle renders metadata.title.
	FieldTitle = "title"
	// FieldSummary renders the summaries; a derived field.
	FieldSummary = "summary"
	// FieldContent renders the stored text (raw and cleaned) and its
	// content type.
	FieldContent = "content"
	// FieldSections, FieldDecisions, FieldTasks, FieldMentions and
	// FieldGraph render derived parts.
	FieldSections  = "sections"
	FieldDecisions = "decisions"
	FieldTasks     = "tasks"
	FieldMentions  = "mentions"
	FieldGraph     = "graph"
	// FieldTags renders the tag labels.
	FieldTags = "tags"
	// FieldMetadata renders the whole metadata map.
	FieldMetadata = "metadata"
	// FieldSource renders the source.
	FieldSource = "source"
	// FieldCreatedAt and FieldUpdatedAt are always rendered; a policy
	// may list them for clarity.
	FieldCreatedAt = "created_at"
	FieldUpdatedAt = "updated_at"
	// FieldAll, as the only entry of a type's list, renders every
	// field. It is what a policy that hides nothing says; the derived
	// rule then has nothing to withhold.
	FieldAll = "*"
)

// fieldAliases maps alternative spellings to the canonical policy field.
var fieldAliases = map[string]string{
	"summaries":    FieldSummary,
	"text":         FieldContent,
	"raw_content":  FieldContent,
	"text_content": FieldContent,
	"body":         FieldContent,
	"nodes":        FieldGraph,
	"graph_nodes":  FieldGraph,
	"mention":      FieldMentions,
	"tag":          FieldTags,
	"decision":     FieldDecisions,
	"task":         FieldTasks,
	"section":      FieldSections,
}

// canonicalField returns the canonical policy field name of f.
func canonicalField(f string) string {
	f = strings.ToLower(strings.TrimSpace(f))
	if c, ok := fieldAliases[f]; ok {
		return c
	}
	return f
}

// fieldSet is the projected policy fields of one object, canonical.
type fieldSet struct {
	all    bool
	fields map[string]bool
}

// newFieldSet canonicalizes fields; FieldAll anywhere in the list
// projects everything.
func newFieldSet(fields []string) fieldSet {
	fs := fieldSet{fields: map[string]bool{}}
	for _, f := range fields {
		c := canonicalField(f)
		if c == FieldAll {
			fs.all = true
		}
		fs.fields[c] = true
	}
	return fs
}

// has reports whether field f is projected.
func (fs fieldSet) has(f string) bool { return fs.all || fs.fields[f] }

// KnowledgeObject field names as the step contracts name them
// (pipeline.StepContract.Requires and Produces).
const (
	koRawContent  = "RawContent"
	koTextContent = "TextContent"
	koContentType = "ContentType"
	koMetadata    = "Metadata"
	koSummaries   = "Summaries"
	koSections    = "Sections"
	koTags        = "Tags"
	koMentions    = "Mentions"
	koDecisions   = "Decisions"
	koTasks       = "Tasks"
	koGraph       = "Graph"
	koSource      = "Source"
	koVectors     = "Vectors"
)

// contractAliases mirrors pipeline.KeyAliases: the short names a
// contract may use for KnowledgeObject fields. Kept here so the package
// never imports the pipeline.
var contractAliases = map[string]string{
	"text":         koRawContent,
	"content_type": koContentType,
	"type":         "Type",
	"subtype":      "Subtype",
	"sections":     koSections,
	"tags":         koTags,
	"mentions":     koMentions,
	"metadata":     koMetadata,
}

// canonicalContractKey resolves a contract key as pipeline.Canonicalize
// does: top-level aliases are resolved, dotted keys pass through.
func canonicalContractKey(key string) string {
	if !strings.Contains(key, ".") {
		if c, ok := contractAliases[key]; ok {
			return c
		}
	}
	return key
}

// derivedOnly names the KnowledgeObject fields that only a step
// produces (ADR-080 §5.3: summaries, sections, decisions, tasks, graph
// nodes, mentions). With no step known to have produced one, its
// inputs cannot be shown to be projected, so it is withheld unless the
// policy projects every field.
var derivedOnly = map[string]bool{
	koSummaries: true, koSections: true, koDecisions: true, koTasks: true, koGraph: true, koMentions: true,
}

// alwaysKept are the KnowledgeObject fields every rendered object keeps
// (ADR-080 §5.1: IDs, type and created_at, plus the structural labels
// that carry no content: status, pipeline, profile and updated_at).
var alwaysKept = []string{"ID", "Type", "Subtype", "Status", "Pipeline", "ProfileID", "CreatedAt", "UpdatedAt"}

// visibleKO returns the KnowledgeObject fields the policy fields fs
// project, as step contracts name them: what a producing step may
// require and still have its output shown.
func visibleKO(fs fieldSet) map[string]bool {
	vis := map[string]bool{}
	for _, f := range alwaysKept {
		vis[f] = true
	}
	if fs.all {
		for _, f := range []string{koRawContent, koTextContent, koContentType, koMetadata, koSummaries, koSections, koTags, koMentions, koDecisions, koTasks, koGraph, koSource, koVectors, "Classes"} {
			vis[f] = true
		}
		return vis
	}
	for f := range fs.fields {
		switch f {
		case FieldContent:
			vis[koRawContent], vis[koTextContent], vis[koContentType] = true, true, true
		case FieldTitle:
			vis[koMetadata+".title"] = true
		case FieldMetadata:
			vis[koMetadata] = true
		case FieldSummary:
			vis[koSummaries] = true
		case FieldSections:
			vis[koSections] = true
		case FieldTags:
			vis[koTags] = true
		case FieldMentions:
			vis[koMentions] = true
		case FieldDecisions:
			vis[koDecisions] = true
		case FieldTasks:
			vis[koTasks] = true
		case FieldGraph:
			vis[koGraph] = true
		case FieldSource:
			vis[koSource] = true
		}
	}
	return vis
}

// requireSatisfied reports whether a contract's required key is
// projected: a dotted metadata key is when the whole map or that key
// is.
func requireSatisfied(key string, vis map[string]bool) bool {
	key = canonicalContractKey(key)
	if vis[key] {
		return true
	}
	if strings.HasPrefix(key, koMetadata+".") {
		return vis[koMetadata]
	}
	return false
}

// derivedOK reports whether KnowledgeObject field ko of an object may
// be rendered (ADR-080 §5.3): every step of its pipeline that produces
// the field requires only projected fields. A field no step is known
// to produce is primary and rendered as projected, unless it is a
// derived-only field, which is then withheld. A policy projecting
// every field has nothing to withhold.
func derivedOK(ko string, fs fieldSet, vis map[string]bool, steps []StepContract) bool {
	if fs.all {
		return true
	}
	produced := false
	for _, s := range steps {
		if !slices.ContainsFunc(s.Produces, func(p string) bool { return canonicalContractKey(p) == ko }) {
			continue
		}
		produced = true
		for _, r := range s.Requires {
			if !requireSatisfied(r, vis) {
				return false
			}
		}
	}
	if !produced {
		return !derivedOnly[ko]
	}
	return true
}
