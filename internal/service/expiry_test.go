package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/expiry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type stubSpanLocator struct{ spans []string }

func (s stubSpanLocator) LocateSpans(_ context.Context, _, _ string) ([]string, error) {
	return s.spans, nil
}

func TestSweepExpired_WholeObjectDerivedAndHoldProtection(t *testing.T) {
	svc, drv, _ := newEraseTestService(t)
	ctx := ownerCtx()
	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-time.Hour)
	future := now.Add(24 * time.Hour)

	// 1. Expired root + derived object.
	if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "exp-root", Type: "note", RawContent: "ephemeral note", TextContent: "ephemeral note",
		Status: storage.ObjectStatusActive, ExpiresAt: &past, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "exp-derived", Type: "summary", RawContent: "derived summary", TextContent: "derived summary",
		Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := drv.Edges().Create(ctx, &storage.Edge{
		ID: "e-exp-1", FromType: "object", FromID: "exp-derived", ToType: "object", ToID: "exp-root", EdgeType: "derives_from", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	// 2. Expired object under active hold -> must NOT be erased until released.
	if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "exp-held", Type: "note", RawContent: "held capture", TextContent: "held capture",
		Status: storage.ObjectStatusActive, ExpiresAt: &past, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PlaceHold(ctx, PlaceHoldOptions{
		ObjectID: "exp-held", HoldID: "hold-exp-1", Reason: "litigation", PlacedBy: "admin",
	}); err != nil {
		t.Fatal(err)
	}

	// 3. Future expiry -> untouched.
	if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "exp-future", Type: "note", RawContent: "keep me", TextContent: "keep me",
		Status: storage.ObjectStatusActive, ExpiresAt: &future, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	// First sweep: erases exp-root + exp-derived, skips exp-held, leaves exp-future.
	res, err := svc.SweepExpired(ctx, now, 100)
	if err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}
	if res.SkippedHeld != 1 || len(res.DeletedIDs) != 2 || len(res.DerivedDeletedIDs) != 1 {
		t.Fatalf("unexpected sweep result: %+v", res)
	}
	if _, err := drv.Objects().Get(ctx, "exp-root"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("exp-root should be deleted, err=%v", err)
	}
	if _, err := drv.Objects().Get(ctx, "exp-derived"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("exp-derived should be deleted, err=%v", err)
	}
	if _, err := drv.Objects().Get(ctx, "exp-held"); err != nil {
		t.Fatalf("exp-held must survive while held: %v", err)
	}
	if _, err := drv.Objects().Get(ctx, "exp-future"); err != nil {
		t.Fatalf("exp-future must survive: %v", err)
	}

	// Release hold -> second sweep erases exp-held.
	if _, err := svc.ReleaseHold(ctx, ReleaseHoldOptions{HoldID: "hold-exp-1", ReleasedBy: "admin"}); err != nil {
		t.Fatal(err)
	}
	res2, err := svc.SweepExpired(ctx, now, 100)
	if err != nil || len(res2.DeletedIDs) != 1 || res2.DeletedIDs[0] != "exp-held" {
		t.Fatalf("second sweep after hold release = (%+v, %v)", res2, err)
	}
}

func mustCreateExpiryObj(t *testing.T, drv storage.StorageDriver, obj *storage.KnowledgeObject) {
	t.Helper()
	if err := drv.Objects().Create(ownerCtx(), obj); err != nil {
		t.Fatal(err)
	}
}

func TestSweepExpired_SelectiveDeterministicScopes(t *testing.T) {
	svc, drv, _ := newEraseTestService(t)
	ctx := ownerCtx()
	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-time.Minute)

	mustCreateExpiryObj(t, drv, &storage.KnowledgeObject{
		ID: "sel-pii", Type: "note",
		RawContent:  "Call bob@example.com at 555-333-4444 about architecture.",
		TextContent: "Call bob@example.com at 555-333-4444 about architecture.",
		Summaries:   []string{"Architecture discussion with bob@example.com"},
		Status:      storage.ObjectStatusActive, ExpiresAt: &past, ExpiryScope: "emails and phone numbers",
		CreatedAt: now, UpdatedAt: now,
	})
	mustCreateExpiryObj(t, drv, &storage.KnowledgeObject{
		ID: "sel-raw", Type: "note",
		RawContent:  "Full sensitive meeting transcript",
		TextContent: "Full sensitive meeting transcript",
		Summaries:   []string{"Decided to ship Q4"},
		Status:      storage.ObjectStatusActive, ExpiresAt: &past, ExpiryScope: "raw content, keep summary",
		CreatedAt: now, UpdatedAt: now,
	})

	res, err := svc.SweepExpired(ctx, now, 100)
	if err != nil || len(res.RedactedIDs) != 2 || res.Errors != 0 {
		t.Fatalf("unexpected selective sweep result: %+v, err=%v", res, err)
	}

	piiObj, err := drv.Objects().Get(ctx, "sel-pii")
	if err != nil || strings.Contains(piiObj.RawContent, "bob@example.com") || strings.Contains(piiObj.RawContent, "555-333-4444") || !strings.Contains(piiObj.RawContent, "architecture") || piiObj.ExpiresAt != nil {
		t.Fatalf("unexpected sel-pii: %+v, err=%v", piiObj, err)
	}
	rawObj, err := drv.Objects().Get(ctx, "sel-raw")
	if err != nil || rawObj.RawContent != "" || len(rawObj.Summaries) != 1 || rawObj.Summaries[0] != "Decided to ship Q4" {
		t.Fatalf("unexpected sel-raw: %+v, err=%v", rawObj, err)
	}
}

func TestSweepExpired_SelectiveNaturalLanguageLocator(t *testing.T) {
	svc, drv, _ := newEraseTestService(t)
	ctx := ownerCtx()
	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-time.Minute)

	mustCreateExpiryObj(t, drv, &storage.KnowledgeObject{
		ID: "sel-nl", Type: "note",
		RawContent:  "Offer is $310,000 for Project Falcon.",
		TextContent: "Offer is $310,000 for Project Falcon.",
		Status:      storage.ObjectStatusActive, ExpiresAt: &past, ExpiryScope: "compensation numbers and codenames",
		CreatedAt: now, UpdatedAt: now,
	})

	// Without SpanLocator, free-form NL fails closed and preserves ExpiresAt.
	res, err := svc.SweepExpired(ctx, now, 100)
	if err != nil || res.Errors != 1 || len(res.RedactedIDs) != 0 {
		t.Fatalf("expected fail-closed sweep without locator: %+v, err=%v", res, err)
	}
	nlObj, err := drv.Objects().Get(ctx, "sel-nl")
	if err != nil || nlObj.ExpiresAt == nil {
		t.Fatalf("sel-nl should keep ExpiresAt when locator missing: %+v, %v", nlObj, err)
	}

	// With SpanLocator installed, exact spans are redacted and ExpiresAt is cleared.
	svc.SetExpirySpanLocator(stubSpanLocator{spans: []string{"$310,000", "Project Falcon"}})
	res2, err := svc.SweepExpired(ctx, now, 100)
	if err != nil || len(res2.RedactedIDs) != 1 || res2.RedactedIDs[0] != "sel-nl" {
		t.Fatalf("second selective sweep = (%+v, %v)", res2, err)
	}
	nlObj2, err := drv.Objects().Get(ctx, "sel-nl")
	if err != nil || strings.Contains(nlObj2.RawContent, "$310,000") || strings.Contains(nlObj2.RawContent, "Project Falcon") || nlObj2.ExpiresAt != nil {
		t.Fatalf("sel-nl after locator redaction = %+v, %v", nlObj2, err)
	}
}

func TestAnalyzeRawAndInbox_PersistExpiryAndScope(t *testing.T) {
	svc, drv, _ := newEraseTestService(t)
	ctx := ownerCtx()
	exp := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)

	id, err := svc.Analyze(ctx, AnalyzeRequest{
		Content:     "temporary raw note",
		Type:        "text",
		Raw:         true,
		ExpiresAt:   &exp,
		ExpiryScope: "PII",
	})
	if err != nil {
		t.Fatalf("Analyze raw: %v", err)
	}
	got, err := drv.Objects().Get(ctx, id)
	if err != nil || got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) || got.ExpiryScope != "PII" {
		t.Fatalf("raw object expiry = (%v, %q), err=%v", got.ExpiresAt, got.ExpiryScope, err)
	}

	inboxObj, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{
		Content:     "temporary inbox note",
		Type:        "text",
		ExpiresAt:   &exp,
		ExpiryScope: "raw content, keep summary",
	})
	if err != nil {
		t.Fatalf("CaptureToInbox: %v", err)
	}
	gotInbox, err := drv.Objects().Get(ctx, inboxObj.ID)
	if err != nil || gotInbox.ExpiresAt == nil || !gotInbox.ExpiresAt.Equal(exp) || gotInbox.ExpiryScope != "raw content, keep summary" {
		t.Fatalf("inbox object expiry = (%v, %q), err=%v", gotInbox.ExpiresAt, gotInbox.ExpiryScope, err)
	}

	// Ensure expiry.ErrScopeUnresolved is exported and testable.
	_ = expiry.ErrScopeUnresolved
}
