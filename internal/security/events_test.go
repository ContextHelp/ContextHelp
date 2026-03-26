package security

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// --- stub AuditStore ---

type stubAudit struct {
	mu      sync.Mutex
	entries []*storage.AuditEntry
}

func (s *stubAudit) Append(_ context.Context, e *storage.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	return nil
}

func (s *stubAudit) List(_ context.Context, _ storage.AuditFilter) ([]*storage.AuditEntry, int, error) {
	return nil, 0, nil
}

func (s *stubAudit) GetObjectHistory(_ context.Context, _ string) ([]*storage.AuditEntry, error) {
	return nil, nil
}

func (s *stubAudit) snapshot() []*storage.AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*storage.AuditEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// --- sliding counter tests ---

func TestSlidingCounter_WithinWindow(t *testing.T) {
	c := newSlidingCounter(60 * time.Second)
	now := time.Now()
	for i := 0; i < 5; i++ {
		c.Add(now)
	}
	if got := c.Count(now); got != 5 {
		t.Fatalf("want 5, got %d", got)
	}
}

func TestSlidingCounter_EvictsOldEntries(t *testing.T) {
	c := newSlidingCounter(60 * time.Second)
	old := time.Now().Add(-90 * time.Second)
	c.Add(old)
	c.Add(old)

	now := time.Now()
	c.Add(now)
	if got := c.Count(now); got != 1 {
		t.Fatalf("want 1 (only recent), got %d", got)
	}
}

func TestSlidingCounter_AddReturnsCurrentCount(t *testing.T) {
	c := newSlidingCounter(60 * time.Second)
	now := time.Now()
	for i := 1; i <= 4; i++ {
		got := c.Add(now)
		if got != i {
			t.Fatalf("iteration %d: want %d, got %d", i, i, got)
		}
	}
}

// --- threshold detection tests ---

func TestEmitter_AuthFailure_BelowThreshold_NoAlert(t *testing.T) {
	audit := &stubAudit{}
	cfg := DefaultConfig()
	cfg.AuthFailureThreshold = 3

	var fired atomic.Bool
	e := New(cfg, audit)
	e.AddHandler(func(_ context.Context, _ Alert) { fired.Store(true) })

	ctx := context.Background()
	e.RecordAuthFailure(ctx, "user:alice")
	e.RecordAuthFailure(ctx, "user:alice")
	// 2 < threshold 3 — no alert yet

	time.Sleep(20 * time.Millisecond) // let goroutines settle
	if fired.Load() {
		t.Fatal("alert should not fire below threshold")
	}
	if len(audit.snapshot()) != 0 {
		t.Fatal("audit should be empty below threshold")
	}
}

func TestEmitter_AuthFailure_AtThreshold_Fires(t *testing.T) {
	audit := &stubAudit{}
	cfg := DefaultConfig()
	cfg.AuthFailureThreshold = 3

	var mu sync.Mutex
	var alerts []Alert
	e := New(cfg, audit)
	e.AddHandler(func(_ context.Context, a Alert) {
		mu.Lock()
		alerts = append(alerts, a)
		mu.Unlock()
	})

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		e.RecordAuthFailure(ctx, "user:bob")
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	got := len(alerts)
	mu.Unlock()
	if got == 0 {
		t.Fatal("expected alert at threshold")
	}
	if alerts[0].Kind != EventAuthFailure {
		t.Fatalf("wrong kind: %s", alerts[0].Kind)
	}
}

func TestEmitter_ACLDenial_AtThreshold_Fires(t *testing.T) {
	audit := &stubAudit{}
	cfg := DefaultConfig()
	cfg.ACLDenialThreshold = 5

	fired := make(chan Alert, 10)
	e := New(cfg, audit)
	e.AddHandler(func(_ context.Context, a Alert) {
		fired <- a
	})

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		e.RecordACLDenial(ctx, "agent:analyst")
	}

	select {
	case a := <-fired:
		if a.Kind != EventACLDenial {
			t.Fatalf("wrong kind: %s", a.Kind)
		}
		if a.Principal != "agent:analyst" {
			t.Fatalf("wrong principal: %s", a.Principal)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("alert not received within timeout")
	}
}

func TestEmitter_QuotaExhausted_ImmediateFire(t *testing.T) {
	audit := &stubAudit{}
	cfg := DefaultConfig()

	fired := make(chan Alert, 1)
	e := New(cfg, audit)
	e.AddHandler(func(_ context.Context, a Alert) { fired <- a })

	e.RecordQuotaExhausted(context.Background(), "user:carol", "stripe-registry")

	select {
	case a := <-fired:
		if a.Kind != EventQuotaExhausted {
			t.Fatalf("wrong kind: %s", a.Kind)
		}
		if a.Meta["registry"] != "stripe-registry" {
			t.Fatalf("wrong registry meta: %v", a.Meta)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("alert not received within timeout")
	}
}

func TestEmitter_SigningFailure_ImmediateFire(t *testing.T) {
	audit := &stubAudit{}
	cfg := DefaultConfig()

	fired := make(chan Alert, 1)
	e := New(cfg, audit)
	e.AddHandler(func(_ context.Context, a Alert) { fired <- a })

	e.RecordSigningFailure(context.Background(), "bundle.sig mismatch")

	select {
	case a := <-fired:
		if a.Kind != EventSigningFailure {
			t.Fatalf("wrong kind: %s", a.Kind)
		}
		if a.Level != AlertLevelCritical {
			t.Fatalf("wrong level: %s", a.Level)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("alert not received within timeout")
	}
}

func TestEmitter_WorkerCrashLoop_ImmediateFire(t *testing.T) {
	audit := &stubAudit{}
	cfg := DefaultConfig()

	fired := make(chan Alert, 1)
	e := New(cfg, audit)
	e.AddHandler(func(_ context.Context, a Alert) { fired <- a })

	e.RecordWorkerCrashLoop(context.Background(), "worker-1", "panic: nil pointer")

	select {
	case a := <-fired:
		if a.Kind != EventWorkerCrashLoop {
			t.Fatalf("wrong kind: %s", a.Kind)
		}
		if a.Meta["worker_id"] != "worker-1" {
			t.Fatalf("wrong worker_id: %v", a.Meta)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("alert not received within timeout")
	}
}

// --- audit log handler test ---

func TestEmitter_DefaultHandler_WritesAuditEntry(t *testing.T) {
	audit := &stubAudit{}
	cfg := DefaultConfig()
	cfg.AuthFailureThreshold = 1

	e := New(cfg, audit)
	e.RecordAuthFailure(context.Background(), "user:dan")

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(audit.snapshot()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	entries := audit.snapshot()
	if len(entries) == 0 {
		t.Fatal("expected audit entry from default handler")
	}
	entry := entries[0]
	if entry.EventType != EventAuthFailure {
		t.Fatalf("wrong event_type: %s", entry.EventType)
	}
	if entry.Payload["event_class"] != "security" {
		t.Fatalf("expected event_class=security in payload: %v", entry.Payload)
	}
}

// --- webhook handler test ---

func TestWebhookHandler_PostsJSON(t *testing.T) {
	var received []byte
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		mu.Lock()
		received = buf
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.WebhookURL = srv.URL
	cfg.AuthFailureThreshold = 1

	e := New(cfg, nil)
	e.RecordAuthFailure(context.Background(), "user:eve")

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := len(received) > 0
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	body := received
	mu.Unlock()

	if len(body) == 0 {
		t.Fatal("webhook not called")
	}
	var a Alert
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatalf("unmarshal webhook body: %v", err)
	}
	if a.Kind != EventAuthFailure {
		t.Fatalf("wrong kind in webhook payload: %s", a.Kind)
	}
}

// --- isolation: separate principals have independent counters ---

func TestEmitter_DifferentPrincipals_Independent(t *testing.T) {
	audit := &stubAudit{}
	cfg := DefaultConfig()
	cfg.AuthFailureThreshold = 3

	var mu sync.Mutex
	var alerts []Alert
	e := New(cfg, audit)
	e.AddHandler(func(_ context.Context, a Alert) {
		mu.Lock()
		alerts = append(alerts, a)
		mu.Unlock()
	})

	ctx := context.Background()
	// alice hits threshold
	for i := 0; i < 3; i++ {
		e.RecordAuthFailure(ctx, "user:alice")
	}
	// bob only 1 — should NOT fire
	e.RecordAuthFailure(ctx, "user:bob")

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	for _, a := range alerts {
		if a.Principal == "user:bob" {
			t.Fatal("bob should not trigger an alert")
		}
	}
}
