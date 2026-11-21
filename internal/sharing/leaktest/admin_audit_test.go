package leaktest

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Changes to who can see what are the owner's to review (ADR-080 §9):
// every audience, membership, policy, grant, ceiling, key binding and
// invite change an administrator makes leaves an entry in the audit
// log naming the administrator, and only the owner reads that log. A
// change is never silent, whoever made it, so an administrator
// granting a viewer (itself included) a view is visible after the fact.
func TestAdminSharingChangesAreAudited(t *testing.T) {
	adminSharingChangesAreAudited(t, storageutil.NewTestDriver)
}

func adminSharingChangesAreAudited(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	ctx := context.Background()
	for _, p := range f.Admins() {
		send := func(method, path string, body any) Result {
			t.Helper()
			r, err := f.HTTP(ctx, p, method, path, body)
			require.NoError(t, err)
			return r
		}
		aud := "audit-" + p.Name
		created := send(http.MethodPost, "/share/audiences", map[string]any{"name": aud})
		require.Equal(t, http.StatusCreated, created.Status, "%s: %s", p.Name, created.Body)
		member := send(http.MethodPut, "/share/audiences/"+aud+"/members/"+p.ID, map[string]any{})
		require.Equal(t, http.StatusOK, member.Status, "%s: %s", p.Name, member.Body)
		policyName := "audit-policy-" + p.Name
		policy := send(http.MethodPost, "/share/policies", map[string]any{"document": "name: " + policyName + "\nversion: 1\ndata: \"type==contact\"\nfields:\n  contact: [title]\ncapabilities:\n  search: [fts]\n"})
		require.Equal(t, http.StatusCreated, policy.Status, "%s: %s", p.Name, policy.Body)
		grant := send(http.MethodPost, "/share/grants", map[string]any{"audience": aud, "policy": policyName})
		require.Equal(t, http.StatusCreated, grant.Status, "%s: %s", p.Name, grant.Body)
		var granted struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(grant.Body, &granted))
		ceiling := send(http.MethodPut, "/principals/"+p.ID+"/ceiling", map[string]any{"deny_classes": []string{}})
		require.Equal(t, http.StatusOK, ceiling.Status, "%s: %s", p.Name, ceiling.Body)
		revoked := send(http.MethodDelete, "/share/grants/"+granted.ID, nil)
		require.Equal(t, http.StatusNoContent, revoked.Status, "%s: %s", p.Name, revoked.Body)
		removed := send(http.MethodDelete, "/share/audiences/"+aud+"/members/"+p.ID, nil)
		require.Equal(t, http.StatusNoContent, removed.Status, "%s: %s", p.Name, removed.Body)

		// The owner reads who did what; the entries name the administrator.
		log := fetch(t, f, f.Owner(), "/audit-log?limit=500&actor="+p.ID)
		require.Equal(t, http.StatusOK, log.Status, "%s", log.Body)
		var out struct {
			Data []storage.AuditEntry `json:"data"`
		}
		require.NoError(t, json.Unmarshal(log.Body, &out))
		seen := map[string]storage.AuditEntry{}
		for _, e := range out.Data {
			assert.Equal(t, p.ID, e.Actor)
			seen[e.EventType] = e
		}
		for _, event := range []string{
			"share.audience.created", "share.member.added", "share.policy.created", "share.grant.created",
			"share.ceiling.set", "share.grant.revoked", "share.member.removed",
		} {
			assert.Contains(t, seen, event, "%s: the owner's audit log lacks %s", p.Name, event)
		}
		assert.Equal(t, p.ID, seen["share.member.added"].Payload["principal_id"], p.Name)
		assert.Equal(t, granted.ID, seen["share.grant.created"].Payload["grant_id"], p.Name)
		assert.Equal(t, policyName, seen["share.grant.created"].Payload["policy"], p.Name)
		assert.Equal(t, aud, seen["share.grant.created"].Payload["audience"], p.Name)
	}
}
