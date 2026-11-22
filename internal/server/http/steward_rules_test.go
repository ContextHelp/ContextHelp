package http

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// A grant that fails a check cannot be elevated into: the refusal
// carries the resolution code and nothing is stored.
func stewardFlowElevateRefused(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	require.Equal(t, http.StatusOK, f.do(sfOwner, http.MethodPost, "/api/v1/steward/grants/"+grant+"/revoke", nil).StatusCode)
	resp := f.elevate(sfSteward, grant)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "STEWARD_GRANT_REVOKED", errCode(t, resp))
	assert.Empty(t, f.session(sfSteward).StewardGrantID)

	expired := f.grant()
	f.clock.advance(49 * time.Hour)
	resp = f.elevate(sfSteward, expired)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "STEWARD_GRANT_EXPIRED", errCode(t, resp))
	assert.Empty(t, f.session(sfSteward).StewardGrantID)
}

// A session elevates into one grant at a time, and only the steward
// elevates: a requester who sees the grant is refused, a stranger is
// told nothing exists.
func stewardFlowElevateRules(t *testing.T, f *stewardFlow) {
	first, second := f.grant(), f.grant()
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, first).StatusCode)
	resp := f.elevate(sfSteward, second)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, codeElevationActive, errCode(t, resp))
	assert.Equal(t, first, f.session(sfSteward).StewardGrantID)
	assert.Equal(t, http.StatusOK, f.elevate(sfSteward, first).StatusCode, "the same grant again refreshes it")

	resp = f.elevate(sfRequester, second)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "the requester sees the grant but is not its steward")
	assert.Equal(t, "STEWARD_NOT_STEWARD", errCode(t, resp))
	resp = f.elevate(sfDual, second)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "an approver sees the grant but is not its steward")
	assert.Equal(t, "STEWARD_NOT_STEWARD", errCode(t, resp))
	assert.Equal(t, http.StatusNotFound, f.elevate(sfOther, second).StatusCode, "a principal that cannot see it")
	assert.Equal(t, http.StatusNotFound, f.elevate(sfSteward, "stg_nothing").StatusCode)
	resp = f.elevate(sfPlain, second)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "no request:steward")
	assert.Equal(t, CodeInsufficientScope, errCode(t, resp))
}

// Its steward, its requester, a holder of approve:steward and the owner
// see a grant and may revoke it; anyone else is told it does not
// exist.
func stewardFlowVisibility(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	for _, who := range []string{sfSteward, sfRequester, sfApprover, sfDual, sfOwner} {
		resp := f.do(who, http.MethodGet, "/api/v1/steward/grants/"+grant, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", who, errBody(t, resp))
		rec := decodeBody[stewardGrantRecord](t, resp)
		assert.Equal(t, grant, rec.ID, who)
		assert.Equal(t, storage.StewardStateActive, rec.State, who)
		assert.Equal(t, sfRequester, rec.RequestedBy, who)
		assert.Equal(t, sfSteward, rec.StewardID, who)
		assert.Equal(t, rec.ExpiresAt, rec.EffectiveExpiresAt, who)

		resp = f.do(who, http.MethodGet, "/api/v1/steward/grants", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, who)
		list := decodeBody[struct {
			Data  []stewardGrantRecord `json:"data"`
			Total int                  `json:"total"`
		}](t, resp)
		require.Len(t, list.Data, 1, who)
		assert.Equal(t, grant, list.Data[0].ID, who)
	}
	resp := f.do(sfPlain, http.MethodGet, "/api/v1/steward/grants/"+grant, nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp = f.do(sfPlain, http.MethodGet, "/api/v1/steward/grants", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotContains(t, string(mustRead(t, resp)), grant)
	resp = f.do(sfPlain, http.MethodPost, "/api/v1/steward/grants/"+grant+"/revoke", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "a stranger cannot revoke")

	for q, want := range map[string]int{"?state=active": 1, "?state=revoked": 0, "?steward=" + sfSteward: 1, "?steward=" + sfPlain: 0, "?limit=1": 1} {
		resp := f.do(sfOwner, http.MethodGet, "/api/v1/steward/grants"+q, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, q)
		assert.Equal(t, want, decodeBody[struct {
			Total int `json:"total"`
		}](t, resp).Total, q)
	}
	for _, q := range []string{"?state=pending", "?limit=0", "?grant=" + grant, "?state=active&state=revoked"} {
		resp := f.do(sfOwner, http.MethodGet, "/api/v1/steward/grants"+q, nil)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, q)
	}

	resp = f.do(sfSteward, http.MethodPost, "/api/v1/steward/grants/"+grant+"/revoke", map[string]string{"reason": "done early"})
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	rec := decodeBody[stewardGrantRecord](t, resp)
	assert.Equal(t, storage.StewardStateRevoked, rec.State)
	assert.Equal(t, sfSteward, rec.RevokedBy)
	assert.Equal(t, "done early", rec.RevokeReason)
	resp = f.do(sfOwner, http.MethodPost, "/api/v1/steward/grants/"+grant+"/revoke", nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, codeGrantClosed, errCode(t, resp))
}

// A filing the kind or the mechanism refuses answers its stable code
// with its status; a body with an unknown key is refused before it.
func stewardFlowFilingRefusals(t *testing.T, f *stewardFlow) {
	with := func(k string, v any) map[string]any {
		body := f.filing()
		if v == nil {
			delete(body, k)
		} else {
			body[k] = v
		}
		return body
	}
	now := f.clock.now()
	for name, c := range map[string]struct {
		who    string
		body   map[string]any
		status int
		code   string
	}{
		"the owner files nothing":       {sfOwner, f.filing(), http.StatusForbidden, approval.CodeOwnerCannotRequest},
		"a steward without the role":    {sfRequester, with("steward_id", sfPlain), http.StatusBadRequest, steward.CodeStewardIneligible},
		"a short reason":                {sfRequester, with("reason", "because"), http.StatusBadRequest, approval.CodeReasonTooShort},
		"a window past the cap":         {sfRequester, with("expires_at", now.Add(15*24*time.Hour)), http.StatusBadRequest, steward.CodeWindowTooLong},
		"no window end":                 {sfRequester, with("expires_at", nil), http.StatusBadRequest, steward.CodeWindowInvalid},
		"a scope and an audience":       {sfRequester, with("as_audience", "partners"), http.StatusBadRequest, steward.CodeScopeInvalid},
		"a scope asking for masking":    {sfRequester, with("scope", json.RawMessage(`{"data":"type==contact","redact":{"mask":["email"]}}`)), http.StatusBadRequest, steward.CodeScopeInvalid},
		"a renewal of nothing":          {sfRequester, with("renews_id", "stg_nothing"), http.StatusBadRequest, steward.CodeRenewalInvalid},
		"an unknown key":                {sfRequester, with("steward_grant_id", "stg_x"), http.StatusBadRequest, "INVALID_REQUEST"},
		"no request:steward on the way": {sfApprover, f.filing(), http.StatusForbidden, CodeInsufficientScope},
	} {
		resp := f.do(c.who, http.MethodPost, "/api/v1/steward/grants", c.body)
		assert.Equal(t, c.status, resp.StatusCode, name)
		assert.Equal(t, c.code, errCode(t, resp), name)
	}
	list, err := f.drv.Approvals().List(ownerCtx(), storage.ApprovalFilter{})
	require.NoError(t, err)
	assert.Empty(t, list, "nothing was filed")

	// The steward defaults to the caller.
	body := f.filing()
	delete(body, "steward_id")
	rec := f.file(sfSteward, body)
	assert.Equal(t, sfSteward, rec.SubjectID)
	assert.Equal(t, steward.KindName, rec.Kind)
	assert.Equal(t, approval.StateRequested, rec.State)
}

// The mechanism's separation holds for a steward grant over the API:
// the requester never decides its own request, the steward never
// decides its own grant, and a principal without approve:steward does
// not see the request at all.
func stewardFlowApprovalRules(t *testing.T, f *stewardFlow) {
	ownBody := f.filing()
	ownBody["steward_id"] = sfDual
	own := f.file(sfDual, ownBody)
	resp := f.approve(sfDual, own)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, approval.CodeRequesterCannotDecide, errCode(t, resp))

	forDual := f.file(sfRequester, ownBody)
	resp = f.approve(sfDual, forDual)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, approval.CodeBeneficiaryCannotDecide, errCode(t, resp))

	for _, who := range []string{sfPlain, sfSteward} {
		resp = f.do(who, http.MethodGet, "/api/v1/approvals/"+own.ID, nil)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, who)
		resp = f.approve(who, own)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, who)
	}
	resp = f.approve(sfApprover, own)
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, approval.StateApproved, decodeBody[decisionResponse](t, resp).State)
}
