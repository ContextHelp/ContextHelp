package http

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stewardInjections are the ways a client might try to name a grant on
// a request: headers, query parameters, a cookie. None is read.
func stewardInjections(grant string) map[string]reqOpt {
	return map[string]reqOpt{
		"X-Steward-Grant-Id header":   withHeader("X-Steward-Grant-Id", grant),
		"X-Ctxt-Steward-Grant header": withHeader("X-Ctxt-Steward-Grant", grant),
		"Steward-Grant-Id header":     withHeader("Steward-Grant-Id", grant),
		"steward_grant_id header":     withHeader("steward_grant_id", grant),
		"X-Ctxt-View-As header":       withHeader("X-Ctxt-View-As", grant),
		"steward_grant_id query":      withQuery("steward_grant_id", grant),
		"as_steward query":            withQuery("as_steward", grant),
		"steward_grant_id cookie":     withCookie(&http.Cookie{Name: "steward_grant_id", Value: grant}),
		"X-Steward header":            withHeader("X-Steward", "grant="+grant),
	}
}

// withQuery adds a query parameter.
func withQuery(k, v string) reqOpt {
	return func(r *http.Request) {
		q := r.URL.Query()
		q.Set(k, v)
		r.URL.RawQuery = q.Encode()
	}
}

// Nothing a client sends elevates a request: only the session row,
// written by the elevate route, names a grant. A steward whose session
// is not elevated, and a key holding request:steward, keep the ordinary
// view whatever header, parameter, cookie or body field they send.
func stewardFlowInjection(t *testing.T, f *stewardFlow) {
	grant := f.grant()
	for name, opt := range stewardInjections(grant) {
		for _, who := range []string{sfSteward, sfDevice} {
			code, ids := f.objects(who, opt)
			if code == http.StatusOK {
				assert.Empty(t, ids, "%s as %s elevated the request", name, who)
			} else {
				assert.NotEqual(t, http.StatusInternalServerError, code, "%s as %s", name, who)
			}
		}
	}
	for _, who := range []string{sfSteward, sfDevice} {
		resp := f.do(who, http.MethodPost, "/api/v1/find", map[string]any{"query": "Sarah", "steward_grant_id": grant})
		assert.NotContains(t, string(mustRead(t, resp)), f.contact, "a body field elevated %s's find", who)
	}
	assert.Empty(t, f.session(sfSteward).StewardGrantID)

	// Once elevated, a header naming no grant does not end it, nor does
	// one naming another grant move it.
	other := f.grant()
	require.Equal(t, http.StatusOK, f.elevate(sfSteward, grant).StatusCode)
	for name, opt := range stewardInjections(other) {
		code, ids := f.objects(sfSteward, opt)
		if code == http.StatusOK {
			assert.Equal(t, []string{f.contact}, ids, name)
		} else {
			assert.NotEqual(t, http.StatusInternalServerError, code, name)
		}
		assert.Equal(t, grant, f.session(sfSteward).StewardGrantID, "%s moved or ended the elevation", name)
	}
	_, ids := f.objects(sfSteward)
	assert.Equal(t, []string{f.contact}, ids)
}
