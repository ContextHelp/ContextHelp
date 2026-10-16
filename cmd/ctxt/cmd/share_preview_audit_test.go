package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// seedPreviewContact stores a work contact with details in metadata,
// as cardamum ingests one.
func seedPreviewContact(t *testing.T, db *testDB) string {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	o := &storage.KnowledgeObject{
		ID: "preview-contact", Type: projection.TypeContact, Status: storage.ObjectStatusActive,
		RawContent: "Sarah Lee | Head of Ops @ Acme", ContentHash: "sha256:preview-contact", ProfileID: "work",
		Metadata:  map[string]any{"name": "Sarah Lee", "org": "Acme", "role": "Head of Ops", "email": "sarah.lee@acme.example", "phone": "+1 555 0100 2001"},
		Tags:      []storage.Tag{{Label: "work"}},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	return o.ID
}

// The owner previews a policy's view of a contact: the card without
// the details; a command that writes is refused; the preview lands in
// the read audit as the owner's; --as on a plain command does the same.
func TestSharePreviewAndAudit(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	id := seedPreviewContact(t, db)
	if out, err := db.exec("share", "template", "install", sharing.TemplateColleagues); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}

	out, err := db.exec("share", "preview", "--as", sharing.TemplateColleagues, "show", id, "--format", "json")
	if err != nil {
		t.Fatalf("preview show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Sarah Lee") || strings.Contains(out, "sarah.lee@acme.example") || strings.Contains(out, "+1 555 0100 2001") {
		t.Errorf("preview show = %s", out)
	}
	var shown storage.KnowledgeObject
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if _, has := shown.Metadata["email"]; has {
		t.Errorf("email reached the preview: %v", shown.Metadata)
	}

	out, err = db.exec("--as", sharing.TemplateColleagues, "show", id, "--format", "json")
	if err != nil {
		t.Fatalf("--as show: %v\n%s", err, out)
	}
	if strings.Contains(out, "sarah.lee@acme.example") {
		t.Errorf("--as show leaked the email: %s", out)
	}
	if out, err := db.exec("show", id, "--format", "json"); err != nil || !strings.Contains(out, "sarah.lee@acme.example") {
		t.Errorf("the owner's own show lost the email: %v\n%s", err, out)
	}

	if out, err := db.exec("share", "preview", "--as", sharing.TemplateColleagues, "delete", id); err == nil {
		t.Fatalf("a destructive command was previewed:\n%s", out)
	} else if !strings.Contains(err.Error(), "not a read command") {
		t.Errorf("refusal = %v", err)
	}
	if _, err := db.exec("share", "preview", "--as", "nobody-here", "show", id); err == nil {
		t.Fatal("an unknown viewer was accepted")
	}
	if _, err := db.exec("share", "preview", "show", id); err == nil {
		t.Fatal("a preview without --as ran")
	}

	if err := db.Server.Stack.Audit.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	out, err = db.exec("share", "audit", "--principal", storage.OwnerPrincipal, "--format", "json")
	if err != nil {
		t.Fatalf("audit: %v\n%s", err, out)
	}
	var listed struct {
		Entries []storage.AccessEntry `json:"entries"`
		Total   int                   `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if listed.Total < 2 {
		t.Fatalf("audit lists %d entries, want the two previews:\n%s", listed.Total, out)
	}
	for _, e := range listed.Entries {
		if !strings.HasPrefix(e.PolicyVersions, "preview="+sharing.TemplateColleagues+";") {
			t.Errorf("entry %+v is not marked a preview", e)
		}
	}
	out, err = db.exec("share", "audit", "--object", id)
	if err != nil {
		t.Fatalf("audit by object: %v\n%s", err, out)
	}
	if !strings.Contains(out, storage.OwnerPrincipal) || !strings.Contains(out, "GET /objects/{id}") {
		t.Errorf("audit table = %s", out)
	}
	if _, err := db.exec("share", "audit"); err == nil {
		t.Fatal("audit without a filter ran")
	}
}

// A reader cannot preview: the instance refuses the header.
func TestSharePreviewIsOwnerOnly(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	id := seedPreviewContact(t, db)
	db.useRole(t, dpkmstest.RoleReader)
	out, err := db.exec("--as", dpkmstest.PrincipalWriter, "show", id)
	if err == nil {
		t.Fatalf("a reader previewed another principal:\n%s", out)
	}
	if !strings.Contains(err.Error(), "VIEW_AS_OWNER_ONLY") && !strings.Contains(err.Error(), "403") {
		t.Errorf("refusal = %v", err)
	}
}
