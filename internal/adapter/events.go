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
// Allowed actions: pre_validated, pre_persisted, persisted, failed.
// pre_* actions are veto-able by kit/runtime/policy via the existing
// internal/policy bootstrap; CEL rules subscribed to these topics
// can return PolicyDeniedError to abort an in-flight mutation.
func EntityTopic(protocol, action string) string {
	return fmt.Sprintf("dpkms.%s.entity.%s", protocol, action)
}
