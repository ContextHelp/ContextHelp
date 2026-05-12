// Package eval provides offline evaluation of the lateral discovery
// substrate. It replays JSONL fixtures of historical CapturedEvents
// through a configured registry, captures emitted candidates, and
// compares them against labelled expectations.
//
// The harness has three responsibilities:
//
//  1. Replay (replay.go): read fixtures, dispatch through the registry,
//     capture candidates + errors per event. The replay output is a
//     ReplayReport — the input to all metric calculation.
//  2. Metrics (metrics.go): compute precision and recall per strategy
//     against labelled fixtures.
//  3. Conversion (conversion.go): subscribe to the live bus and track
//     candidate→promotion rates per strategy over a configurable window.
//
// The CLI surface is `ctxt lateral eval replay <fixtures.jsonl>` and
// `ctxt lateral eval metrics <fixtures.jsonl>` (cmd/ctxt/cmd/
// lateral_eval.go). Both commands are also consumed by the CI workflow
// (.github/workflows/lateral-eval.yml) to gate PR merges on regression.
//
// # Fixture format
//
// One JSON object per line. Each object has two top-level keys:
//
//	{"event": {<lateral.CapturedEvent fields>}, "expect": {<expectation>}}
//
// The expectation block (FixtureExpectation) carries the strategy ID
// the operator labelled the event with, the minimum candidate count,
// and the expected canonical URLs. See FixtureExpectation for the
// full shape.
//
// # Why offline
//
// The lateral pipeline is event-driven and stateful (caches, breakers,
// floor trackers). Offline replay isolates strategy behaviour from
// runtime variability so PR-time eval is deterministic.
package eval
