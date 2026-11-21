package leaktest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// An administrator cannot become a viewer it administers: it never
// receives a working credential for another principal, so it cannot
// read through that principal's grants what its own view hides. A key
// for the colleague, a sign-in link for the partner or a proxy login
// bound to the principal in both audiences would be exactly that; each
// is refused, carries no secret and leaves the target's credentials as
// they were.
func TestAdminCannotTakeAViewersCredential(t *testing.T) {
	adminCannotTakeAViewersCredential(t, storageutil.NewTestDriver)
}

func adminCannotTakeAViewersCredential(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	ctx := context.Background()
	auth := f.Server.Driver.Auth()
	state := func(id string) string {
		t.Helper()
		idns, err := auth.Identities().ListByPrincipal(ctx, id)
		require.NoError(t, err)
		return fmt.Sprint(len(idns))
	}
	n := 0
	for _, target := range []string{ColleagueID, PartnerID, BothID} {
		before := state(target)
		for _, p := range f.Admins() {
			for _, call := range []struct {
				name string
				path string
				body func() any
			}{
				{"key", "/principals/" + target + "/keys", func() any { return map[string]any{"name": "takeover"} }},
				{"link", "/principals/" + target + "/links", func() any { return nil }},
				{"identity", "/principals/" + target + "/identities", func() any {
					n++
					return map[string]any{"method": "proxy", "issuer": "tailscale", "subject": fmt.Sprintf("admin-%d@example.com", n)}
				}},
			} {
				label := fmt.Sprintf("%s: %s for %s", p.Name, call.name, target)
				r, err := f.HTTP(ctx, p, http.MethodPost, call.path, call.body())
				require.NoError(t, err)
				var env struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(r.Body, &env), "%s: %s", label, r.Body)
				var top map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(r.Body, &top), label)
				assert.Len(t, top, 1, "%s: the refusal carries only the error: %s", label, r.Body)
				assert.NotContains(t, string(r.Body), "#t=", label)
				want := authn.CodeCredentialForOtherPrincipal
				if p.Name == NameAdminSession {
					// A browser session holds no admin:principals.
					want = httpserver.CodeInsufficientScope
				}
				assert.Equal(t, http.StatusForbidden, r.Status, "%s: %s", label, r.Body)
				assert.Equal(t, want, env.Error.Code, label)
			}
		}
		assert.Equal(t, before, state(target), "%s: an administrator added a credential", target)
		keys, err := authn.NewKeysOver(auth, nil).ListByPrincipal(ctx, target)
		require.NoError(t, err)
		for _, k := range keys {
			assert.NotEqual(t, "takeover", k.Name, "%s holds the administrator's key", target)
		}
	}
	// An invite into the audience the colleague is in would make the
	// redeemer a member of it: no administrator mints one, the browser
	// session holding admin:shares included.
	audiences, err := f.Server.Driver.Sharing().Audiences().List(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, audiences)
	for _, p := range f.Admins() {
		for _, body := range []map[string]any{
			{"display_name": "takeover"},
			{"display_name": "takeover", "audience": audiences[0].ID},
		} {
			r, err := f.HTTP(ctx, p, http.MethodPost, "/share/invites", body)
			require.NoError(t, err)
			assert.Equal(t, http.StatusForbidden, r.Status, "%s invite %v: %s", p.Name, body, r.Body)
			assert.Contains(t, string(r.Body), authn.CodeCredentialForOtherPrincipal, "%s invite %v", p.Name, body)
			assert.NotContains(t, string(r.Body), "#t=", "%s invite %v", p.Name, body)
		}
	}
	// Each refusal is a denied receipt in the read audit: route_access
	// failed with the stable reason credential_owner_only.
	require.NoError(t, f.Server.Stack.Audit.Flush(ctx))
	for _, p := range f.Admins() {
		entries, err := f.Server.Driver.Sharing().AccessLog().Query(ctx, storage.AccessFilter{PrincipalID: p.ID})
		require.NoError(t, err)
		refused := 0
		for _, e := range entries {
			for _, c := range e.Checks {
				if c.Check == sharing.CheckRouteAccess && c.Reason == sharing.ReasonCredentialOwnerOnly {
					assert.Equal(t, storage.AccessDecisionDenied, e.Decision, "%s %s", p.Name, e.Route)
					assert.Equal(t, storage.AccessOutcomeFail, c.Outcome, "%s %s", p.Name, e.Route)
					refused++
				}
			}
		}
		if p.Name == NameAdminSession {
			// A session holds no admin:principals: only its invites reach the guard.
			assert.NotZero(t, refused, "%s: invite refusals are in the audit", p.Name)
			continue
		}
		assert.NotZero(t, refused, "%s: credential refusals are in the audit", p.Name)
	}
}
