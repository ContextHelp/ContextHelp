package projection

import (
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// BodyText returns the object's body under the defaulting rule storage
// applies when it persists an object: TextContent, else RawContent. A
// blob:// ref is a pointer to content, not text, and is never the body:
// an object whose content is a ref and that carries no text has none.
func BodyText(ko *storage.KnowledgeObject) string {
	if ko == nil {
		return ""
	}
	for _, s := range []string{ko.TextContent, ko.RawContent} {
		if s != "" && !blobref.Is(s) {
			return s
		}
	}
	return ""
}

// EmbeddingText returns the text ingest embeds for ko: ProjectIndex's
// EmbeddingText (body, summaries, sections) with the body taken from
// BodyText. A pipeline draft has not been through storage's TextContent
// default yet, so it yields the same text before and after persistence.
// ko is not modified.
func EmbeddingText(ko *storage.KnowledgeObject) string {
	if ko == nil {
		return ""
	}
	if ko.TextContent != "" || ko.RawContent == "" {
		return ProjectIndex(ko).EmbeddingText
	}
	defaulted := *ko
	defaulted.TextContent = BodyText(ko)
	return ProjectIndex(&defaulted).EmbeddingText
}
