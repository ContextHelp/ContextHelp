package storageutil

import (
	"reflect"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
)

// MetadataDataKey is the object metadata key that holds the structured
// data a caller attached to a capture (`ctxt analyze --data`). Namespaced
// so caller keys never collide with the keys pipeline steps write.
const MetadataDataKey = "data"

// Submission is what a re-submission of an already-stored source carries
// to the object it matches: the caller's note, structured data, hints and
// mentions. It never carries content: a re-submission updates what the
// caller said about the source, not the stored text or its hash.
type Submission struct {
	Note     string
	Data     map[string]any
	Hints    []storage.Tag
	Mentions []uri.URI
}

// Apply merges s into obj and returns the object fields it changed, in
// the names storage.ObjectUpdatedPayload.Fields uses. A non-empty note
// replaces the stored one and non-empty data replaces metadata["data"];
// hints and mentions are added to the stored ones (a stored tag wins on
// a shared label). Empty parts leave the stored value alone.
func (s Submission) Apply(obj *storage.KnowledgeObject) []string {
	var fields []string
	if s.Note != "" && s.Note != obj.InboxNote {
		obj.InboxNote = s.Note
		fields = append(fields, "inbox_note")
	}
	if len(s.Data) > 0 && !reflect.DeepEqual(obj.Metadata[MetadataDataKey], s.Data) {
		if obj.Metadata == nil {
			obj.Metadata = map[string]any{}
		}
		obj.Metadata[MetadataDataKey] = s.Data
		fields = append(fields, "metadata")
	}
	if tags, grew := unionTags(obj.Tags, s.Hints); grew {
		obj.Tags = tags
		fields = append(fields, "tags")
	}
	if ms, grew := unionMentions(obj.Mentions, s.Mentions); grew {
		obj.Mentions = ms
		fields = append(fields, "mentions")
	}
	return fields
}

func unionTags(have, add []storage.Tag) ([]storage.Tag, bool) {
	seen := make(map[string]bool, len(have))
	for _, t := range have {
		seen[t.Label] = true
	}
	out, grew := have, false
	for _, t := range add {
		if !seen[t.Label] {
			seen[t.Label] = true
			out = append(out[:len(out):len(out)], t)
			grew = true
		}
	}
	return out, grew
}

func unionMentions(have, add []uri.URI) ([]uri.URI, bool) {
	seen := make(map[string]bool, len(have))
	for _, m := range have {
		seen[m.String()] = true
	}
	out, grew := have, false
	for _, m := range add {
		if k := m.String(); !seen[k] {
			seen[k] = true
			out = append(out[:len(out):len(out)], m)
			grew = true
		}
	}
	return out, grew
}
