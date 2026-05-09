// Package events enumerates the bus topics emitted by the lateral
// substrate pipeline plus the typed payload structs that downstream
// notify.FilterSink chains use to route by severity.
//
// The 26 topic vars below mirror schemas/lateral_events.json (the
// authoritative wire schema). They are vars rather than consts because
// bus.TopicOf(...).Action(...) panics on validation failure and is
// therefore not a constant expression — drift between catalog and
// schema surfaces as a panic at process init, not a silent mismatch
// at publish time.
package events

import "hop.top/kit/go/runtime/bus"

// Lateral-substrate bus topic vars. Wire form per the kit extended
// notation: <Source>.<Category>.<Object>[_<modifier>].<Action>. All
// constructed via bus.TopicOf so kit validates them at process init —
// any drift surfaces as a panic at startup, not a silent mismatch at
// publish time. Schema definitions live in schemas/lateral_events.json.
var (
	ScanStarted   = bus.TopicOf("ctxt", "lateral", "scan").Action("started")
	ScanCompleted = bus.TopicOf("ctxt", "lateral", "scan").Action("completed")
	ScanDeferred  = bus.TopicOf("ctxt", "lateral", "scan").Action("deferred")
	ScanSkipped   = bus.TopicOf("ctxt", "lateral", "scan").Action("skipped")
	ScanFailed    = bus.TopicOf("ctxt", "lateral", "scan").Action("failed")

	SubpathFailed = bus.TopicOf("ctxt", "lateral", "subpath").Action("failed")

	CandidateCreated     = bus.TopicOf("ctxt", "lateral", "candidate").Action("created")
	CandidatePromoted    = bus.TopicOf("ctxt", "lateral", "candidate").Action("promoted")
	CandidateExpired     = bus.TopicOf("ctxt", "lateral", "candidate").Action("expired")
	CandidateRejected    = bus.TopicOf("ctxt", "lateral", "candidate").Action("rejected")
	CandidateResurrected = bus.TopicOf("ctxt", "lateral", "candidate").Action("resurrected")

	ReaperCycleStarted   = bus.TopicOf("ctxt", "lateral", "reaper").Mod("cycle").Action("started")
	ReaperCycleCompleted = bus.TopicOf("ctxt", "lateral", "reaper").Mod("cycle").Action("completed")
	ReaperCycleFailed    = bus.TopicOf("ctxt", "lateral", "reaper").Mod("cycle").Action("failed")
	ReaperCycleSkipped   = bus.TopicOf("ctxt", "lateral", "reaper").Mod("cycle").Action("skipped")

	SanityCheckViolated = bus.TopicOf("ctxt", "lateral", "sanity").Mod("check").Action("violated")

	ScoreFlagged      = bus.TopicOf("ctxt", "lateral", "score").Action("flagged")
	SignalDegraded    = bus.TopicOf("ctxt", "lateral", "signal").Action("degraded")
	ResolutionFlagged = bus.TopicOf("ctxt", "lateral", "resolution").Action("flagged")

	RecipeRefreshed = bus.TopicOf("ctxt", "lateral", "recipe").Action("refreshed")
	RecipeServed    = bus.TopicOf("ctxt", "lateral", "recipe").Action("served")

	ClassificationMatched    = bus.TopicOf("ctxt", "lateral", "classification").Action("matched")
	ClassificationClassified = bus.TopicOf("ctxt", "lateral", "classification").Action("classified")
	ClassificationCached     = bus.TopicOf("ctxt", "lateral", "classification").Action("cached")

	ResearchIntentBoosted    = bus.TopicOf("ctxt", "lateral", "research").Mod("intent").Action("boosted")
	ResearchIntentSuppressed = bus.TopicOf("ctxt", "lateral", "research").Mod("intent").Action("suppressed")
)
