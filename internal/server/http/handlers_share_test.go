package http

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The sharing registry routes (ADR-080 §11) over the ceremony fixture:
// the owner "ops" (admin:shares) and the person "other" (reader).

const colleaguesYAML = `name: colleagues-network
data: "type==contact;tag=in=(work,client)"
exclude_classes: [personal]
fields:
  contact: [title, org, role, tags, city]
capabilities:
  search: [fts, vector]
  entities: true
`

func (f *uiFixture) createAudience(tok, name string) *http.Response {
	f.t.Helper()
	return f.do(http.MethodPost, "/api/v1/share/audiences", map[string]any{"name": name}, withBearer(tok))
}

// doPolicy sends body as the policy document of a JSON body.
func (f *uiFixture) doPolicy(method, path, body string, opts ...reqOpt) *http.Response {
	f.t.Helper()
	return f.do(method, path, map[string]any{"document": body}, opts...)
}

func TestShare_AudienceLifecycle(t *testing.T) {
	f := newCeremonyFixture(t)

	resp := f.createAudience(f.other, "partners")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "admin:shares is the admin bundle's")
	assert.Equal(t, CodeInsufficientScope, errCode(t, resp))

	resp = f.createAudience(f.ops, "Bad Name")
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, errBody(t, resp), "name:")

	resp = f.createAudience(f.ops, "partners")
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	aud := decodeBody[audienceRecord](t, resp)
	assert.True(t, len(aud.ID) > 4 && aud.ID[:4] == "aud_", aud.ID)
	assert.Equal(t, "partners", aud.Name)
	assert.Empty(t, aud.Members)

	resp = f.createAudience(f.ops, "partners")
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, CodeAlreadyExists, errCode(t, resp))

	resp = f.do(http.MethodGet, "/api/v1/share/audiences", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	list := decodeBody[listOf[audienceRecord]](t, resp)
	require.Len(t, list.Data, 1)
	assert.Equal(t, 1, list.Total)

	for _, ref := range []string{aud.ID, aud.Name} {
		resp = f.do(http.MethodGet, "/api/v1/share/audiences/"+ref, nil, withBearer(f.ops))
		require.Equal(t, http.StatusOK, resp.StatusCode, ref)
		assert.Equal(t, aud.ID, decodeBody[audienceRecord](t, resp).ID)
	}
	resp = f.do(http.MethodGet, "/api/v1/share/audiences/none", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Members: the owner never joins, an unknown principal is 404, a
	// bad expiry names the field.
	members := "/api/v1/share/audiences/partners/members/"
	resp = f.do(http.MethodPut, members+storage.OwnerPrincipal, nil, withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, errBody(t, resp), "principal:")
	resp = f.do(http.MethodPut, members+"prn_nobody", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp = f.do(http.MethodPut, members+"other", map[string]any{"expires_in": -1}, withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, errBody(t, resp), "expires_in")

	before := time.Now().UTC()
	resp = f.do(http.MethodPut, members+"other", map[string]any{"expires_in": 3600}, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	m := decodeBody[storage.AudienceMember](t, resp)
	assert.Equal(t, aud.ID, m.AudienceID)
	assert.Equal(t, "other", m.PrincipalID)
	assert.Equal(t, storage.OwnerPrincipal, m.AddedBy)
	require.NotNil(t, m.ExpiresAt)
	assert.WithinDuration(t, before.Add(time.Hour), *m.ExpiresAt, 5*time.Second)

	resp = f.do(http.MethodGet, "/api/v1/share/audiences/partners", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, decodeBody[audienceRecord](t, resp).Members, 1)

	resp = f.do(http.MethodDelete, members+"other", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp = f.do(http.MethodDelete, members+"other", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestShare_PolicyCreateEditGrant(t *testing.T) {
	f := newCeremonyFixture(t)
	require.Equal(t, http.StatusCreated, f.createAudience(f.ops, "colleagues").StatusCode)

	resp := f.doPolicy(http.MethodPost, "/api/v1/share/policies", colleaguesYAML, withBearer(f.other))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	resp = f.doPolicy(http.MethodPost, "/api/v1/share/policies", "name: bad\ndata: \"mention==person/x\"\n", withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, errBody(t, resp), "data:")
	resp = f.doPolicy(http.MethodPost, "/api/v1/share/policies", "", withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, errBody(t, resp), "document is required")

	resp = f.doPolicy(http.MethodPost, "/api/v1/share/policies", colleaguesYAML, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	pol := decodeBody[storage.SharePolicy](t, resp)
	assert.True(t, len(pol.ID) > 4 && pol.ID[:4] == "pol_", pol.ID)
	assert.Equal(t, "colleagues-network", pol.Name)
	assert.Equal(t, 1, pol.Version)
	assert.Contains(t, string(pol.Spec), `"type==contact;tag=in=(work,client)"`)

	resp = f.doPolicy(http.MethodPost, "/api/v1/share/policies", colleaguesYAML, withBearer(f.ops))
	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	// Edit bumps the version; the document keeps the policy's name.
	edited := `name: colleagues-network
data: "type==contact"
capabilities: {search: [fts]}
`
	resp = f.doPolicy(http.MethodPut, "/api/v1/share/policies/colleagues-network", edited, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	pol2 := decodeBody[storage.SharePolicy](t, resp)
	assert.Equal(t, pol.ID, pol2.ID)
	assert.Equal(t, 2, pol2.Version)
	assert.Contains(t, string(pol2.Spec), `"type==contact"`)

	resp = f.doPolicy(http.MethodPut, "/api/v1/share/policies/"+pol.ID, "name: renamed\ndata: \"type==contact\"\n", withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, errBody(t, resp), "name:")
	resp = f.doPolicy(http.MethodPut, "/api/v1/share/policies/none", edited, withBearer(f.ops))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = f.do(http.MethodGet, "/api/v1/share/policies", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, decodeBody[listOf[storage.SharePolicy]](t, resp).Data, 1)
	resp = f.do(http.MethodGet, "/api/v1/share/policies/"+pol.ID, nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 2, decodeBody[storage.SharePolicy](t, resp).Version)

	// Grants: by name, with an expiry, naming what they bind.
	resp = f.do(http.MethodPost, "/api/v1/share/grants", map[string]any{"policy": "colleagues-network"}, withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, errBody(t, resp), "audience")
	resp = f.do(http.MethodPost, "/api/v1/share/grants", map[string]any{"audience": "colleagues", "policy": "none"}, withBearer(f.ops))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Contains(t, errBody(t, resp), "policy")

	resp = f.do(http.MethodPost, "/api/v1/share/grants",
		map[string]any{"audience": "colleagues", "policy": "colleagues-network", "expires_in": 3600}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	g := decodeBody[grantRecord](t, resp)
	assert.True(t, len(g.ID) > 4 && g.ID[:4] == "grt_", g.ID)
	assert.Equal(t, "colleagues", g.Audience)
	assert.Equal(t, "colleagues-network", g.Policy)
	assert.Equal(t, 2, g.PolicyVersion)
	assert.Equal(t, storage.OwnerPrincipal, g.CreatedBy)
	require.NotNil(t, g.ExpiresAt)

	// Active grants of a member: none before joining, the grant after.
	resp = f.do(http.MethodGet, "/api/v1/share/grants?principal=other", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, decodeBody[listOf[grantRecord]](t, resp).Data)
	resp = f.do(http.MethodPut, "/api/v1/share/audiences/colleagues/members/other", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	resp = f.do(http.MethodGet, "/api/v1/share/grants?principal=other", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	active := decodeBody[listOf[grantRecord]](t, resp)
	require.Len(t, active.Data, 1)
	assert.Equal(t, g.ID, active.Data[0].ID)
	assert.Equal(t, "colleagues-network", active.Data[0].Policy)

	resp = f.do(http.MethodGet, "/api/v1/share/grants?audience=colleagues", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, decodeBody[listOf[grantRecord]](t, resp).Data, 1)

	// Revoke: gone from the default listing and from the member's view.
	resp = f.do(http.MethodDelete, "/api/v1/share/grants/"+g.ID, nil, withBearer(f.ops))
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp = f.do(http.MethodDelete, "/api/v1/share/grants/grt_none", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp = f.do(http.MethodGet, "/api/v1/share/grants", nil, withBearer(f.ops))
	assert.Empty(t, decodeBody[listOf[grantRecord]](t, resp).Data)
	resp = f.do(http.MethodGet, "/api/v1/share/grants?include_revoked=true", nil, withBearer(f.ops))
	revoked := decodeBody[listOf[grantRecord]](t, resp)
	require.Len(t, revoked.Data, 1)
	assert.NotNil(t, revoked.Data[0].RevokedAt)
	resp = f.do(http.MethodGet, "/api/v1/share/grants?principal=other", nil, withBearer(f.ops))
	assert.Empty(t, decodeBody[listOf[grantRecord]](t, resp).Data)
}

// A template installs once at version 1; a second install is refused,
// so an edited policy is never reset by an install.
func TestShare_TemplateInstallsOnce(t *testing.T) {
	f := newCeremonyFixture(t)

	resp := f.do(http.MethodGet, "/api/v1/share/templates", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	tpls := decodeBody[listOf[templateRecord]](t, resp)
	require.Len(t, tpls.Data, 2)
	assert.Equal(t, sharing.TemplateColleagues, tpls.Data[0].Name)
	assert.Equal(t, sharing.TemplatePartners, tpls.Data[1].Name)
	assert.Equal(t, "type==contact", tpls.Data[1].Spec.Data)

	resp = f.do(http.MethodPost, "/api/v1/share/templates/"+sharing.TemplatePartners, nil, withBearer(f.other))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	resp = f.do(http.MethodPost, "/api/v1/share/templates/nope", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = f.do(http.MethodPost, "/api/v1/share/templates/"+sharing.TemplatePartners, nil, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	pol := decodeBody[storage.SharePolicy](t, resp)
	assert.Equal(t, sharing.TemplatePartners, pol.Name)
	assert.Equal(t, 1, pol.Version)
	p, err := sharing.PolicyFromSpec(&pol)
	require.NoError(t, err)
	assert.Equal(t, []string{"personal"}, p.ExcludeClasses)

	resp = f.do(http.MethodPost, "/api/v1/share/templates/"+sharing.TemplatePartners, nil, withBearer(f.ops))
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, CodeAlreadyExists, errCode(t, resp))
}

func TestShare_CeilingAndKeyPolicy(t *testing.T) {
	f := newCeremonyFixture(t)
	ceiling := "/api/v1/principals/other/ceiling"

	resp := f.do(http.MethodGet, ceiling, nil, withBearer(f.other))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	resp = f.do(http.MethodGet, ceiling, nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, ceilingRecord{PrincipalID: "other", DenyClasses: []string{}}, decodeBody[ceilingRecord](t, resp))

	resp = f.do(http.MethodPut, ceiling, map[string]any{"deny_classes": []string{"personal", "Bad!"}}, withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, errBody(t, resp), "deny_classes:")
	resp = f.do(http.MethodPut, "/api/v1/principals/prn_nobody/ceiling", map[string]any{"deny_classes": []string{"personal"}}, withBearer(f.ops))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = f.do(http.MethodPut, ceiling, map[string]any{"deny_classes": []string{"personal", "financial", "personal"}}, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, []string{"financial", "personal"}, decodeBody[ceilingRecord](t, resp).DenyClasses)
	got, err := f.bundle.svc.Store(ownerCtx()).Sharing().DenyClasses(ownerCtx(), "other")
	require.NoError(t, err)
	assert.Equal(t, []string{"financial", "personal"}, got)

	resp = f.do(http.MethodPut, ceiling, map[string]any{"deny_classes": []string{}}, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, decodeBody[ceilingRecord](t, resp).DenyClasses)

	// A key narrows to a policy: its own holder binds it under
	// write:self, admin:shares binds anyone's but the owner's.
	resp = f.do(http.MethodPost, "/api/v1/share/templates/"+sharing.TemplatePartners, nil, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	pol := decodeBody[storage.SharePolicy](t, resp)
	otherKey := f.keyOf("other")

	resp = f.do(http.MethodPut, "/api/v1/self/keys/"+otherKey+"/policy", map[string]any{"policy": "none"}, withBearer(f.other))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp = f.do(http.MethodPut, "/api/v1/self/keys/"+otherKey+"/policy", map[string]any{"policy": pol.Name}, withBearer(f.other))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	bound := decodeBody[keyPolicyRecord](t, resp)
	assert.Equal(t, otherKey, bound.KeyID)
	assert.Equal(t, pol.ID, bound.PolicyID)
	assert.Equal(t, pol.Name, bound.Policy)
	id, err := f.bundle.svc.Store(ownerCtx()).Sharing().IdentityPolicy(ownerCtx(), bound.IdentityID)
	require.NoError(t, err)
	assert.Equal(t, pol.ID, id)

	resp = f.do(http.MethodPut, "/api/v1/self/keys/"+otherKey+"/policy", map[string]any{"policy": ""}, withBearer(f.ops))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, CodeNotSelf, errCode(t, resp))
	resp = f.do(http.MethodPut, "/api/v1/share/keys/"+otherKey+"/policy", map[string]any{"policy": ""}, withBearer(f.other))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, CodeInsufficientScope, errCode(t, resp))

	resp = f.do(http.MethodPut, "/api/v1/share/keys/"+otherKey+"/policy", map[string]any{"policy": ""}, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	assert.Empty(t, decodeBody[keyPolicyRecord](t, resp).PolicyID)
	id, err = f.bundle.svc.Store(ownerCtx()).Sharing().IdentityPolicy(ownerCtx(), bound.IdentityID)
	require.NoError(t, err)
	assert.Empty(t, id)

	_, dana := f.adminKey("Dana")
	resp = f.do(http.MethodPut, "/api/v1/share/keys/"+f.keyOf(storage.OwnerPrincipal)+"/policy", map[string]any{"policy": pol.Name}, withBearer(dana))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, CodeOwnerProtected, errCode(t, resp))
	resp = f.do(http.MethodPut, "/api/v1/share/keys/key_none/policy", map[string]any{"policy": pol.Name}, withBearer(f.ops))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// Every sharing route is admin:shares, so a reader is refused on each;
// the one exception is a key's own binding under write:self. The table
// half pins the scope of every route under /share and the ceiling, so
// a route re-scoped by mistake fails here by name.
func TestShare_EveryRouteIsAdminShares(t *testing.T) {
	var found int
	for _, rt := range APIRouteSpecs() {
		under := strings.HasPrefix(rt.Pattern, "/share/") || rt.Pattern == "/principals/{id}/ceiling"
		// The read audit sits under admin:audit (ADR-080 §11).
		if !under || rt.Pattern == "/share/audit" {
			continue
		}
		found++
		assert.Equal(t, authn.ScopeAdminShares, rt.Scope, "%s %s", rt.Method, rt.Pattern)
	}
	assert.GreaterOrEqual(t, found, 18, "the sharing routes")

	f := newCeremonyFixture(t)
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/share/audiences"}, {http.MethodPost, "/api/v1/share/audiences"},
		{http.MethodGet, "/api/v1/share/audiences/x"},
		{http.MethodPut, "/api/v1/share/audiences/x/members/other"}, {http.MethodDelete, "/api/v1/share/audiences/x/members/other"},
		{http.MethodGet, "/api/v1/share/policies"}, {http.MethodPost, "/api/v1/share/policies"},
		{http.MethodGet, "/api/v1/share/policies/x"}, {http.MethodPut, "/api/v1/share/policies/x"},
		{http.MethodGet, "/api/v1/share/templates"}, {http.MethodPost, "/api/v1/share/templates/x"},
		{http.MethodGet, "/api/v1/share/grants"}, {http.MethodPost, "/api/v1/share/grants"},
		{http.MethodDelete, "/api/v1/share/grants/x"},
		{http.MethodGet, "/api/v1/principals/other/ceiling"}, {http.MethodPut, "/api/v1/principals/other/ceiling"},
		{http.MethodPut, "/api/v1/share/keys/x/policy"},
		{http.MethodPost, "/api/v1/share/invites"},
	} {
		resp := f.do(call.method, call.path, map[string]any{}, withBearer(f.other))
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s as a reader", call.method, call.path)
		assert.Equal(t, CodeInsufficientScope, errCode(t, resp), "%s %s", call.method, call.path)
	}
}
