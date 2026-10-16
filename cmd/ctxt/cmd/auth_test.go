package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The auth, principal and share commands call the registry routes of
// a protected in-process dpkms (dpkmstest.WithKeys) as the owner.
// executeCommand folds stderr into stdout in write order: a command
// that prints a value alone on stdout and a note on stderr yields the
// value first (keys, ids); a link command prints its note first, then
// the link.

func firstLine(out string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(line)
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func TestAuthWhoami(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	out, err := db.exec("auth", "whoami", "--format", "json")
	if err != nil {
		t.Fatalf("auth whoami: %v\n%s", err, out)
	}
	var who whoamiReply
	if err := json.Unmarshal([]byte(out), &who); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if who.Principal != storage.OwnerPrincipal || who.Via != "token" {
		t.Errorf("whoami = %+v, want the owner by token", who)
	}

	db.useRole(t, dpkmstest.RoleReader)
	out, err = db.exec("auth", "whoami")
	if err != nil {
		t.Fatalf("auth whoami as reader: %v\n%s", err, out)
	}
	if !strings.Contains(out, dpkmstest.PrincipalReader) || !strings.Contains(out, "reader") {
		t.Errorf("reader whoami = %q", out)
	}
}

func TestShareInvitePrintsLinkOnPublicURL(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	out, err := db.exec("share", "invite", "--name", "Dana Partner", "--expires", "2d")
	if err != nil {
		t.Fatalf("share invite: %v\n%s", err, out)
	}
	link := lastLine(out)
	want := db.Server.URL + "/ui/invite#t="
	if !strings.HasPrefix(link, want) {
		t.Fatalf("link = %q, want prefix %q (server.public_url)\n%s", link, want, out)
	}
	if n := len(strings.TrimPrefix(link, want)); n != 43 {
		t.Errorf("secret length = %d, want 43", n)
	}
	if !strings.Contains(firstLine(out), "valid until") {
		t.Errorf("no note ahead of the link:\n%s", out)
	}

	out, err = db.exec("share", "invite", "--name", "Dana", "--format", "json")
	if err != nil {
		t.Fatalf("share invite --format json: %v\n%s", err, out)
	}
	var reply linkReply
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if reply.ExpiresIn != 7*24*3600 {
		t.Errorf("expires_in = %d, want 7 days by default", reply.ExpiresIn)
	}

	// A reader's key holds no admin:shares: exit 5.
	db.useRole(t, dpkmstest.RoleReader)
	if _, err := db.exec("share", "invite", "--name", "Dana"); err == nil {
		t.Fatal("share invite as reader succeeded, want a refusal")
	}
}

func TestPrincipalLifecycleOverAPI(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())

	out, err := db.exec("principal", "create", "--kind", "peer", "--name", "partner instance")
	if err != nil {
		t.Fatalf("principal create: %v\n%s", err, out)
	}
	id := firstLine(out)
	if !strings.HasPrefix(id, "prn_") {
		t.Fatalf("create printed %q, want the new id first", out)
	}

	out, err = db.exec("principal", "list", "--kind", "peer", "--format", "json")
	if err != nil {
		t.Fatalf("principal list: %v\n%s", err, out)
	}
	if !strings.Contains(out, id) || !strings.Contains(out, `"writer"`) {
		t.Errorf("list = %s, want the peer with writer roles", out)
	}

	// The peer's key, minted by the owner, authenticates as the peer.
	out, err = db.exec("auth", "key", "create", "--principal", id, "--name", "push", "--expires", "30d")
	if err != nil {
		t.Fatalf("auth key create --principal: %v\n%s", err, out)
	}
	key := firstLine(out)
	if !strings.HasPrefix(key, "ctxt_") {
		t.Fatalf("key create printed %q, want the key first", out)
	}
	p, err := db.Driver.Auth().Identities().ListByPrincipal(context.Background(), id)
	if err != nil || len(p) != 1 || p[0].Method != storage.IdentityMethodAPIKey {
		t.Fatalf("peer identities = %v, %v; want one api_key row", p, err)
	}

	out, err = db.exec("principal", "show", id)
	if err != nil {
		t.Fatalf("principal show: %v\n%s", err, out)
	}
	for _, want := range []string{"partner instance", "peer", "push", "Keys (1)", "Identities (1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}

	out, err = db.exec("principal", "link", id)
	if err != nil {
		t.Fatalf("principal link: %v\n%s", err, out)
	}
	if !strings.HasPrefix(lastLine(out), db.Server.URL+"/ui/signin#t=") {
		t.Errorf("link = %q, want a sign-in link on server.public_url", out)
	}

	out, err = db.exec("auth", "key", "list", "--principal", id, "--format", "json")
	if err != nil {
		t.Fatalf("auth key list --principal: %v\n%s", err, out)
	}
	var keys struct {
		Keys []struct {
			ID string `json:"id"`
		} `json:"keys"`
	}
	if err := json.Unmarshal([]byte(out), &keys); err != nil || len(keys.Keys) != 1 {
		t.Fatalf("key list = %s (%v), want one key", out, err)
	}
	if out, err = db.exec("auth", "key", "revoke", keys.Keys[0].ID, "--principal", id); err != nil {
		t.Fatalf("auth key revoke: %v\n%s", err, out)
	}

	if out, err = db.exec("principal", "suspend", id); err != nil {
		t.Fatalf("principal suspend: %v\n%s", err, out)
	}
	if _, err = db.exec("principal", "link", id); err == nil {
		t.Fatal("link for a suspended principal succeeded, want a refusal")
	}
	// The confirmation gate is kit's, installed at Execute and covered
	// by its own test; here the revoke itself is exercised.
	if out, err = db.exec("principal", "revoke", id); err != nil {
		t.Fatalf("principal revoke: %v\n%s", err, out)
	}
	out, err = db.exec("principal", "show", id, "--format", "json")
	if err != nil {
		t.Fatalf("principal show after revoke: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"status": "revoked"`) {
		t.Errorf("show after revoke = %s, want status revoked", out)
	}
}

func TestAuthKeyOwnLifecycle(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	db.useRole(t, dpkmstest.RoleReader)

	out, err := db.exec("auth", "key", "create", "--name", "laptop", "--scope", "read:objects")
	if err != nil {
		t.Fatalf("auth key create: %v\n%s", err, out)
	}
	if !strings.HasPrefix(firstLine(out), "ctxt_") {
		t.Fatalf("key create printed %q, want the key first", out)
	}
	out, err = db.exec("auth", "key", "list", "--format", "json")
	if err != nil {
		t.Fatalf("auth key list: %v\n%s", err, out)
	}
	var keys struct {
		Keys []struct {
			ID     string   `json:"id"`
			Name   string   `json:"name"`
			Scopes []string `json:"scopes"`
		} `json:"keys"`
	}
	if err := json.Unmarshal([]byte(out), &keys); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	var created string
	for _, k := range keys.Keys {
		if k.Name == "laptop" {
			created = k.ID
			if len(k.Scopes) != 1 || k.Scopes[0] != "read:objects" {
				t.Errorf("scopes = %v, want [read:objects]", k.Scopes)
			}
		}
	}
	if created == "" {
		t.Fatalf("new key not listed: %s", out)
	}
	if _, err := db.exec("auth", "key", "create", "--scope", "admin:principals"); err == nil {
		t.Fatal("a reader minted an admin scope")
	}
	if out, err := db.exec("auth", "key", "revoke", created); err != nil {
		t.Fatalf("auth key revoke: %v\n%s", err, out)
	}

	// Another principal's key is refused under write:self.
	ownerKeys, err := db.Driver.Auth().Identities().ListByPrincipal(context.Background(), storage.OwnerPrincipal)
	if err != nil || len(ownerKeys) == 0 {
		t.Fatalf("owner identities: %v %v", ownerKeys, err)
	}
	if _, err := db.exec("auth", "key", "revoke", ownerKeys[0].Subject); err == nil {
		t.Fatal("a reader revoked the owner's key")
	}

	out, err = db.exec("auth", "identity", "list", "--format", "json")
	if err != nil {
		t.Fatalf("auth identity list: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"api_key"`) {
		t.Errorf("identity list = %s", out)
	}
	out, err = db.exec("auth", "session", "list", "--format", "json")
	if err != nil {
		t.Fatalf("auth session list: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"total": 0`) {
		t.Errorf("a key caller has no browser sessions: %s", out)
	}
}
