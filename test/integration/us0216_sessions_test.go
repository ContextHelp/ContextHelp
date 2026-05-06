package integration

// US-0216: Work Sessions (per ADR-067 + T-0505).
//
// E2E acceptance criteria from docs/stories/capture/US-0216-work-sessions.md.
// Verifies: client-side three-rule cutter cuts sessions correctly under
// real ambient-event load, soft-FK semantics in dpkms storage absorb
// network-loss replay ordering, idempotent PUT /api/v1/sessions/{id},
// ctxt session list/show CLI works, ctxt compose --session scopes
// correctly.
//
// **Status: skeleton.** Cutter (T-0505) is implemented + unit-tested
// (94% coverage) with a fakeClock. dpkms-side schema migration
// (sessions table + objects.session_id soft-FK) is tracked separately;
// E2E harness needs both pieces to verify full round-trip.

import (
	"testing"
)

func TestCutter_IdleHardCut(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — cutter is unit-tested with fakeClock " +
		"in internal/ambient/session/cutter_test.go::TestCutter_HardCutOnIdleGap; " +
		"E2E verifies real-clock idle cut + bus event emission to subscriber")
}

func TestCutter_SoftCutWithFrequentSwitchingException(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — frequent-switching exception is " +
		"unit-tested; E2E with real foreground-source events")
}

func TestCutter_HardTimeout(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — fake-clock unit test passes; long-running " +
		"E2E (2h+) needs CI accommodation or a clock-skew helper")
}

func TestCutter_ForceEndOnShutdown(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — ForceEnd is unit-tested; E2E with real " +
		"ctxd shutdown signal (SIGTERM)")
}

func TestCutter_DailySafetyNet(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: daily-safety-net cron wiring in cmd/ctxd (not yet built); " +
		"the cutter exposes ForceEnd(EndDailySafetyNet) which is unit-tested")
}

func TestSession_SoftFKReplay(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: dpkms-side schema migration (sessions table + " +
		"objects.session_id column with soft-FK semantics, per ADR-067 " +
		"§Wire shape). Substrate enqueue path emits SessionID; dpkms " +
		"acceptance is the missing piece")
}

func TestSession_PutIdempotent(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: dpkms-side /api/v1/sessions/{id} endpoint (PUT idempotent " +
		"per ADR-067)")
}

func TestComposeBySession(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: dpkms-side ObjectFilter.SessionID field. CLI flag " +
		"ctxt compose --session is plumbed (T-0508) as forward-compat no-op; " +
		"activates when storage migration ships")
}

func TestSessionListAndShow(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: ctxt session list/show CLI commands (planned per US-0216 " +
		"acceptance; not yet built — the cutter is the substrate, the CLI is " +
		"the user surface)")
}
