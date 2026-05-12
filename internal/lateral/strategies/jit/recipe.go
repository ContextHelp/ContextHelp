package jit

import (
	"context"

	"hop.top/kit/go/runtime/bus"
	kitdomain "hop.top/kit/go/runtime/domain"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/events"
)

// EmitRecipeServed publishes ctxt.lateral.recipe.served with
// Circumstance="llm_outage" when a cached JIT proposal is served because the
// LLM is currently unavailable. This is a degraded-success signal, not a
// failure — observers route it via notify.SeverityInfo (vs Error for the
// scan.failed/subpath.failed pair in failure.go).
//
// Schema (schemas/lateral_events.json recipe.served): payload carries
// source_domain, page_type, and qualifiers.circumstance="llm_outage".
//
// Pub-nil is safe (mirrors EmitProposalFailure / EmitFetchFailures). Empty
// sourceDomain or pageType still emits — caller validates upstream if needed.
func EmitRecipeServed(ctx context.Context, pub kitdomain.EventPublisher, sourceDomain, pageType string) error {
	if pub == nil {
		return nil
	}
	payload := map[string]any{
		"source_domain": sourceDomain,
		"page_type":     pageType,
		"qualifiers": bus.Qualifiers{
			Circumstance: "llm_outage",
		},
		"severity": "info",
	}
	return pub.Publish(ctx, string(events.RecipeServed), failureSource, payload)
}
