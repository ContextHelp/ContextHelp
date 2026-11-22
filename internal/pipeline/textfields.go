package pipeline

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// NonTextField names the first text field of obj that holds binary
// (invalid UTF-8 or NUL, see blobref.NeedsBlob), or "" when they all are
// text. Only content has a blob to go to: text content, summaries,
// sections, tags and the source must be text. With skipTextContent a binary
// object's own text_content is not checked (an externalize step empties
// it). withMetadata adds every string in the metadata, keys included.
func NonTextField(obj *storage.KnowledgeObject, skipTextContent, withMetadata bool) string {
	if !skipTextContent && blobref.NeedsBlob(obj.TextContent) {
		return "text_content"
	}
	if blobref.NeedsBlob(obj.Source) || blobref.NeedsBlob(obj.SourceKey) {
		return "source"
	}
	for i, t := range obj.Tags {
		if blobref.NeedsBlob(t.Label) {
			return fmt.Sprintf("tags[%d]", i)
		}
	}
	for i, s := range obj.Summaries {
		if blobref.NeedsBlob(s) {
			return fmt.Sprintf("summaries[%d]", i)
		}
	}
	for i, sec := range obj.Sections {
		if blobref.NeedsBlob(sec.Title) || blobref.NeedsBlob(sec.Content) {
			return fmt.Sprintf("sections[%d]", i)
		}
		if withMetadata {
			if k := nonTextIn(sec.Metadata); k != "" {
				return fmt.Sprintf("sections[%d].metadata.%s", i, k)
			}
		}
	}
	if withMetadata {
		if k := nonTextIn(obj.Metadata); k != "" {
			return "metadata." + k
		}
	}
	return ""
}

// nonTextIn is the path of the first binary string (or key) in m, or "".
func nonTextIn(m map[string]any) string {
	for k, v := range m {
		if blobref.NeedsBlob(k) {
			return fmt.Sprintf("%q", k)
		}
		if p := nonTextValue(v); p != "" {
			return k + p
		}
	}
	return ""
}

func nonTextValue(v any) string {
	switch t := v.(type) {
	case string:
		if blobref.NeedsBlob(t) {
			return " (string)"
		}
	case []string:
		for i, s := range t {
			if blobref.NeedsBlob(s) {
				return fmt.Sprintf("[%d]", i)
			}
		}
	case []any:
		for i, e := range t {
			if p := nonTextValue(e); p != "" {
				return fmt.Sprintf("[%d]%s", i, p)
			}
		}
	case map[string]any:
		if k := nonTextIn(t); k != "" {
			return "." + k
		}
	case []map[string]any:
		for i, e := range t {
			if k := nonTextIn(e); k != "" {
				return fmt.Sprintf("[%d].%s", i, k)
			}
		}
	case map[string]string:
		for k, s := range t {
			if blobref.NeedsBlob(k) || blobref.NeedsBlob(s) {
				return "." + k
			}
		}
	case []map[string]string:
		for i, e := range t {
			for k, s := range e {
				if blobref.NeedsBlob(k) || blobref.NeedsBlob(s) {
					return fmt.Sprintf("[%d].%s", i, k)
				}
			}
		}
	}
	return ""
}
