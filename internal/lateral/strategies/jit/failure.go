package jit

import (
	"context"

	"hop.top/kit/go/runtime/bus"
	"hop.top/kit/go/runtime/domain"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/events"
)

// failureSource is the kit event-source identifier for jit-emitted failures.
// Co-located with the rest of the jit package so it's visible alongside the
// failures it explains; the daemon does not need to wire this.
const failureSource = "lateral.strategies.jit"

// EmitProposalFailure publishes ctxt.lateral.scan.failed with
// Mechanism="jit_proposal" when the proposal pipeline (LLM call, parse, or
// any error from CachedProposer.Propose) fails for sourceURL. The event
// represents the whole-scan failure for the captured object — empty
// proposals are NOT failures and must not call this.
//
// Pub-nil is safe (mirrors the pattern in jobs/cold_cycle.go): callers can
// pass nil during tests or when the daemon hasn't wired a publisher yet.
// A nil err is also safe (no-op) so call sites need not double-guard.
//
// Returns the publisher error verbatim; callers usually ignore it (the bus
// is best-effort and the surrounding pipeline has already failed).
func EmitProposalFailure(ctx context.Context, pub domain.EventPublisher, objectID, sourceURL string, err error) error {
	if pub == nil || err == nil {
		return nil
	}
	payload := events.FailurePayload{
		Qualifiers: bus.Qualifiers{
			Mechanism: "jit_proposal",
			Reason:    err.Error(),
		},
		Topic:   events.ScanFailed,
		Subject: sourceURL,
	}
	// object_id is part of the wire schema (schemas/lateral_events.json
	// scan.failed.object_id) but FailurePayload doesn't carry it as a typed
	// field — wrap so it appears in the JSON envelope.
	wrapped := map[string]any{
		"object_id":   objectID,
		"qualifiers":  payload.Qualifiers,
		"topic":       payload.Topic,
		"subject":     payload.Subject,
		"severity":    "error",
	}
	return pub.Publish(ctx, string(events.ScanFailed), failureSource, wrapped)
}

// EmitFetchFailures publishes ctxt.lateral.subpath.failed once per failed
// FetchResult. Per the schema (schemas/lateral_events.json subpath.failed),
// the payload carries object_id, subpath URL, and reason — no Mechanism
// qualifier (the topic itself names the mechanism axis).
//
// Returns the count of events successfully published. A nil pub or empty
// failures slice is a no-op returning 0. Per-event publisher errors are
// swallowed so a transient bus failure on one URL does not suppress the
// rest.
func EmitFetchFailures(ctx context.Context, pub domain.EventPublisher, objectID string, failures []FetchResult) int {
	if pub == nil || len(failures) == 0 {
		return 0
	}
	emitted := 0
	for _, f := range failures {
		if f.Err == nil {
			// Defensive: caller should have routed only failures here, but
			// don't emit a failure event for a successful fetch if a
			// non-failure slipped through.
			continue
		}
		payload := map[string]any{
			"object_id": objectID,
			"subpath":   f.URL,
			"reason":    f.Err.Error(),
			"severity":  "error",
		}
		if err := pub.Publish(ctx, string(events.SubpathFailed), failureSource, payload); err == nil {
			emitted++
		}
	}
	return emitted
}
