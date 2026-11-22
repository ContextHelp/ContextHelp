package security

import (
	"context"
	"testing"
	"time"
)

// Every steward event fires at once, with no threshold: the alert names
// its kind, its actor and its ids, and the default handler writes an
// audit row of class security carrying them.
func TestEmitter_StewardEventsFireImmediately(t *testing.T) {
	audit := &stubAudit{}
	e := New(DefaultConfig(), audit)
	closeOnCleanup(t, e)
	fired := make(chan Alert, len(StewardEvents))
	e.AddHandler(func(_ context.Context, a Alert) { fired <- a })

	for _, kind := range StewardEvents {
		e.RecordSteward(context.Background(), kind, "prn_actor", map[string]string{"grant_id": "stg_1"})
	}
	seen := map[string]bool{}
	for range StewardEvents {
		select {
		case a := <-fired:
			seen[a.Kind] = true
			if a.Principal != "prn_actor" || a.Meta["grant_id"] != "stg_1" || a.Count != 1 || a.Message == "" {
				t.Errorf("%s: alert = %+v", a.Kind, a)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d steward alerts arrived", len(seen), len(StewardEvents))
		}
	}
	for _, kind := range StewardEvents {
		if !seen[kind] {
			t.Errorf("%s: no alert", kind)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for len(audit.snapshot()) < len(StewardEvents) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	rows := audit.snapshot()
	if len(rows) != len(StewardEvents) {
		t.Fatalf("audit rows = %d, want %d", len(rows), len(StewardEvents))
	}
	for _, r := range rows {
		if r.Payload["event_class"] != "security" || r.Payload["grant_id"] != "stg_1" || r.Actor != "prn_actor" {
			t.Errorf("%s: audit row = %+v", r.EventType, r)
		}
	}
}

// A nil emitter records nothing and does not panic: a service built
// without one still records steward transitions.
func TestEmitter_StewardOnNilIsANoOp(t *testing.T) {
	var e *Emitter
	e.RecordSteward(context.Background(), EventStewardGrantRevoked, "prn_x", nil)
}
