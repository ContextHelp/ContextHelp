package http

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestStewardFlow(t *testing.T) { runStewardFlowSuite(t, storageutil.NewTestDriver) }

// runStewardFlowSuite runs the elevation lifecycle over the store
// newDriver opens.
func runStewardFlowSuite(t *testing.T, newDriver func(*testing.T) storage.StorageDriver) {
	for name, fn := range map[string]func(*testing.T, *stewardFlow){
		"request, approve, elevate, end":           stewardFlowElevateAndEnd,
		"idle ends the elevation, not a refusal":   stewardFlowIdle,
		"revoke between two requests":              stewardFlowRevoke,
		"off: every route is absent":               stewardFlowDisabled,
		"an API key cannot elevate":                stewardFlowKeyCannotElevate,
		"every transition is a security event":     stewardFlowEvents,
		"elevating a grant that cannot be used":    stewardFlowElevateRefused,
		"one elevation at a time, the steward's":   stewardFlowElevateRules,
		"who sees, who revokes":                    stewardFlowVisibility,
		"filing refusals answer their stable code": stewardFlowFilingRefusals,
		"the approval rules hold over the API":     stewardFlowApprovalRules,
		"nothing a client sends elevates":          stewardFlowInjection,
	} {
		t.Run(name, func(t *testing.T) { fn(t, newStewardFlow(t, newDriver)) })
	}
}

// Elevating makes the next request the grant's viewer, alone; ending it
// makes the next request the ordinary view again.
func stewardFlowElevateAndEnd(t *testing.T, f *stewardFlow) {
	code, ids := f.objects(sfSteward)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, ids, "the steward's ordinary view holds nothing")

	grant := f.grant()
	resp := f.elevate(sfSteward, grant)
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	el := decodeBody[elevationRecord](t, resp)
	assert.Equal(t, grant, el.GrantID)
	assert.Equal(t, "CASE-117", el.MatterRef)
	assert.Equal(t, f.clock.now(), el.ActiveAt)
	assert.Equal(t, el.ActiveAt.Add(15*time.Minute), el.IdleExpiresAt)
	assert.Equal(t, f.session(sfSteward).ID, el.SessionID)

	code, ids = f.objects(sfSteward)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{f.contact}, ids, "elevated: the grant's scope and nothing else")

	resp = f.do(sfSteward, http.MethodDelete, "/api/v1/steward/grants/"+grant+"/elevate", nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode, errBody(t, resp))
	code, ids = f.objects(sfSteward)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, ids, "ended: the ordinary view")
	assert.Empty(t, f.session(sfSteward).StewardGrantID)

	resp = f.do(sfSteward, http.MethodDelete, "/api/v1/steward/grants/"+grant+"/elevate", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "no elevation left to end")
}

// Idle past steward.elevation_idle, the next request is the ordinary
// view, not a refusal, and the elevation is gone.
func stewardFlowIdle(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)
	f.clock.advance(14 * time.Minute)
	_, ids := f.objects(sfSteward)
	assert.Equal(t, []string{f.contact}, ids, "inside the idle window")
	f.clock.advance(14 * time.Minute)
	_, ids = f.objects(sfSteward)
	assert.Equal(t, []string{f.contact}, ids, "each request restarts the window")

	f.clock.advance(15 * time.Minute)
	code, ids := f.objects(sfSteward)
	require.Equal(t, http.StatusOK, code, "idle is no refusal")
	assert.Empty(t, ids, "idle: the ordinary view")
	assert.Empty(t, f.session(sfSteward).StewardGrantID, "the elevation is cleared")
}

// A revocation between two requests of one elevated session refuses the
// second with the grant's code; the elevation is cleared and the
// session serves ordinary requests.
func stewardFlowRevoke(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)
	_, ids := f.objects(sfSteward)
	require.Equal(t, []string{f.contact}, ids)

	resp := f.do(sfOwner, http.MethodPost, "/api/v1/steward/grants/"+grant+"/revoke", map[string]string{"reason": "matter closed"})
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, storage.StewardStateRevoked, decodeBody[stewardGrantRecord](t, resp).State)

	resp = f.do(sfSteward, http.MethodGet, "/api/v1/objects", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "STEWARD_GRANT_REVOKED", errCode(t, resp))

	code, ids := f.objects(sfSteward)
	require.Equal(t, http.StatusOK, code, "the session survives the grant")
	assert.Empty(t, ids)
	assert.Empty(t, f.session(sfSteward).StewardGrantID)
	resp = f.do(sfSteward, http.MethodGet, "/api/v1/whoami", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// stewardPaths are the steward routes, each with a body it may take.
func stewardPaths(grant string) [][2]string {
	return [][2]string{
		{http.MethodPost, "/api/v1/steward/grants"},
		{http.MethodGet, "/api/v1/steward/grants"},
		{http.MethodGet, "/api/v1/steward/grants/" + grant},
		{http.MethodPost, "/api/v1/steward/grants/" + grant + "/revoke"},
		{http.MethodPost, "/api/v1/steward/grants/" + grant + "/elevate"},
		{http.MethodDelete, "/api/v1/steward/grants/" + grant + "/elevate"},
	}
}

// steward.enabled off: every steward route is absent, for every caller
// and whatever scope it holds; nothing is filed, no elevation starts,
// and a live one is refused at its next request and cleared.
func stewardFlowDisabled(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)
	filed, err := f.drv.Approvals().List(ownerCtx(), storage.ApprovalFilter{})
	require.NoError(t, err)

	f.settings.Enabled = false
	resp := f.do(sfSteward, http.MethodGet, "/api/v1/objects", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "STEWARD_STEWARD_DISABLED", errCode(t, resp))
	assert.Empty(t, f.session(sfSteward).StewardGrantID, "the refused elevation is cleared")

	for _, who := range []string{sfSteward, sfRequester, sfApprover, sfDual, sfPlain, sfOwner, sfDevice} {
		for _, rt := range stewardPaths(grant) {
			var body any
			if rt[0] == http.MethodPost {
				body = f.filing()
			}
			resp := f.do(who, rt[0], rt[1], body)
			assert.Equal(t, http.StatusNotFound, resp.StatusCode, "%s %s as %s", rt[0], rt[1], who)
			assert.Equal(t, CodeNotFound, errCode(t, resp), "%s %s as %s", rt[0], rt[1], who)
		}
	}
	after, err := f.drv.Approvals().List(ownerCtx(), storage.ApprovalFilter{})
	require.NoError(t, err)
	assert.Len(t, after, len(filed), "nothing was filed")
	assert.Empty(t, f.session(sfSteward).StewardGrantID, "no elevation started")
	code, ids := f.objects(sfSteward)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, ids)
}

// Only a browser session elevates: an API key, the owner's included,
// gets a stable refusal and no session is elevated.
func stewardFlowKeyCannotElevate(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	for _, who := range []string{sfOwner, sfDevice} {
		for _, method := range []string{http.MethodPost, http.MethodDelete} {
			resp := f.do(who, method, "/api/v1/steward/grants/"+grant+"/elevate", nil)
			assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s", method, who)
			assert.Equal(t, codeElevationBrowserOnly, errCode(t, resp), "%s %s", method, who)
		}
	}
	assert.Empty(t, f.session(sfSteward).StewardGrantID)
	assert.NotEmpty(t, f.alerts.of(security.EventStewardElevationRefused), "a refused elevation is an event")
}

// Every transition reaches the security emitter: the request filed and
// approved, the elevation started and ended, a refusal, the elevation
// ended by it, and the revocation.
func stewardFlowEvents(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)
	require.Equal(t, http.StatusNoContent, f.do(sfSteward, http.MethodDelete, "/api/v1/steward/grants/"+grant+"/elevate", nil).StatusCode)
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)
	require.Equal(t, http.StatusOK, f.do(sfApprover, http.MethodPost, "/api/v1/steward/grants/"+grant+"/revoke", nil).StatusCode)
	require.Equal(t, http.StatusForbidden, f.do(sfSteward, http.MethodGet, "/api/v1/objects", nil).StatusCode)

	assert.Equal(t, []string{
		security.EventStewardElevationEnded, security.EventStewardElevationEnded,
		security.EventStewardElevationRefused,
		security.EventStewardElevationStarted, security.EventStewardElevationStarted,
		security.EventStewardGrantRevoked,
		security.EventStewardRequestApproved, security.EventStewardRequestFiled,
	}, f.alerts.kinds(t, 8))
	ended := map[string]bool{}
	for _, a := range f.alerts.of(security.EventStewardElevationEnded) {
		ended[a.Meta["reason"]] = true
		assert.Equal(t, sfSteward, a.Principal)
	}
	assert.Equal(t, map[string]bool{"ended": true, "refused": true}, ended)
	refused := f.alerts.of(security.EventStewardElevationRefused)[0]
	assert.Equal(t, "grant_revoked", refused.Meta["reason"])
	assert.Equal(t, grant, refused.Meta["grant_id"])
	assert.Equal(t, sfApprover, f.alerts.of(security.EventStewardGrantRevoked)[0].Principal)
}
