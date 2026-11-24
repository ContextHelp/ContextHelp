package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/keys"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

type stubEncryptLocator struct{ spans []string }

func (s stubEncryptLocator) LocateSpans(_ context.Context, _, _ string) ([]string, error) {
	return s.spans, nil
}

func newEncryptTestService(t *testing.T, kp keys.Provider) (*Service, *sqlite.Driver) {
	t.Helper()
	ctx := context.Background()
	bs, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	drv, err := sqlite.New(":memory:", bs)
	if err != nil {
		t.Fatal(err)
	}
	if err := drv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = drv.Close(ctx) })
	var opts []Option
	if kp != nil {
		opts = append(opts, WithKeyProvider(kp))
	}
	return NewWithOptions(drv, nil, nil, nil, "", nil, opts, config.Config{}), drv
}

func TestPerCaptureEncryption_WholeAndFailClosed(t *testing.T) {
	ctx := ownerCtx()

	svcNoKey, _ := newEncryptTestService(t, nil)
	if _, err := svcNoKey.Analyze(ctx, AnalyzeRequest{
		Content: "secret body", Type: "text", Raw: true, Encrypt: true,
	}); !errors.Is(err, keys.ErrUnavailable) {
		t.Fatalf("Analyze without keyProvider err = %v, want ErrUnavailable", err)
	}

	kp, err := keys.NewMemory("k1")
	if err != nil {
		t.Fatal(err)
	}
	svc, drv := newEncryptTestService(t, kp)

	const wholeBody = "confidential board discussion notes for q4"
	id, err := svc.Analyze(ctx, AnalyzeRequest{
		Content: wholeBody, Type: "text", Raw: true, Encrypt: true,
	})
	if err != nil {
		t.Fatalf("Analyze whole encrypt: %v", err)
	}
	rawRow, err := drv.Objects().Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !rawRow.Encrypted || strings.Contains(rawRow.RawContent, wholeBody) || strings.Contains(rawRow.TextContent, wholeBody) {
		t.Fatalf("whole encrypted row leaked plaintext: %+v", rawRow)
	}
	got, err := svc.GetObject(ctx, id)
	if err != nil {
		t.Fatalf("GetObject decrypted: %v", err)
	}
	if got.RawContent != wholeBody || got.TextContent != wholeBody {
		t.Fatalf("GetObject decrypted = (%q, %q), want %q", got.RawContent, got.TextContent, wholeBody)
	}

	wrongKP, _ := keys.NewMemory("k1")
	if err := DecryptObject(ctx, wrongKP, rawRow); !errors.Is(err, keys.ErrUnwrap) {
		t.Fatalf("wrong key DecryptObject err = %v, want ErrUnwrap", err)
	}
}

func TestPerCaptureEncryption_SelectiveAndNaturalLanguageScope(t *testing.T) {
	ctx := ownerCtx()
	kp, err := keys.NewMemory("k1")
	if err != nil {
		t.Fatal(err)
	}
	svc, drv := newEncryptTestService(t, kp)

	// 1. Deterministic PII scope seals email + phone inline, leaves topic searchable.
	inboxObj, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{
		Content:      "Discuss roadmap with carol@example.com at 555-777-8888",
		Type:         "text",
		EncryptScope: "emails and phone numbers",
	})
	if err != nil {
		t.Fatalf("CaptureToInbox selective: %v", err)
	}
	rawInbox, err := drv.Objects().Get(ctx, inboxObj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rawInbox.Encrypted || rawInbox.EncryptScope != "emails and phone numbers" ||
		strings.Contains(rawInbox.RawContent, "carol@example.com") ||
		strings.Contains(rawInbox.RawContent, "555-777-8888") ||
		!strings.Contains(rawInbox.RawContent, "roadmap") {
		t.Fatalf("unexpected selective stored row: %q", rawInbox.RawContent)
	}
	decInbox, err := svc.GetObject(ctx, inboxObj.ID)
	if err != nil {
		t.Fatalf("GetObject selective: %v", err)
	}
	if decInbox.RawContent != "Discuss roadmap with carol@example.com at 555-777-8888" {
		t.Fatalf("GetObject selective decrypted = %q", decInbox.RawContent)
	}

	// 2. Free-form NL scope fails closed without locator, seals exact spans with locator.
	if _, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{
		Content:      "Salary $295,000 for Project Nova",
		Type:         "text",
		EncryptScope: "salary figures and project codenames",
	}); !errors.Is(err, keys.ErrEncryptScopeUnresolved) {
		t.Fatalf("expected ErrEncryptScopeUnresolved without locator, got %v", err)
	}
	svc.SetEncryptSpanLocator(stubEncryptLocator{spans: []string{"$295,000", "Project Nova"}})
	nlObj, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{
		Content:      "Salary $295,000 for Project Nova",
		Type:         "text",
		EncryptScope: "salary figures and project codenames",
	})
	if err != nil {
		t.Fatalf("CaptureToInbox NL locator: %v", err)
	}
	rawNL, err := drv.Objects().Get(ctx, nlObj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rawNL.RawContent, "$295,000") || strings.Contains(rawNL.RawContent, "Project Nova") || !strings.Contains(rawNL.RawContent, "Salary") {
		t.Fatalf("unexpected NL sealed row: %q", rawNL.RawContent)
	}
	decNL, err := svc.GetObject(ctx, nlObj.ID)
	if err != nil {
		t.Fatalf("GetObject NL: %v", err)
	}
	if decNL.RawContent != "Salary $295,000 for Project Nova" {
		t.Fatalf("GetObject NL decrypted = %q", decNL.RawContent)
	}
}
