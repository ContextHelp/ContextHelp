package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Every mutating route of the principal registry, the sharing registry
// and a principal's own credentials carries a class, and every class
// names a route: a registry write route lands with its class or the
// build fails (ADR-083 §10 B, implementation step 3).
func TestRegistryWrites_EveryRegistryWriteRouteIsClassified(t *testing.T) {
	writes := map[string]bool{}
	for _, rt := range APIRouteSpecs() {
		if rt.Method == http.MethodGet || rt.Method == http.MethodHead || rt.Method == http.MethodOptions {
			continue
		}
		for _, prefix := range []string{"/principals", "/share/", "/self/"} {
			if strings.HasPrefix(rt.Pattern, prefix) {
				writes[routeKey(rt.Method, rt.Pattern)] = true
			}
		}
	}
	require.NotEmpty(t, writes)
	for key := range writes {
		_, ok := registryWrites[key]
		assert.True(t, ok, "%s is a registry write route with no class in registryWrites", key)
	}
	for key, w := range registryWrites {
		assert.True(t, writes[key], "registryWrites names %s, which is no registry write route", key)
		switch w.kind {
		case writeShareChange, writeOwnBinding:
			assert.NotEmpty(t, w.action, "%s: a route that submits a change names its action", key)
		default:
			assert.Empty(t, w.action, "%s: only a route that submits a change has an action", key)
		}
	}
}

// Every action of the share.change kind belongs to exactly one route
// that classifies it, except the key binding, which a principal's own
// route also makes: no action is left without a route, none is claimed
// twice by the administrators' routes.
func TestRegistryWrites_EveryActionHasOneAdministratorRoute(t *testing.T) {
	routes := map[string][]string{}
	for key, w := range registryWrites {
		if w.kind == writeShareChange {
			routes[w.action] = append(routes[w.action], key)
		}
	}
	for _, action := range sharechange.Actions {
		assert.Len(t, routes[action], 1, "%s: routes %v", action, routes[action])
	}
	assert.Len(t, routes, len(sharechange.Actions))
}

// A handler that submits a change its route's class does not name is a
// programming error: refused with a 500 before anything is written.
func TestSubmit_RefusesAChangeThatIsNotTheRoutes(t *testing.T) {
	s := &shareRoutes{}
	for _, c := range []struct {
		name  string
		route string
		c     sharing.Change
	}{
		{"unknown route", "POST /share/nothing", sharing.AudienceCreate{Name: "x"}},
		{"another action", routeCreateGrant, sharing.AudienceCreate{Name: "x"}},
		{"a credential route", "POST /principals/{id}/keys", sharing.AudienceCreate{Name: "x"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			_, ok := s.submit(rec, req, c.route, c.c)
			assert.False(t, ok)
			assert.Equal(t, http.StatusInternalServerError, rec.Code)
		})
	}
}

// A change that waits for an approver on an instance with no approval
// mechanism is refused (501) and says why.
func TestWriteChangeError_MapsTheOutcomes(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{sharechange.ErrNoApprovals, http.StatusNotImplemented, "NOT_IMPLEMENTED"},
		{&sharechange.Error{What: "audience", Err: storage.ErrAlreadyExists}, http.StatusConflict, CodeAlreadyExists},
		{&sharechange.Error{What: "grant", Err: storage.ErrNotFound}, http.StatusNotFound, CodeNotFound},
		{&approval.Error{Code: approval.CodeInvalid, Message: "no such policy"}, http.StatusBadRequest, approval.CodeInvalid},
		{errors.New("disk on fire"), http.StatusInternalServerError, "INTERNAL_ERROR"},
	} {
		rec := httptest.NewRecorder()
		writeChangeError(rec, httptest.NewRequest(http.MethodPut, "/", nil).WithContext(context.Background()), c.err)
		assert.Equal(t, c.status, rec.Code, "%v", c.err)
		assert.Contains(t, rec.Body.String(), c.code, "%v", c.err)
		assert.NotContains(t, rec.Body.String(), "disk on fire", "a failure names no cause")
	}
}

// Each row of the route table answers a non-owner administrator as
// ADR-083 §10 B says, over both kinds of administrator: widening is 202
// with the request and a Location, narrowing and neutral changes are
// applied at once exactly as before.
func TestShareRoutes_RowsClassifyAsTheTableSays(t *testing.T) {
	for _, row := range httpRows() {
		for _, who := range []string{"key", "session"} {
			t.Run(row.name+"/"+who, func(t *testing.T) {
				f := newShareFx(t)
				if row.setup != nil {
					row.setup(f)
				}
				caller := f.admin
				if who == "session" {
					caller = f.sess
				}
				before := f.dump()
				filed := f.count("approval.request.filed")
				method, path, body := row.call(f)
				resp := f.callAs(caller, method, "/api/v1"+path, body)
				require.Equal(t, row.adminStatus, resp.StatusCode, "%s %s: %s", method, path, errBody(t, resp))

				if !row.pending() {
					if !row.unchanged {
						assert.NotEqual(t, before, f.dump(), "an applied change changed the registry")
					}
					assert.Equal(t, filed, f.count("approval.request.filed"), "an applied change files nothing")
					assert.Empty(t, f.approvals())
					return
				}
				assert.Equal(t, "/api/v1/approvals/", resp.Header.Get("Location")[:len("/api/v1/approvals/")])
				rec := decodeBody[approvalRecord](t, resp)
				assert.Equal(t, "/api/v1/approvals/"+rec.ID, resp.Header.Get("Location"))
				assert.Equal(t, sharechange.KindName, rec.Kind)
				assert.Equal(t, row.action, rec.Action)
				assert.Equal(t, approval.StateRequested, rec.State)
				assert.Equal(t, caller.principal, rec.RequestedBy)
				assert.Equal(t, 1, rec.ApprovalsRequired)
				assert.NotEmpty(t, rec.PayloadDigest)
				assert.False(t, rec.CanDecide)

				assert.Equal(t, before, f.dump(), "a pending change writes nothing to the registry")
				assert.Equal(t, filed+1, f.count("approval.request.filed"))
				assert.Empty(t, f.shareEventsAfter(before), "a pending change is not on the record as a change")
			})
		}
	}
}

// shareEventsAfter lists the share.* events written by the calls since
// dump before was taken: none for a pending change. (The seed's own
// entries are in the log already; they are the owner's.)
func (f *shareFx) shareEventsAfter(string) []*storage.AuditEntry {
	var out []*storage.AuditEntry
	for _, e := range f.shareEvents() {
		if e.Actor == f.admin.principal || e.Actor == f.sess.principal {
			out = append(out, e)
		}
	}
	return out
}

// The owner's changes are never pending: every row applies at once.
func TestShareRoutes_OwnerChangesAreNeverPending(t *testing.T) {
	for _, row := range httpRows() {
		t.Run(row.name, func(t *testing.T) {
			f := newShareFx(t)
			if row.setup != nil {
				row.setup(f)
			}
			before := f.dump()
			method, path, body := row.call(f)
			resp := f.owner(method, path, body)
			require.Equal(t, row.ownerStatus, resp.StatusCode, "%s %s: %s", method, path, errBody(t, resp))
			if !row.unchanged {
				assert.NotEqual(t, before, f.dump())
			}
			assert.Empty(t, f.approvals(), "the owner files nothing")
			assert.Zero(t, f.count("approval.request.filed"))
		})
	}
}

// A pending change is approved by an eligible approver and then takes
// effect, recorded as the administrator's change with the request and
// its approvers; the people it benefits, the requester and an
// administrator without approve:shares cannot approve it.
func TestShareRoutes_ApprovalAppliesAPendingChange(t *testing.T) {
	for _, row := range httpRows() {
		if !row.pending() {
			continue
		}
		t.Run(row.name, func(t *testing.T) {
			f := newShareFx(t)
			if row.setup != nil {
				row.setup(f)
			}
			method, path, body := row.call(f)
			resp := f.asKey(method, path, body)
			require.Equal(t, http.StatusAccepted, resp.StatusCode, errBody(t, resp))
			rec := decodeBody[approvalRecord](t, resp)
			before := f.dump()

			// Those who may not decide it.
			requireCodeOf(t, f.decide(f.dana, "approve", rec), http.StatusForbidden, approval.CodeBeneficiaryCannotDecide)
			requireCodeOf(t, f.decide(f.dana, "reject", rec), http.StatusForbidden, approval.CodeBeneficiaryCannotDecide)
			asRequester := f.asKey(http.MethodPost, "/approvals/"+rec.ID+"/approve", map[string]string{"payload_digest": rec.PayloadDigest})
			requireCodeOf(t, asRequester, http.StatusForbidden, approval.CodeRequesterCannotDecide)
			asOtherAdmin := f.callAs(f.sess, http.MethodPost, "/api/v1/approvals/"+rec.ID+"/approve", map[string]string{"payload_digest": rec.PayloadDigest})
			assert.Equal(t, http.StatusNotFound, asOtherAdmin.StatusCode, "an administrator without approve:shares does not even see the request")
			wrong := f.asActor(f.alice, http.MethodPost, "/approvals/"+rec.ID+"/approve", map[string]string{"payload_digest": "v1:sha256:00"})
			requireCodeOf(t, wrong, http.StatusConflict, approval.CodeDigestMismatch)
			assert.Equal(t, before, f.dump(), "nobody who may not decide it changed anything")

			// An approver it does not benefit.
			resp = f.decide(f.alice, "approve", rec)
			require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
			done := decodeBody[decisionResponse](t, resp)
			assert.Equal(t, approval.StateApproved, done.State)
			assert.NotEmpty(t, done.ResultRef)
			assert.NotEqual(t, before, f.dump(), "the approval applied the change")

			events := f.shareEvents()
			last := events[len(events)-1]
			assert.Equal(t, f.admin.principal, last.Actor, "the change is the administrator's")
			assert.Equal(t, rec.ID, last.Payload["request_id"])
			assert.Equal(t, []any{f.alice.id}, last.Payload["approvers"])
		})
	}
}

// The owner approves too.
func TestShareRoutes_OwnerApprovesAPendingChange(t *testing.T) {
	f := newShareFx(t)
	f.seedTeam()
	resp := f.asKey(http.MethodPost, "/share/grants", map[string]any{"audience": "team", "policy": "base"})
	require.Equal(t, http.StatusAccepted, resp.StatusCode, errBody(t, resp))
	rec := decodeBody[approvalRecord](t, resp)
	resp = f.owner(http.MethodPost, "/approvals/"+rec.ID+"/approve", map[string]string{"payload_digest": rec.PayloadDigest})
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	resp = f.owner(http.MethodGet, "/share/grants", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, decodeBody[struct {
		Data []grantRecord `json:"data"`
	}](t, resp).Data, 1)
}

// Rejecting leaves the registry as it was.
func TestShareRoutes_RejectedChangeChangesNothing(t *testing.T) {
	f := newShareFx(t)
	f.seedTeam()
	resp := f.asKey(http.MethodPost, "/share/grants", map[string]any{"audience": "team", "policy": "base"})
	rec := decodeBody[approvalRecord](t, resp)
	before := f.dump()
	resp = f.decide(f.alice, "reject", rec)
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, before, f.dump())
	requireCodeOf(t, f.decide(f.bob, "approve", rec), http.StatusConflict, approval.CodeClosed)
}

// A policy edited while a grant of it is pending ends the grant request
// stale: the edit applied at once (nothing was granted yet) and the
// approver never reviewed it.
func TestShareRoutes_PolicyEditWhileAGrantIsPendingMakesItStale(t *testing.T) {
	f := newShareFx(t)
	f.seedTeam()
	resp := f.asKey(http.MethodPost, "/share/grants", map[string]any{"audience": "team", "policy": "base"})
	require.Equal(t, http.StatusAccepted, resp.StatusCode, errBody(t, resp))
	rec := decodeBody[approvalRecord](t, resp)
	resp = f.callAs(f.sess, http.MethodPut, "/api/v1/share/policies/base", map[string]any{"document": widerDoc})
	require.Equal(t, http.StatusOK, resp.StatusCode, "an edit of a policy no grant uses applies at once: %s", errBody(t, resp))
	before := f.dump()

	requireCodeOf(t, f.decide(f.alice, "approve", rec), http.StatusConflict, approval.CodeStale)
	assert.Equal(t, before, f.dump(), "a stale request applies nothing")
	resp = f.owner(http.MethodGet, "/share/grants", nil)
	assert.Empty(t, decodeBody[struct {
		Data []grantRecord `json:"data"`
	}](t, resp).Data)
}

// A stored spec the compiler cannot read, or a new filter that is not a
// restriction of the old, is widening: the edit waits.
func TestShareRoutes_UnclassifiableIsWidening(t *testing.T) {
	t.Run("stored spec does not parse", func(t *testing.T) {
		f := newShareFx(t)
		f.seedGranted()
		pol, err := f.bundle.svc.Store(ownerCtx()).Sharing().Policies().GetByName(ownerCtx(), "base")
		require.NoError(t, err)
		_, err = f.bundle.svc.Store(ownerCtx()).Sharing().Policies().Update(ownerCtx(), pol.ID, []byte(`{"bogus":1}`), f.clock.now())
		require.NoError(t, err)
		resp := f.asKey(http.MethodPut, "/share/policies/base", map[string]any{"document": narrowDoc})
		require.Equal(t, http.StatusAccepted, resp.StatusCode, errBody(t, resp))
	})
	t.Run("new filter is not a restriction of the old", func(t *testing.T) {
		f := newShareFx(t)
		f.seedGranted()
		doc := `{"name":"base","data":"type==contact,type==note","fields":{"contact":["title"]}}`
		resp := f.asKey(http.MethodPut, "/share/policies/base", map[string]any{"document": doc})
		require.Equal(t, http.StatusAccepted, resp.StatusCode, errBody(t, resp))
	})
}

// A principal binding or unbinding its own key's policy is not an admin
// change: it applies at once, though unbinding widens that key's view.
func TestShareRoutes_OwnKeyPolicyAppliesAtOnce(t *testing.T) {
	f := newShareFx(t)
	f.seedTeam()
	f.bindDanaKey("base")
	before := f.dump()
	resp := f.asActor(f.dana, http.MethodPut, "/self/keys/"+f.danaKeyID()+"/policy", map[string]any{"policy": ""})
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	assert.NotEqual(t, before, f.dump())
	assert.Empty(t, f.approvals())
	assert.Equal(t, 1, f.count("share.key_policy.bound")-1, "the unbinding is on the record")
}

// A change that fails validation is refused as before and files
// nothing: a pending request is never made for a body the registry
// would have refused.
func TestShareRoutes_RefusalsFileNothing(t *testing.T) {
	f := newShareFx(t)
	f.seedTeam()
	before := f.dump()
	for _, c := range []struct {
		name         string
		method, path string
		body         any
		status       int
	}{
		{"unknown audience", http.MethodPost, "/share/grants", map[string]any{"audience": "nobody", "policy": "base"}, http.StatusNotFound},
		{"unknown policy", http.MethodPost, "/share/grants", map[string]any{"audience": "team", "policy": "nothing"}, http.StatusNotFound},
		{"the owner joins no audience", http.MethodPut, "/share/audiences/team/members/owner", map[string]any{}, http.StatusBadRequest},
		{"unknown principal", http.MethodPut, "/share/audiences/team/members/prn_nope", map[string]any{}, http.StatusNotFound},
		{"expiry in the past", http.MethodPut, "/share/audiences/team/members/" + f.dana.id, map[string]any{"expires_at": "2001-01-01T00:00:00Z"}, http.StatusBadRequest},
		{"bad class", http.MethodPut, "/principals/" + f.dana.id + "/ceiling", map[string]any{"deny_classes": []string{"Bad!"}}, http.StatusBadRequest},
		{"unknown principal's ceiling", http.MethodPut, "/principals/prn_nope/ceiling", map[string]any{"deny_classes": []string{}}, http.StatusNotFound},
		{"unparseable policy", http.MethodPut, "/share/policies/base", map[string]any{"document": "not: [valid"}, http.StatusBadRequest},
		{"another name", http.MethodPut, "/share/policies/base", map[string]any{"document": partnerDoc}, http.StatusBadRequest},
		{"unknown policy for a key", http.MethodPut, "/share/keys/" + f.danaKeyID() + "/policy", map[string]any{"policy": "nothing"}, http.StatusNotFound},
		{"the owner's key", http.MethodPut, "/share/keys/" + f.keyOf(storageOwner) + "/policy", map[string]any{"policy": "base"}, http.StatusForbidden},
	} {
		t.Run(c.name, func(t *testing.T) {
			resp := f.asKey(c.method, c.path, c.body)
			assert.Equal(t, c.status, resp.StatusCode, errBody(t, resp))
		})
	}
	assert.Equal(t, before, f.dump())
	assert.Empty(t, f.approvals())
}

const storageOwner = storage.OwnerPrincipal
