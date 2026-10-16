package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The audit lists who was refused and why: a reader's refused calls are
// entries with the decision and the failing check, in the table, the
// check trail and the JSON.
func TestShareAuditShowsDecisionAndReasons(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	id := seedPreviewContact(t, db)

	db.useRole(t, dpkmstest.RoleReader)
	if out, err := db.exec("--as", dpkmstest.PrincipalWriter, "show", id); err == nil {
		t.Fatalf("a reader previewed another principal:\n%s", out)
	}
	if out, err := db.exec("share", "audit", "--decision", "denied"); err == nil {
		t.Fatalf("a reader read the audit:\n%s", out)
	}
	if out, err := db.exec("show", id, "--format", "json"); err != nil {
		t.Fatalf("the reader's own read: %v\n%s", err, out)
	}

	db.useRole(t, dpkmstest.RoleAdmin)
	if err := db.Server.Stack.Audit.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	out, err := db.exec("share", "audit", "--decision", "denied")
	if err != nil {
		t.Fatalf("audit --decision denied: %v\n%s", err, out)
	}
	for _, want := range []string{"DECISION", "WHY", "denied", "view_as: owner_only", "scope: scope_missing", dpkmstest.PrincipalReader} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "allowed") {
		t.Errorf("--decision denied listed an allowed entry:\n%s", out)
	}

	out, err = db.exec("share", "audit", "--principal", dpkmstest.PrincipalReader, "--checks")
	if err != nil {
		t.Fatalf("audit --checks: %v\n%s", err, out)
	}
	for _, want := range []string{"view_as", "FAIL", "owner_only", "scope", "scope_missing", "PASS", "authenticated", "digest v1:sha256:"} {
		if !strings.Contains(out, want) {
			t.Errorf("--checks lacks %q:\n%s", want, out)
		}
	}

	out, err = db.exec("share", "audit", "--principal", dpkmstest.PrincipalReader, "--format", "json")
	if err != nil {
		t.Fatalf("audit json: %v\n%s", err, out)
	}
	var listed struct {
		Entries []storage.AccessEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	decisions := map[storage.AccessDecision]int{}
	for _, e := range listed.Entries {
		decisions[e.Decision]++
		if e.OperationDigest == "" || len(e.Checks) == 0 {
			t.Errorf("entry without a receipt: %+v", e)
		}
	}
	if decisions[storage.AccessDecisionDenied] < 2 || decisions[storage.AccessDecisionAllowed] < 1 {
		t.Errorf("decisions = %v, want the two refusals and the allowed read", decisions)
	}

	if _, err := db.exec("share", "audit", "--decision", "maybe"); err == nil {
		t.Error("an unknown decision was accepted")
	}
}

// failNth fails the nth write only.
type failNth struct{ n, writes int }

func (f *failNth) Write(p []byte) (int, error) {
	f.writes++
	if f.writes == f.n {
		return 0, errors.New("write failed")
	}
	return len(p), nil
}

// A failed write anywhere in the check listing is returned, not
// swallowed by the writes after it.
func TestRenderAuditChecksReturnsWriteErrors(t *testing.T) {
	entries := []storage.AccessEntry{{
		PrincipalID: "prn_a", Route: "GET /objects", Decision: storage.AccessDecisionAllowed,
		Checks:          []storage.AccessCheck{{Check: "scope", Outcome: storage.AccessOutcomePass, Reason: "scope_held"}},
		OperationDigest: storage.DigestPrefix + "00",
	}}
	for n := 1; n <= 3; n++ { // header, one check, digest
		if err := renderAuditChecks(&failNth{n: n}, entries); err == nil {
			t.Errorf("write %d failed and the error was dropped", n)
		}
	}
}
