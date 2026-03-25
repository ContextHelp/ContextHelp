// Package taxonomy centralises KnowledgeObject Type and Subtype string constants.
// Use these instead of inline literals to prevent typos and ease future refactors.
package taxonomy

// Object types — values for KnowledgeObject.Type.
const (
	TypeURL      = "url"
	TypeText     = "text"
	TypeImage    = "image"
	TypeAudio    = "audio"
	TypeVideo    = "video"
	TypeFeed     = "feed"
	TypeDocument = "document"
	TypeCode     = "code"
)
