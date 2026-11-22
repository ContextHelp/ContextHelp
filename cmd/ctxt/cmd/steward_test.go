package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/kit/go/console/output"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The steward commands against the in-process dpkms: off by default,
// then on, with a person to steward, a second person holding
// the steward-requester role and a key, and the owner, who approves.

const (
	cliSteward   = "prn_cli_steward"
	cliRequester = "prn_cli_requester"
	cliReason    = "Legal hold for matter 2026-117: opposing counsel asked for the supplier contacts"
)

// useToken rewrites the client config so commands authenticate with
// tok.
func (db *testDB) useToken(t *testing.T, tok string) {
	t.Helper()
	body := "server:\n  urls:\n    - " + db.Server.URL + "\n  token: " + tok + "\n"
	if err := os.WriteFile(db.ClientConfigPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write client config: %v", err)
	}
}

// stewardDB is a protected instance with steward access on, the
// steward, and the requester's key.
func stewardDB(t *testing.T) (*testDB, string) {
	t.Helper()
	db := setupTestDB(t, dpkmstest.WithKeys())
	db.Server.Config.Steward.Enabled = true
	ctx := context.Background()
	for id, p := range map[string]*storage.Principal{
		cliSteward:   {ID: cliSteward, Kind: storage.PrincipalKindPerson, DisplayName: "Steward", Roles: []string{authn.RoleReader, authn.RoleStewardRequester}},
		cliRequester: {ID: cliRequester, Kind: storage.PrincipalKindPerson, DisplayName: "Legal ops", Roles: []string{authn.RoleReader, authn.RoleStewardRequester}},
	} {
		if err := db.Driver.Auth().Principals().Create(ctx, p); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	tok, _, err := authn.NewKeysOver(db.Driver.Auth(), nil).Mint(ctx, authn.MintKey{PrincipalID: cliRequester, Name: "legal ops"})
	if err != nil {
		t.Fatalf("mint requester key: %v", err)
	}
	return db, tok
}

// scopeFile writes a YAML scope document and returns its path.
func scopeFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scope.yaml")
	if err := os.WriteFile(path, []byte("data: type==contact\nfields:\n  contact: [title]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Off, the steward routes do not exist: every command is answered not
// found, and nothing is filed.
func TestStewardCommands_Off(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	for _, args := range [][]string{
		{"steward", "list"},
		{"steward", "show", "stg_x"},
		{"steward", "revoke", "stg_x"},
		{"steward", "request", "--steward", cliSteward, "--scope", scopeFile(t), "--expires", "7d", "--reason", cliReason},
	} {
		if _, err := db.exec(args...); exitOf(err) != output.ExitNotFound {
			t.Errorf("%v: exit %d (%v), want not found", args, exitOf(err), err)
		}
	}
}

// Request, approve, list, show and revoke, each as the principal that
// may: the requester files and sees the pending request, the owner
// approves it, both see the grant, an unrelated key sees none, and the
// requester revokes it once.
func TestStewardCommands_Flow(t *testing.T) {
	db, requesterKey := stewardDB(t)
	db.useToken(t, requesterKey)
	out, err := db.exec("steward", "request", "--steward", cliSteward, "--scope", scopeFile(t),
		"--expires", "7d", "--matter", "CASE-117", "--reason", cliReason)
	if err != nil {
		t.Fatalf("request: %v\n%s", err, out)
	}
	reqID := strings.TrimSpace(firstLine(out))
	if !strings.HasPrefix(reqID, "apr_") {
		t.Fatalf("request printed %q, want the request id first", out)
	}

	db.useRole(t, dpkmstest.RoleAdmin)
	out, err = db.exec("approval", "show", reqID, "--format", "json")
	var shown approvalReply
	if err != nil || json.Unmarshal([]byte(out), &shown) != nil || shown.PayloadDigest == "" {
		t.Fatalf("show %s = %q (%v), want the request with its digest", reqID, out, err)
	}
	out, err = db.exec("approval", "approve", reqID, "--digest", shown.PayloadDigest, "--format", "json")
	if err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	var decided approvalReply
	if err := json.Unmarshal([]byte(out), &decided); err != nil || decided.State != "approved" || !strings.HasPrefix(decided.ResultRef, "stg_") {
		t.Fatalf("approve answered %q (%v), want an approved request naming its grant", out, err)
	}
	grant := decided.ResultRef

	for _, who := range []string{"owner", "requester"} {
		if who == "requester" {
			db.useToken(t, requesterKey)
		}
		out, err = db.exec("steward", "list")
		if err != nil || !strings.Contains(out, grant) || !strings.Contains(out, "active") || !strings.Contains(out, "CASE-117") {
			t.Errorf("list as %s = %q (%v), want the active grant", who, out, err)
		}
		out, err = db.exec("steward", "show", grant)
		if err != nil || !strings.Contains(out, cliSteward) || !strings.Contains(out, cliRequester) || !strings.Contains(out, "type==contact") {
			t.Errorf("show as %s = %q (%v)", who, out, err)
		}
	}
	out, err = db.exec("steward", "list", "--format", "json")
	var listed struct {
		Grants []stewardGrantReply `json:"grants"`
		Total  int                 `json:"total"`
	}
	if err != nil || json.Unmarshal([]byte(out), &listed) != nil || listed.Total != 1 || listed.Grants[0].ID != grant {
		t.Errorf("list json = %q (%v)", out, err)
	}

	db.useRole(t, dpkmstest.RoleReader)
	out, err = db.exec("steward", "list")
	if err != nil || !strings.Contains(out, "No steward grants.") {
		t.Errorf("list as an unrelated key = %q (%v), want none", out, err)
	}
	if _, err := db.exec("steward", "show", grant); exitOf(err) != output.ExitNotFound {
		t.Errorf("show as an unrelated key: exit %d (%v), want not found", exitOf(err), err)
	}

	db.useToken(t, requesterKey)
	out, err = db.exec("steward", "revoke", grant, "--reason", "matter closed")
	if err != nil || !strings.Contains(out, "Revoked steward grant "+grant) {
		t.Fatalf("revoke = %q (%v)", out, err)
	}
	if _, err := db.exec("steward", "revoke", grant); err == nil {
		t.Error("a second revocation succeeded")
	}
	out, err = db.exec("steward", "list", "--state", "revoked")
	if err != nil || !strings.Contains(out, grant) {
		t.Errorf("list --state revoked = %q (%v)", out, err)
	}
}

// The command checks its flags before it calls the instance, and the
// instance's refusals reach the caller with their exit class.
func TestStewardCommands_Refusals(t *testing.T) {
	db, requesterKey := stewardDB(t)
	scope := scopeFile(t)
	db.useToken(t, requesterKey)
	for name, args := range map[string][]string{
		"no scope":           {"steward", "request", "--expires", "7d", "--reason", cliReason},
		"scope and audience": {"steward", "request", "--scope", scope, "--as-audience", "partners", "--expires", "7d", "--reason", cliReason},
		"a bad expiry":       {"steward", "request", "--scope", scope, "--expires", "soon", "--reason", cliReason},
		"a negative limit":   {"steward", "list", "--limit", "-1"},
	} {
		_, err := db.exec(args...)
		if exitOf(err) != output.ExitUsage {
			t.Errorf("%s: exit %d (%v), want usage", name, exitOf(err), err)
		}
		if strings.HasPrefix(name, "no scope") || strings.HasPrefix(name, "scope and") {
			if err == nil || !strings.Contains(err.Error(), "--scope or --as-audience") {
				t.Errorf("%s: err = %v, want the flags named before the instance is called", name, err)
			}
		}
	}
	// Cobra refuses a required flag left out before the command runs.
	for flag, args := range map[string][]string{
		"expires": {"steward", "request", "--scope", scope, "--reason", cliReason},
		"reason":  {"steward", "request", "--scope", scope, "--expires", "7d"},
	} {
		if _, err := db.exec(args...); err == nil || !strings.Contains(err.Error(), `"`+flag+`"`) {
			t.Errorf("no --%s: err = %v, want the required flag named", flag, err)
		}
	}
	out, err := db.exec("steward", "request", "--steward", cliSteward, "--scope", scope, "--expires", "7d", "--reason", cliReason, "--dry-run")
	if err != nil || !strings.HasPrefix(out, "Would request steward access for "+cliSteward) {
		t.Errorf("dry run = %q (%v)", out, err)
	}

	db.useRole(t, dpkmstest.RoleAdmin)
	if _, err := db.exec("steward", "request", "--steward", cliSteward, "--scope", scope, "--expires", "7d", "--reason", cliReason); err == nil ||
		!strings.Contains(err.Error(), "OWNER_CANNOT_FILE") && !strings.Contains(err.Error(), "owner") {
		t.Errorf("the owner filed: %v", err)
	}
	list, err := db.Driver.Approvals().List(context.Background(), storage.ApprovalFilter{})
	if err != nil || len(list) != 0 {
		t.Errorf("approval requests = %d (%v), want none filed", len(list), err)
	}
}

// The help says where reading under a grant happens: the web UI only.
func TestStewardCommands_HelpSaysElevationIsBrowserOnly(t *testing.T) {
	out, err := executeCommand("steward", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(out, "web UI") || !strings.Contains(out, "No command") {
		t.Errorf("steward help does not say elevation is browser-only:\n%s", out)
	}
}
