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
// change is never silent, whoever made it. A change that widens
// anyone's view (a membership, a grant) is on the record first as the
// administrator's request, and as the change itself, still the
// administrator's, once an approver accepts it (ADR-083 §10 B).
func TestAdminSharingChangesAreAudited(t *testing.T) {
	adminSharingChangesAreAudited(t, storageutil.NewTestDriver)
}

func adminSharingChangesAreAudited(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	ctx := context.Background()
	for _, p := range f.Admins() {
		send := func(who Principal, method, path string, body any) Result {
			t.Helper()
			r, err := f.HTTP(ctx, who, method, path, body)
			require.NoError(t, err)
			return r
		}
		// pend sends a widening change as the administrator, which must
		// be filed, and has the owner approve it.
		pend := func(method, path string, body any) {
			t.Helper()
			r := send(p, method, path, body)
			require.Equal(t, http.StatusAccepted, r.Status, "%s %s %s: %s", p.Name, method, path, r.Body)
			var req struct {
				ID            string `json:"id"`
				PayloadDigest string `json:"payload_digest"`
			}
			require.NoError(t, json.Unmarshal(r.Body, &req))
			done := send(f.Owner(), http.MethodPost, "/approvals/"+req.ID+"/approve", map[string]string{"payload_digest": req.PayloadDigest})
			require.Equal(t, http.StatusOK, done.Status, "%s: the owner approves %s: %s", p.Name, req.ID, done.Body)
		}
		aud := "audit-" + p.Name
		created := send(p, http.MethodPost, "/share/audiences", map[string]any{"name": aud})
		require.Equal(t, http.StatusCreated, created.Status, "%s: %s", p.Name, created.Body)
		pend(http.MethodPut, "/share/audiences/"+aud+"/members/"+p.ID, map[string]any{})
		policyName := "audit-policy-" + p.Name
		policy := send(p, http.MethodPost, "/share/policies", map[string]any{"document": "name: " + policyName + "\nversion: 1\ndata: \"type==contact\"\nfields:\n  contact: [title]\ncapabilities:\n  search: [fts]\n"})
		require.Equal(t, http.StatusCreated, policy.Status, "%s: %s", p.Name, policy.Body)
		pend(http.MethodPost, "/share/grants", map[string]any{"audience": aud, "policy": policyName})
		grants := fetch(t, f, f.Owner(), "/share/grants?audience="+aud)
		require.Equal(t, http.StatusOK, grants.Status, "%s", grants.Body)
		var listed struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(grants.Body, &listed))
		require.Len(t, listed.Data, 1, "%s: the approved grant exists", p.Name)
		granted := listed.Data[0]
		ceiling := send(p, http.MethodPut, "/principals/"+p.ID+"/ceiling", map[string]any{"deny_classes": []string{}})
		require.Equal(t, http.StatusOK, ceiling.Status, "%s: %s", p.Name, ceiling.Body)
		revoked := send(p, http.MethodDelete, "/share/grants/"+granted.ID, nil)
		require.Equal(t, http.StatusNoContent, revoked.Status, "%s: %s", p.Name, revoked.Body)
		removed := send(p, http.MethodDelete, "/share/audiences/"+aud+"/members/"+p.ID, nil)
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
			"share.ceiling.set", "share.grant.revoked", "share.member.removed", "approval.request.filed",
		} {
			assert.Contains(t, seen, event, "%s: the owner's audit log lacks %s", p.Name, event)
		}
		assert.Equal(t, p.ID, seen["share.member.added"].Payload["principal_id"], p.Name)
		assert.Equal(t, granted.ID, seen["share.grant.created"].Payload["grant_id"], p.Name)
		assert.Equal(t, policyName, seen["share.grant.created"].Payload["policy"], p.Name)
		assert.Equal(t, aud, seen["share.grant.created"].Payload["audience"], p.Name)
	}
}
