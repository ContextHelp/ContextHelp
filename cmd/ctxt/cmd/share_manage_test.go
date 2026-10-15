package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The share audience, policy, grant, revoke, ceiling and template
// commands against a protected in-process dpkms as the owner.

const testPolicyYAML = `name: colleagues-network
data: "type==contact;tag=in=(work,client)"
exclude_classes: [personal]
fields:
  contact: [title, org, role, tags, city]
capabilities:
  search: [fts, vector]
  entities: true
`

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShareAudienceGrantLifecycle(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())

	out, err := db.exec("share", "audience", "create", "partners")
	if err != nil {
		t.Fatalf("audience create: %v\n%s", err, out)
	}
	audID := firstLine(out)
	if !strings.HasPrefix(audID, "aud_") {
		t.Fatalf("create printed %q, want the id first", out)
	}
	if out, err := db.exec("share", "audience", "create", "Bad Name"); err == nil {
		t.Fatalf("a bad name was accepted:\n%s", out)
	}

	out, err = db.exec("share", "audience", "add", "partners", dpkmstest.PrincipalReader, "--expires", "2d")
	if err != nil {
		t.Fatalf("audience add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Added "+dpkmstest.PrincipalReader+" to partners, until ") {
		t.Errorf("add printed %q", out)
	}

	out, err = db.exec("share", "audience", "list", "--format", "json")
	if err != nil {
		t.Fatalf("audience list: %v\n%s", err, out)
	}
	var listed struct {
		Audiences []audienceReply `json:"audiences"`
		Total     int             `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if listed.Total != 1 || len(listed.Audiences) != 1 || listed.Audiences[0].ID != audID || len(listed.Audiences[0].Members) != 1 {
		t.Fatalf("list = %+v", listed)
	}
	if listed.Audiences[0].Members[0].ExpiresAt == nil {
		t.Error("membership has no expiry")
	}
	out, err = db.exec("share", "audience", "list")
	if err != nil {
		t.Fatalf("audience list (table): %v\n%s", err, out)
	}
	if !strings.Contains(out, "partners") || !strings.Contains(out, dpkmstest.PrincipalReader+" (until ") {
		t.Errorf("table = %s", out)
	}

	// Templates: list, install once.
	out, err = db.exec("share", "template", "list")
	if err != nil {
		t.Fatalf("template list: %v\n%s", err, out)
	}
	if !strings.Contains(out, sharing.TemplateColleagues) || !strings.Contains(out, sharing.TemplatePartners) {
		t.Errorf("template list = %s", out)
	}
	out, err = db.exec("share", "template", "install", sharing.TemplatePartners)
	if err != nil {
		t.Fatalf("template install: %v\n%s", err, out)
	}
	if !strings.HasPrefix(firstLine(out), "pol_") {
		t.Fatalf("install printed %q, want the policy id first", out)
	}
	if _, err := db.exec("share", "template", "install", sharing.TemplatePartners); err == nil {
		t.Fatal("a second install succeeded, want a refusal")
	}
	if _, err := db.exec("share", "template", "install", "nope"); err == nil {
		t.Fatal("an unknown template was accepted")
	}

	// Grant, list the member's view, revoke.
	out, err = db.exec("share", "grant", "partners", sharing.TemplatePartners, "--expires", "30d")
	if err != nil {
		t.Fatalf("grant: %v\n%s", err, out)
	}
	grantID := firstLine(out)
	if !strings.HasPrefix(grantID, "grt_") {
		t.Fatalf("grant printed %q, want the grant id first", out)
	}
	if !strings.Contains(out, "Granted "+sharing.TemplatePartners+" (version 1) to partners, until ") {
		t.Errorf("grant note = %q", out)
	}
	active, err := db.Driver.Sharing().Grants().ListActiveForPrincipal(context.Background(), dpkmstest.PrincipalReader, time.Now().UTC())
	if err != nil || len(active) != 1 || active[0].Grant.ID != grantID {
		t.Fatalf("active grants of the reader = %+v, %v", active, err)
	}

	out, err = db.exec("share", "revoke", grantID)
	if err != nil {
		t.Fatalf("revoke: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Revoked grant "+grantID) {
		t.Errorf("revoke printed %q", out)
	}
	active, err = db.Driver.Sharing().Grants().ListActiveForPrincipal(context.Background(), dpkmstest.PrincipalReader, time.Now().UTC())
	if err != nil || len(active) != 0 {
		t.Fatalf("after revoke: %+v, %v", active, err)
	}

	out, err = db.exec("share", "audience", "remove", "partners", dpkmstest.PrincipalReader)
	if err != nil {
		t.Fatalf("audience remove: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed "+dpkmstest.PrincipalReader+" from partners") {
		t.Errorf("remove printed %q", out)
	}

	// Ceiling.
	out, err = db.exec("share", "ceiling", dpkmstest.PrincipalReader, "--deny-classes", "personal,financial")
	if err != nil {
		t.Fatalf("ceiling: %v\n%s", err, out)
	}
	if !strings.Contains(out, "never sees: financial, personal") {
		t.Errorf("ceiling printed %q", out)
	}
	classes, err := db.Driver.Sharing().DenyClasses(context.Background(), dpkmstest.PrincipalReader)
	if err != nil || strings.Join(classes, ",") != "financial,personal" {
		t.Fatalf("stored ceiling = %v, %v", classes, err)
	}
	if _, err := db.exec("share", "ceiling", dpkmstest.PrincipalReader, "--deny-classes", "Bad!"); err == nil {
		t.Fatal("a bad class was accepted")
	}
	out, err = db.exec("share", "ceiling", dpkmstest.PrincipalReader, "--deny-classes", "")
	if err != nil {
		t.Fatalf("ceiling clear: %v\n%s", err, out)
	}
	if !strings.Contains(out, "has no ceiling") {
		t.Errorf("clear printed %q", out)
	}

	// A reader's key holds no admin:shares: exit 5.
	db.useRole(t, dpkmstest.RoleReader)
	if _, err := db.exec("share", "audience", "list"); err == nil {
		t.Fatal("share audience list as reader succeeded, want a refusal")
	}
}

func TestSharePolicyCreateShowEdit(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	file := writeTemp(t, "colleagues.yaml", testPolicyYAML)

	out, err := db.exec("share", "policy", "create", file)
	if err != nil {
		t.Fatalf("policy create: %v\n%s", err, out)
	}
	polID := firstLine(out)
	if !strings.HasPrefix(polID, "pol_") {
		t.Fatalf("create printed %q, want the id first", out)
	}
	if !strings.Contains(out, "at version 1") {
		t.Errorf("create note = %q", out)
	}
	if out, err := db.exec("share", "policy", "create", writeTemp(t, "bad.yaml", "name: bad\ndata: \"mention==x\"\n")); err == nil {
		t.Fatalf("a bad document was accepted:\n%s", out)
	} else if !strings.Contains(err.Error(), "data:") {
		t.Errorf("refusal does not name the field: %v", err)
	}

	out, err = db.exec("share", "policy", "show", "colleagues-network")
	if err != nil {
		t.Fatalf("policy show: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "# "+polID+", version 1, updated ") || !strings.Contains(out, "name: colleagues-network") ||
		!strings.Contains(out, "data: type==contact;tag=in=(work,client)") {
		t.Errorf("show = %s", out)
	}

	// The shown document edits back; the version bumps.
	edited := strings.Replace(out, "city", "country", 1)
	out, err = db.exec("share", "policy", "edit", "colleagues-network", writeTemp(t, "edited.yaml", edited))
	if err != nil {
		t.Fatalf("policy edit: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Policy colleagues-network is now at version 2.") {
		t.Errorf("edit printed %q", out)
	}
	row, err := db.Driver.Sharing().Policies().GetByName(context.Background(), "colleagues-network")
	if err != nil || row.Version != 2 || !strings.Contains(string(row.Spec), "country") {
		t.Fatalf("stored policy = %+v, %v", row, err)
	}

	// Stdin feeds create and edit when no file is given.
	rootCmd.SetIn(strings.NewReader(strings.Replace(testPolicyYAML, "colleagues-network", "stdin-policy", 1)))
	out, err = db.exec("share", "policy", "create")
	rootCmd.SetIn(nil)
	if err != nil {
		t.Fatalf("policy create from stdin: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Created policy stdin-policy") {
		t.Errorf("stdin create printed %q", out)
	}

	out, err = db.exec("share", "policy", "list", "--format", "json")
	if err != nil {
		t.Fatalf("policy list: %v\n%s", err, out)
	}
	var listed struct {
		Policies []storage.SharePolicy `json:"policies"`
		Total    int                   `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if listed.Total != 2 || listed.Policies[0].Name != "colleagues-network" || listed.Policies[0].Version != 2 {
		t.Errorf("list = %+v", listed)
	}
	out, err = db.exec("share", "policy", "list")
	if err != nil {
		t.Fatalf("policy list (table): %v\n%s", err, out)
	}
	if !strings.Contains(out, "colleagues-network") || !strings.Contains(out, "stdin-policy") {
		t.Errorf("table = %s", out)
	}
}

// An invite minted with --audience lands the person in the audience
// when they accept it in the browser.
func TestShareInviteIntoAudience(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	if _, err := db.exec("share", "audience", "create", "partners"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.exec("share", "invite", "--name", "Dana", "--audience", "nope"); err == nil {
		t.Fatal("an unknown audience was accepted")
	}
	out, err := db.exec("share", "invite", "--name", "Dana Partner", "--audience", "partners")
	if err != nil {
		t.Fatalf("share invite --audience: %v\n%s", err, out)
	}
	link := lastLine(out)
	_, secret, ok := strings.Cut(link, "#t=")
	if !ok {
		t.Fatalf("link = %q", link)
	}

	// Accept it the way the web UI does.
	body, _ := json.Marshal(map[string]string{"ticket": secret})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, db.Server.URL+"/ui/invite/session", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", db.Server.URL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-Ctxt-CSRF", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var signed struct {
		Principal string `json:"principal"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&signed); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("redeem: %d %v", resp.StatusCode, err)
	}

	aud, err := db.Driver.Sharing().Audiences().GetByName(context.Background(), "partners")
	if err != nil {
		t.Fatal(err)
	}
	members, err := db.Driver.Sharing().Audiences().ListMembers(context.Background(), aud.ID)
	if err != nil || len(members) != 1 || members[0].PrincipalID != signed.Principal {
		t.Fatalf("members = %+v, %v (want %s)", members, err, signed.Principal)
	}
	if members[0].AddedBy != storage.OwnerPrincipal {
		t.Errorf("added by %q, want the owner", members[0].AddedBy)
	}
	out, err = db.exec("share", "audience", "list")
	if err != nil {
		t.Fatalf("audience list: %v\n%s", err, out)
	}
	if !strings.Contains(out, signed.Principal) {
		t.Errorf("list lacks the invited person:\n%s", out)
	}
}
