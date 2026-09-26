package projection

import "github.com/ideacrafterslabs/ctxt/pkg/pluginapi"

// BodyText returns the object's body under the defaulting rule storage
// applies when it persists an object: TextContent, else RawContent.
func BodyText(ko *pluginapi.KnowledgeObject) string {
	if ko == nil {
		return ""
	}
	if ko.TextContent == "" {
		return ko.RawContent
	}
	return ko.TextContent
}

// EmbeddingText returns the text ingest embeds for ko: ProjectIndex's
// EmbeddingText (body, summaries, sections) with the body taken from
// BodyText. A pipeline draft has not been through storage's TextContent
// default yet, so it yields the same text before and after persistence.
// ko is not modified.
func EmbeddingText(ko *pluginapi.KnowledgeObject) string {
	if ko == nil {
		return ""
	}
	if ko.TextContent != "" || ko.RawContent == "" {
		return ProjectIndex(ko).EmbeddingText
	}
	defaulted := *ko
	defaulted.TextContent = ko.RawContent
	return ProjectIndex(&defaulted).EmbeddingText
}
