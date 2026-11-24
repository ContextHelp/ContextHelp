package expiry

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseTTLAndExpiry(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	d, err := ParseTTL("7d")
	if err != nil || d != 7*24*time.Hour {
		t.Fatalf("ParseTTL(7d) = (%v, %v)", d, err)
	}
	d, err = ParseTTL("2w")
	if err != nil || d != 14*24*time.Hour {
		t.Fatalf("ParseTTL(2w) = (%v, %v)", d, err)
	}
	d, err = ParseTTL("1d12h")
	if err != nil || d != 36*time.Hour {
		t.Fatalf("ParseTTL(1d12h) = (%v, %v)", d, err)
	}
	if _, err := ParseTTL("0s"); err == nil {
		t.Fatal("expected error for 0s ttl")
	}

	at, err := ParseExpiry("2026-12-31T23:59:59Z", now)
	if err != nil || at.Format(time.RFC3339) != "2026-12-31T23:59:59Z" {
		t.Fatalf("ParseExpiry RFC3339 = (%v, %v)", at, err)
	}
	at, err = ParseExpiry("in 3d", now)
	if err != nil || !at.Equal(now.Add(72*time.Hour)) {
		t.Fatalf("ParseExpiry(in 3d) = (%v, %v)", at, err)
	}
}

type fakeLocator struct{ spans []string }

func (f fakeLocator) LocateSpans(_ context.Context, _, _ string) ([]string, error) {
	return f.spans, nil
}

func TestClassifyAndRedactScope(t *testing.T) {
	ctx := context.Background()

	// Deterministic PII redaction (emails + phone numbers).
	plan := ClassifyScope("emails and phone numbers")
	if plan.NeedsLLM {
		t.Fatal("expected deterministic plan for emails and phone numbers")
	}
	out, n, err := RedactText(ctx, "Contact alice@example.com or 555-234-5678 for details.", "emails and phone numbers", "[EXPIRED]", plan, nil)
	if err != nil || n != 2 {
		t.Fatalf("RedactText = (%q, %d, %v)", out, n, err)
	}
	if strings.Contains(out, "alice@example.com") || strings.Contains(out, "555-234-5678") {
		t.Fatalf("sensitive spans remained: %q", out)
	}

	// Free-form NL without SpanLocator fails closed.
	nlPlan := ClassifyScope("salary figures and client codenames")
	if !nlPlan.NeedsLLM {
		t.Fatal("expected NeedsLLM for free-form scope")
	}
	if _, _, err := RedactText(ctx, "Salary $250,000 for Project Orion.", "salary figures and client codenames", "[EXPIRED]", nlPlan, nil); !errors.Is(err, ErrScopeUnresolved) {
		t.Fatalf("expected ErrScopeUnresolved without locator, got %v", err)
	}

	// Free-form NL with SpanLocator redacts exact spans.
	out, n, err = RedactText(ctx, "Salary $250,000 for Project Orion.", "salary figures and client codenames", "[EXPIRED]", nlPlan, fakeLocator{spans: []string{"$250,000", "Project Orion"}})
	if err != nil || n != 2 || strings.Contains(out, "$250,000") || strings.Contains(out, "Project Orion") {
		t.Fatalf("NL RedactText = (%q, %d, %v)", out, n, err)
	}
}
