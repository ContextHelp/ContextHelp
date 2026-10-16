package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// shareEvents lists the sharing events in the audit log, in the order the store returns them (oldest first).
func (f *uiFixture) shareEvents() []*storage.AuditEntry {
	f.t.Helper()
	entries, _, err := f.bundle.svc.Store(ownerCtx()).AuditLog().List(ownerCtx(), storage.AuditFilter{Limit: 1000})
	require.NoError(f.t, err)
	var out []*storage.AuditEntry
	for _, e := range entries {
		if strings.HasPrefix(e.EventType, "share.") {
			out = append(out, e)
		}
	}
	return out
}

func eventTypes(entries []*storage.AuditEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.EventType
	}
	return out
}

// Every change through the sharing routes is one audit log entry naming
// its caller, the owner's included, and a request that changed nothing
// records nothing. Administrators and the owner alike are on the record
// (ADR-080 §9).
func TestShareChanges_EachWriteIsOneAuditEntry(t *testing.T) {
	f := newCeremonyFixture(t)
	admin := f.adminCallers()[0]

	resp := f.createAudience(f.ops, "partners")
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	resp = f.do(http.MethodPost, "/api/v1/share/templates/"+sharing.TemplatePartners, nil, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	pol := decodeBody[storage.SharePolicy](t, resp)
	resp = f.do(http.MethodPost, "/api/v1/share/grants", map[string]any{"audience": "partners", "policy": pol.Name}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	grant := decodeBody[grantRecord](t, resp)
	resp = f.do(http.MethodPost, "/api/v1/share/invites", map[string]any{"display_name": "Bob", "audience": "partners"}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	secret := secretOf(t, decodeBody[linkResponse](t, resp).URL)

	// The administrator narrows the policy and binds its own key to it:
	// both apply at once. Joining the audience would widen what it sees:
	// that is a pending request, on the record as a request and as
	// nothing else; the owner adds it.
	resp = f.callAs(admin, http.MethodPut, "/api/v1/share/policies/"+pol.Name, map[string]any{"document": "name: " + pol.Name + "\nversion: 1\ndata: \"type==contact\"\nexclude_classes: [personal]\nfields:\n  contact: [title]\ncapabilities:\n  search: [fts]\n"})
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	atRequest := len(f.shareEvents())
	resp = f.callAs(admin, http.MethodPut, "/api/v1/share/audiences/partners/members/"+admin.principal, map[string]any{})
	require.Equal(t, http.StatusAccepted, resp.StatusCode, errBody(t, resp))
	require.Len(t, f.shareEvents(), atRequest, "a pending change is not a change on the record")
	resp = f.do(http.MethodPut, "/api/v1/share/audiences/partners/members/"+admin.principal, map[string]any{}, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	resp = f.callAs(admin, http.MethodPut, "/api/v1/share/keys/"+f.keyOf(admin.principal)+"/policy", map[string]any{"policy": pol.Name})
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))

	// A request that changes nothing records nothing: a grant to an
	// audience that does not exist, a member that is the owner, a bad
	// ceiling, an invite only the owner can mint.
	before := len(f.shareEvents())
	resp = f.callAs(admin, http.MethodPost, "/api/v1/share/grants", map[string]any{"audience": "nobody", "policy": pol.Name})
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp = f.callAs(admin, http.MethodPut, "/api/v1/share/audiences/partners/members/owner", map[string]any{})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp = f.callAs(admin, http.MethodPut, "/api/v1/principals/"+admin.principal+"/ceiling", map[string]any{"deny_classes": []string{"Bad!"}})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp = f.callAs(admin, http.MethodPost, "/api/v1/share/invites", map[string]any{"display_name": "Mallory", "audience": "partners"})
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Len(t, f.shareEvents(), before, "refusals leave no entry")

	resp = f.callAs(admin, http.MethodPut, "/api/v1/principals/"+admin.principal+"/ceiling", map[string]any{"deny_classes": []string{"personal"}})
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	resp = f.callAs(admin, http.MethodDelete, "/api/v1/share/grants/"+grant.ID, nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp = f.callAs(admin, http.MethodDelete, "/api/v1/share/audiences/partners/members/"+admin.principal, nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	events := f.shareEvents()
	assert.Equal(t, []string{
		EventAudienceCreated, EventPolicyCreated, EventGrantCreated, EventInviteMinted,
		EventPolicyUpdated, EventMemberAdded, EventKeyPolicyBound,
		EventCeilingSet, EventGrantRevoked, EventMemberRemoved,
	}, eventTypes(events))
	actors := []string{storage.OwnerPrincipal, storage.OwnerPrincipal, storage.OwnerPrincipal, storage.OwnerPrincipal,
		admin.principal, storage.OwnerPrincipal, admin.principal, admin.principal, admin.principal, admin.principal}
	for i, e := range events {
		assert.Equal(t, actors[i], e.Actor, "%s", e.EventType)
		assert.NotEmpty(t, e.ID)
		assert.False(t, e.CreatedAt.IsZero())
		assert.Empty(t, e.ObjectID, "a sharing change is about no object")
	}
	assert.Equal(t, "partners", events[2].Payload["audience"])
	assert.Equal(t, pol.Name, events[2].Payload["policy"])
	assert.Equal(t, grant.ID, events[2].Payload["grant_id"])
	assert.Equal(t, admin.principal, events[5].Payload["principal_id"])
	assert.Equal(t, pol.ID, events[6].Payload["policy_id"])

	// The invite's link is a credential: no entry holds it.
	for _, e := range events {
		b, err := json.Marshal(e.Payload)
		require.NoError(t, err)
		assert.NotContains(t, string(b), secret, "%s", e.EventType)
	}
}

// The audit log is the owner's: an administrator reads no entry of it,
// its own changes included.
func TestShareChanges_AdministratorsCannotReadTheLog(t *testing.T) {
	f := newCeremonyFixture(t)
	for _, c := range f.adminCallers() {
		resp := f.callAs(c, http.MethodPost, "/api/v1/share/audiences", map[string]any{"name": "log-" + c.name})
		require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
		resp = f.callAs(c, http.MethodGet, "/api/v1/audit-log", nil)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s admin", c.name)
	}
	assert.Len(t, f.shareEvents(), 2)
}

// Redeeming an invite that names an audience puts the new person in it:
// a sharing change like any other, so one share.member.added entry, made
// on the invite minter's authority, naming the redeemer and the invite
// as the way in. An invite with no audience adds no member.
func TestShareChanges_InviteRedemptionRecordsTheMembership(t *testing.T) {
	f := newCeremonyFixture(t)
	resp := f.createAudience(f.ops, "partners")
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	audience := decodeBody[storage.Audience](t, resp)

	// No audience: nothing to record.
	f.acceptInvite("Plain")
	assert.NotContains(t, eventTypes(f.shareEvents()), EventMemberAdded)

	resp = f.do(http.MethodPost, "/api/v1/share/invites", map[string]any{"display_name": "Dana", "audience": "partners"}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	secret := secretOf(t, decodeBody[linkResponse](t, resp).URL)
	resp = f.redeemInvite(secret)
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	redeemer := decodeBody[exchangeResponse](t, resp)

	var added []*storage.AuditEntry
	for _, e := range f.shareEvents() {
		if e.EventType == EventMemberAdded {
			added = append(added, e)
		}
	}
	require.Len(t, added, 1)
	assert.Equal(t, storage.OwnerPrincipal, added[0].Actor, "the invite's minter")
	assert.Equal(t, redeemer.Principal, added[0].Payload["principal_id"])
	assert.Equal(t, audience.ID, added[0].Payload["audience_id"])
	assert.Equal(t, "partners", added[0].Payload["audience"])
	assert.Equal(t, "invite", added[0].Payload["via"])
	b, err := json.Marshal(added[0].Payload)
	require.NoError(t, err)
	assert.NotContains(t, string(b), secret)
}
