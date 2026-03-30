// Package projection exposes ProjectDocument and ProjectIndex for plugin use.
// Plugins that are separate Go modules (under plugins/) must import this
// package instead of internal/projection which is not accessible outside the
// main module.
package projection

import (
	iprojection "github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// ProjectDocument derives a DocumentProjection from a KnowledgeObject.
// Uses graph nodes when Graph is non-nil and non-empty; falls back to flat fields.
func ProjectDocument(ko *pluginapi.KnowledgeObject) pluginapi.DocumentProjection {
	return iprojection.ProjectDocument(ko)
}

// ProjectIndex derives an IndexProjection from a KnowledgeObject.
// Uses graph nodes when Graph is non-nil and non-empty; falls back to flat fields.
func ProjectIndex(ko *pluginapi.KnowledgeObject) pluginapi.IndexProjection {
	return iprojection.ProjectIndex(ko)
}
