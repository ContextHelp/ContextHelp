package adapter

import "fmt"

// LifecycleTopic returns the kit-conformant topic name for an adapter
// lifecycle event. Topics follow kit/bus's 4-segment past-tense rule:
// dpkms.adapter.lifecycle.<action>.
//
// Past-tense actions emitted by the runner (T-0487):
//
//   - started: the adapter's Start() was invoked. Emitted BEFORE
//     Start to give subscribers a "transition begun" signal even if
//     Start subsequently errors.
//   - readied: Start() returned without error and the adapter is
//     usable. Emitted AFTER successful Start.
//   - drained: Drain() completed. Emitted AFTER Drain (the runner
//     calls Drain to flush in-flight work before Stop).
//   - stopped: Stop() completed. Emitted AFTER Stop, last in the
//     lifecycle stream.
//
// Plan.md §Task 4 originally listed "ready" and "draining" as
// in-progress markers; kit/bus.ValidateTopic enforces past-tense
// action segments (see bus.pastTenseWhitelist + the -ed heuristic),
// so "ready" → "readied" and "draining" → "drained" align with the
// "transition completed" naming pattern used elsewhere (wsm,
// kit/runtime/state). Subscribers pattern-match on
// `dpkms.adapter.lifecycle.*` to observe every adapter's lifecycle.
func LifecycleTopic(action string) string {
	return fmt.Sprintf("dpkms.adapter.lifecycle.%s", action)
}

// EntityTopic returns the kit-conformant topic name for a per-protocol
// entity event. Topics follow kit/bus's 4-segment past-tense rule:
// dpkms.<protocol>.entity.<action>.
//
// **Reserved for observability / audit, NOT for policy gating.**
// kit/runtime/policy's `allowedTopics` only accepts the three
// `kit.runtime.*` veto-able topics (see kit/runtime/policy/config.go);
// rules with `on: dpkms.<protocol>.entity.<action>` fail at YAML
// load. Adapters that need policy-gated entity mutations route
// through domain.Service[T] (which fires `kit.runtime.entity.
// pre_persisted` natively, inheriting the gate PR #23 already wired).
//
// Use EntityTopic for post-events (persisted, failed) consumed by
// audit subscribers, federation pushers, dashboards. Pre-events
// (pre_validated, pre_persisted) are still defined here for symmetry
// but adapters should prefer the kit-namespaced topic via
// domain.Service[T] for any pre-event that needs policy gating.
//
// Allowed actions: pre_validated, pre_persisted, persisted, failed.
// See ADR-065 §Amendment-2026-05-06 §Acceptance Gate 4 for the full
// rationale.
func EntityTopic(protocol, action string) string {
	return fmt.Sprintf("dpkms.%s.entity.%s", protocol, action)
}
