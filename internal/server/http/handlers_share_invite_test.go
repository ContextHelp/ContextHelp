package http

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// An invite minted into an audience lands the person in it; whoami
// then reports who shares with them once a grant applies.
func TestShare_InviteIntoAudienceAndWhoamiShared(t *testing.T) {
	f := newCeremonyFixture(t)
	require.Equal(t, http.StatusCreated, f.createAudience(f.ops, "partners").StatusCode)

	resp := f.do(http.MethodPost, "/api/v1/share/invites", map[string]any{"display_name": "Bob", "audience": "nope"}, withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, errBody(t, resp), "audience")

	resp = f.do(http.MethodPost, "/api/v1/share/invites", map[string]any{"display_name": "Bob", "audience": "partners"}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	link := decodeBody[linkResponse](t, resp)
	resp = f.redeemInvite(secretOf(t, link.URL))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	cookie, signed := sessionCookieOf(t, resp), decodeBody[exchangeResponse](t, resp)

	resp = f.do(http.MethodGet, "/api/v1/share/audiences/partners", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	members := decodeBody[audienceRecord](t, resp).Members
	require.Len(t, members, 1)
	assert.Equal(t, signed.Principal, members[0].PrincipalID)
	assert.Equal(t, storage.OwnerPrincipal, members[0].AddedBy)

	// No grant yet: nothing is shared. A reader's session holds no
	// admin:shares, so the sharing routes refuse it.
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(cookie), fromUI)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Nil(t, decodeBody[whoamiResponse](t, resp).Shared)
	resp = f.do(http.MethodGet, "/api/v1/share/audiences", nil, withCookie(cookie), fromUI)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	resp = f.do(http.MethodPost, "/api/v1/share/templates/"+sharing.TemplatePartners, nil, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp = f.do(http.MethodPost, "/api/v1/share/grants", map[string]any{"audience": "partners", "policy": sharing.TemplatePartners}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))

	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(cookie), fromUI)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	who := decodeBody[whoamiResponse](t, resp)
	require.NotNil(t, who.Shared)
	assert.NotEmpty(t, who.Shared.Owner)
	assert.Equal(t, []string{sharing.TemplatePartners}, who.Shared.Policies)

	// The owner is never "shared with"; the owner's session reaches the
	// Sharing page.
	owner := f.signIn()
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(owner), fromUI)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Nil(t, decodeBody[whoamiResponse](t, resp).Shared)
	resp = f.do(http.MethodGet, "/api/v1/share/audiences", nil, withCookie(owner), fromUI)
	assert.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	resp = f.do(http.MethodPost, "/api/v1/share/audiences", map[string]any{"name": "colleagues"}, withCookie(owner), fromUI)
	assert.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
}
