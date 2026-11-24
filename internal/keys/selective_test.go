package keys

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubEncryptLocator struct{ spans []string }

func (s stubEncryptLocator) LocateSpans(_ context.Context, _, _ string) ([]string, error) {
	return s.spans, nil
}

func TestSelectiveEncryption_WholeField(t *testing.T) {
	ctx := context.Background()
	p, err := NewMemory("k1")
	if err != nil {
		t.Fatal(err)
	}
	wrongP, err := NewMemory("k1")
	if err != nil {
		t.Fatal(err)
	}
	fieldAAD := []byte("object:o1:raw_content")
	spanAAD := []byte("object:o1:raw_content:span")

	sealed, err := SealFieldText(ctx, p, "top secret capture body", fieldAAD)
	if err != nil {
		t.Fatalf("SealFieldText: %v", err)
	}
	if !strings.HasPrefix(sealed, FieldCiphertextPrefix) || strings.Contains(sealed, "top secret") {
		t.Fatalf("sealed field leaked plaintext: %q", sealed)
	}
	if _, err := DecryptFieldOrSpans(ctx, wrongP, sealed, fieldAAD, spanAAD); !errors.Is(err, ErrUnwrap) {
		t.Fatalf("wrong key decrypt err = %v, want ErrUnwrap", err)
	}
	plain, err := DecryptFieldOrSpans(ctx, p, sealed, fieldAAD, spanAAD)
	if err != nil || plain != "top secret capture body" {
		t.Fatalf("DecryptFieldOrSpans = (%q, %v)", plain, err)
	}
}

func TestSelectiveEncryption_DeterministicPII(t *testing.T) {
	ctx := context.Background()
	p, err := NewMemory("k1")
	if err != nil {
		t.Fatal(err)
	}
	fieldAAD := []byte("object:o1:raw_content")
	spanAAD := []byte("object:o1:raw_content:span")

	plan := ClassifyEncryptScope("emails and phone numbers")
	if plan.WholeContent || plan.NeedsLLM {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	orig := "Meeting with alice@example.com (555-867-5309) about roadmap."
	sel, count, err := SealSelectiveText(ctx, p, orig, "emails and phone numbers", spanAAD, plan, nil)
	if err != nil || count != 2 {
		t.Fatalf("SealSelectiveText = (%q, %d, %v)", sel, count, err)
	}
	if strings.Contains(sel, "alice@example.com") || strings.Contains(sel, "555-867-5309") {
		t.Fatalf("PII leaked in selective ciphertext: %q", sel)
	}
	if !strings.Contains(sel, "Meeting with") || !strings.Contains(sel, "about roadmap.") {
		t.Fatalf("non-sensitive context lost: %q", sel)
	}
	dec, err := DecryptFieldOrSpans(ctx, p, sel, fieldAAD, spanAAD)
	if err != nil || dec != orig {
		t.Fatalf("DecryptFieldOrSpans selective = (%q, %v), want %q", dec, err, orig)
	}
}

func TestSelectiveEncryption_NaturalLanguageLocator(t *testing.T) {
	ctx := context.Background()
	p, err := NewMemory("k1")
	if err != nil {
		t.Fatal(err)
	}
	fieldAAD := []byte("object:o1:raw_content")
	spanAAD := []byte("object:o1:raw_content:span")

	nlPlan := ClassifyEncryptScope("salary figures and client codenames")
	if !nlPlan.NeedsLLM {
		t.Fatal("expected NeedsLLM for free-form scope")
	}
	nlOrig := "Approved $420,000 budget for Project Apollo."
	if _, _, err := SealSelectiveText(ctx, p, nlOrig, "salary figures and client codenames", spanAAD, nlPlan, nil); !errors.Is(err, ErrEncryptScopeUnresolved) {
		t.Fatalf("expected ErrEncryptScopeUnresolved, got %v", err)
	}
	nlSel, n, err := SealSelectiveText(ctx, p, nlOrig, "salary figures and client codenames", spanAAD, nlPlan, stubEncryptLocator{spans: []string{"$420,000", "Project Apollo"}})
	if err != nil || n != 2 || strings.Contains(nlSel, "$420,000") || strings.Contains(nlSel, "Project Apollo") || !strings.Contains(nlSel, "Approved") {
		t.Fatalf("NL selective encrypt = (%q, %d, %v)", nlSel, n, err)
	}
	nlDec, err := DecryptFieldOrSpans(ctx, p, nlSel, fieldAAD, spanAAD)
	if err != nil || nlDec != nlOrig {
		t.Fatalf("NL selective decrypt = (%q, %v)", nlDec, err)
	}
}
