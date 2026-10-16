package http

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// shareFx is the ceremony fixture with the cast of the sharing-change
// tests: the owner (f.ops), two administrators (a key and a browser
// session, neither holding approve:shares), two share approvers (alice
// and bob) and dana, a share approver every change of the cast
// benefits, with an API key. "team" is the audience, "base" the policy.
type shareFx struct {
	*uiFixture
	admin, sess adminCaller
	alice, bob  actor
	dana        actor
}

// actor is a device principal and its key.
type actor struct{ id, key string }

// deviceKey creates a device holding roles and mints it a key, as the
// owner.
func (f *uiFixture) deviceKey(name string, roles ...string) actor {
	f.t.Helper()
	resp := f.createPrincipal(f.ops, storage.PrincipalKindDevice, name, roles...)
	require.Equal(f.t, http.StatusCreated, resp.StatusCode, errBody(f.t, resp))
	p := decodeBody[storage.Principal](f.t, resp)
	resp = f.do(http.MethodPost, "/api/v1/principals/"+p.ID+"/keys", map[string]any{"name": name}, withBearer(f.ops))
	require.Equal(f.t, http.StatusCreated, resp.StatusCode, errBody(f.t, resp))
	return actor{id: p.ID, key: decodeBody[createdKey](f.t, resp).Key}
}

func newShareFx(t *testing.T) *shareFx {
	t.Helper()
	f := &shareFx{uiFixture: newCeremonyFixture(t)}
	admins := f.adminCallers()
	f.admin, f.sess = admins[0], admins[1]
	approver := []string{authn.RoleReader, authn.RoleShareApprover}
	f.alice = f.deviceKey("Alice", approver...)
	f.bob = f.deviceKey("Bob", approver...)
	f.dana = f.deviceKey("Dana", approver...)
	return f
}

// Documents of the base policy and its edits.
const (
	baseDoc    = `{"name":"base","data":"type==contact","fields":{"contact":["title","org"]}}`
	widerDoc   = `{"name":"base","data":"type==contact","fields":{"contact":["title","org","email"]}}`
	narrowDoc  = `{"name":"base","data":"type==contact","fields":{"contact":["title"]}}`
	partnerDoc = `{"name":"partners-lite","data":"type==contact","fields":{"contact":["title"]}}`
)

// owner calls path (under /api/v1) as the owner.
func (f *shareFx) owner(method, path string, body any) *http.Response {
	f.t.Helper()
	return f.do(method, "/api/v1"+path, body, withBearer(f.ops))
}

// asKey calls path as the key administrator.
func (f *shareFx) asKey(method, path string, body any) *http.Response {
	f.t.Helper()
	return f.callAs(f.admin, method, "/api/v1"+path, body)
}

// asActor calls path as a device holding a key.
func (f *shareFx) asActor(a actor, method, path string, body any) *http.Response {
	f.t.Helper()
	return f.do(method, "/api/v1"+path, body, withBearer(a.key))
}

// mustOwner calls as the owner and requires status.
func (f *shareFx) mustOwner(status int, method, path string, body any) *http.Response {
	f.t.Helper()
	resp := f.owner(method, path, body)
	require.Equal(f.t, status, resp.StatusCode, "%s %s: %s", method, path, errBody(f.t, resp))
	return resp
}

// seedTeam is the audience "team" and the policy "base", unlinked.
func (f *shareFx) seedTeam() {
	f.mustOwner(http.StatusCreated, http.MethodPost, "/share/audiences", map[string]any{"name": "team"})
	f.mustOwner(http.StatusCreated, http.MethodPost, "/share/policies", map[string]any{"document": baseDoc})
}

// seedGranted is seedTeam plus dana in team and base granted to it. It
// returns the grant id.
func (f *shareFx) seedGranted() string {
	f.seedTeam()
	f.mustOwner(http.StatusOK, http.MethodPut, "/share/audiences/team/members/"+f.dana.id, map[string]any{})
	resp := f.mustOwner(http.StatusCreated, http.MethodPost, "/share/grants", map[string]any{"audience": "team", "policy": "base"})
	return decodeBody[grantRecord](f.t, resp).ID
}

// keyID is the id of dana's key.
func (f *shareFx) danaKeyID() string { return f.keyOf(f.dana.id) }

// bindDanaKey binds dana's key to policy (by name) as the owner.
func (f *shareFx) bindDanaKey(policy string) {
	f.mustOwner(http.StatusOK, http.MethodPut, "/share/keys/"+f.danaKeyID()+"/policy", map[string]any{"policy": policy})
}

// dump renders the registry rows the cast can change, straight from
// the store, so a test compares what a request left untouched.
func (f *shareFx) dump() string {
	f.t.Helper()
	st := f.bundle.svc.Store(ownerCtx()).Sharing()
	ctx := ownerCtx()
	var b strings.Builder
	auds, err := st.Audiences().List(ctx)
	require.NoError(f.t, err)
	for _, a := range auds {
		ms, err := st.Audiences().ListMembers(ctx, a.ID)
		require.NoError(f.t, err)
		fmt.Fprintf(&b, "audience %s %s\n", a.ID, a.Name)
		for _, m := range ms {
			fmt.Fprintf(&b, "  member %s %v by %s\n", m.PrincipalID, m.ExpiresAt, m.AddedBy)
		}
	}
	pols, err := st.Policies().List(ctx)
	require.NoError(f.t, err)
	for _, p := range pols {
		fmt.Fprintf(&b, "policy %s v%d %s\n", p.Name, p.Version, p.Spec)
	}
	grants, err := st.Grants().List(ctx, storage.GrantFilter{IncludeRevoked: true})
	require.NoError(f.t, err)
	for _, g := range grants {
		fmt.Fprintf(&b, "grant %s revoked=%v\n", g.ID, g.RevokedAt != nil)
	}
	classes, err := st.DenyClasses(ctx, f.dana.id)
	require.NoError(f.t, err)
	fmt.Fprintf(&b, "ceiling %v\n", classes)
	key, err := f.keys.Get(ctx, f.danaKeyID())
	require.NoError(f.t, err)
	bound, err := st.IdentityPolicy(ctx, key.IdentityID)
	require.NoError(f.t, err)
	fmt.Fprintf(&b, "key -> %q\n", bound)
	return b.String()
}

// count returns how many audit entries have event type kind.
func (f *shareFx) count(kind string) int {
	f.t.Helper()
	entries, _, err := f.bundle.svc.Store(ownerCtx()).AuditLog().List(ownerCtx(), storage.AuditFilter{EventType: kind, Limit: 1000})
	require.NoError(f.t, err)
	return len(entries)
}

// approvals lists the requests the owner sees.
func (f *shareFx) approvals() []approvalRecord {
	f.t.Helper()
	resp := f.owner(http.MethodGet, "/approvals", nil)
	require.Equal(f.t, http.StatusOK, resp.StatusCode, errBody(f.t, resp))
	return decodeBody[struct {
		Data []approvalRecord `json:"data"`
	}](f.t, resp).Data
}

// decide posts a decision on request id as a device holding a key.
func (f *shareFx) decide(a actor, verb string, r approvalRecord) *http.Response {
	f.t.Helper()
	return f.asActor(a, http.MethodPost, "/approvals/"+r.ID+"/"+verb, map[string]string{"payload_digest": r.PayloadDigest, "note": verb})
}

// requireCodeOf asserts resp is status with error code.
func requireCodeOf(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	require.Equal(t, status, resp.StatusCode, "want %s", code)
	assert.Equal(t, code, errCode(t, resp))
}
