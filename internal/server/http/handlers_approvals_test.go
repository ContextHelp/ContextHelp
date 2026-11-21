package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/approval/approvaltest"
	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/auth/authtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// approvalFixture is a router with the approval mechanism and the test
// kind, over token-authenticated person principals: the owner, a requester (an
// admin), two approvers holding share-approver, the target the test
// changes benefit (an approver too) and an outsider.
type approvalFixture struct {
	t     *testing.T
	ts    *httptest.Server
	store storage.StorageDriver
	svc   *approval.Service
	kind  *approvaltest.Kind
	clock *testClock
	// principals by token name.
	who map[string]*authn.Principal
}

func newApprovalFixture(t *testing.T) *approvalFixture {
	t.Helper()
	bundle := newTestServerBundle(t)
	t.Cleanup(bundle.Close)
	store := bundle.svc.Store(ownerCtx())
	f := &approvalFixture{t: t, store: store, clock: &testClock{t: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}, who: map[string]*authn.Principal{}}
	approver := []string{authn.RoleReader, authn.RoleShareApprover}
	entries := []authtest.Token{
		{Token: "owner", Principal: storage.OwnerPrincipal, Roles: authn.OwnerRoles},
		// The owner's key narrowed to a verb role: owner kind, no approval scope.
		{Token: "owner-narrow", Principal: storage.OwnerPrincipal, Roles: []string{authn.RoleReader}},
	}
	for name, roles := range map[string][]string{
		"requester": {authn.RoleAdmin}, "alice": approver, "bob": approver, "target": approver, "outsider": {authn.RoleReader},
	} {
		id := "prn_" + name
		require.NoError(t, store.Auth().Principals().Create(ownerCtx(), &storage.Principal{ID: id, Kind: storage.PrincipalKindPerson, DisplayName: name, Roles: roles}))
		entries = append(entries, authtest.Token{Token: name, Principal: id, Kind: storage.PrincipalKindPerson, Roles: roles})
	}
	for _, e := range entries {
		kind := e.Kind
		if kind == "" {
			kind = storage.PrincipalKindOwner
		}
		f.who[e.Token] = &authn.Principal{ID: e.Principal, Kind: kind, Roles: e.Roles, Scopes: authn.ScopesForRoles(e.Roles)}
	}
	f.kind = approvaltest.New("test.change", store)
	reg := approval.NewRegistry()
	require.NoError(t, reg.Register(f.kind.Definition()))
	svc, err := approval.NewService(store.Approvals(), store.AuditLog(), reg, approval.Options{PendingTTL: 72 * time.Hour, Now: f.clock.now})
	require.NoError(t, err)
	f.svc = svc
	f.ts = httptest.NewServer(NewRouterWithConfig(bundle.svc, RouterConfig{
		Auth:      authtest.TokenChain(entries...),
		Registry:  store.Auth(),
		Sharing:   store.Sharing(),
		AuditLog:  store.AuditLog(),
		Approvals: svc,
	}))
	t.Cleanup(f.ts.Close)
	return f
}

// file files a test change by the requester benefiting the target.
func (f *approvalFixture) file(value string) *storage.ApprovalRequest {
	f.t.Helper()
	r, err := f.svc.File(context.Background(), approval.Filing{
		Kind: f.kind.Name(), Action: approvaltest.Action, Requester: f.who["requester"], SubjectID: "prn_target",
		Payload: approvaltest.Raw("prn_target", value),
	})
	require.NoError(f.t, err)
	return r
}

// call sends method path with body as token's principal.
func (f *approvalFixture) call(token, method, path string, body any) *http.Response {
	f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(f.t, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, f.ts.URL+"/api/v1"+path, rd)
	require.NoError(f.t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(f.t, err)
	f.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (f *approvalFixture) decide(token, verb string, r *storage.ApprovalRequest, digest string) *http.Response {
	return f.call(token, http.MethodPost, "/approvals/"+r.ID+"/"+verb, map[string]string{"payload_digest": digest, "note": verb + " by " + token})
}

// listIDs lists the approvals token sees with query.
func (f *approvalFixture) listIDs(token, query string) []string {
	f.t.Helper()
	resp := f.call(token, http.MethodGet, "/approvals"+query, nil)
	require.Equal(f.t, http.StatusOK, resp.StatusCode, errBody(f.t, resp))
	body := decodeBody[struct {
		Data  []approvalRecord `json:"data"`
		Total int              `json:"total"`
	}](f.t, resp)
	ids := []string{}
	for _, r := range body.Data {
		ids = append(ids, r.ID)
	}
	assert.Equal(f.t, len(ids), body.Total)
	return ids
}

// The queue shows each caller what it filed, is the subject of or may
// decide; anything else is 404, like a request that does not exist.
func TestApprovalRoutes_ListAndShowFollowTheRequest(t *testing.T) {
	f := newApprovalFixture(t)
	r := f.file("blue")
	assert.Equal(t, []string{r.ID}, f.listIDs("owner", ""))
	assert.Equal(t, []string{r.ID}, f.listIDs("requester", ""))
	assert.Equal(t, []string{r.ID}, f.listIDs("alice", ""))
	assert.Equal(t, []string{r.ID}, f.listIDs("target", ""))
	assert.Empty(t, f.listIDs("outsider", ""))

	resp := f.call("alice", http.MethodGet, "/approvals/"+r.ID, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	rec := decodeBody[approvalRecord](t, resp)
	assert.Equal(t, approval.StateRequested, rec.State)
	assert.Equal(t, r.PayloadDigest, rec.PayloadDigest)
	assert.JSONEq(t, `{"target":"prn_target","value":"blue"}`, string(rec.Payload))
	assert.True(t, rec.CanDecide)
	assert.Equal(t, 0, rec.Approvals)
	assert.Equal(t, 1, rec.ApprovalsRequired)
	assert.NotNil(t, rec.Decisions)

	resp = f.call("requester", http.MethodGet, "/approvals/"+r.ID, nil)
	assert.False(t, decodeBody[approvalRecord](t, resp).CanDecide, "the requester never decides")
	for _, path := range []string{"/approvals/" + r.ID, "/approvals/apr_missing"} {
		resp = f.call("outsider", http.MethodGet, path, nil)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, path)
		assert.Equal(t, CodeNotFound, errCode(t, resp))
	}
}

// Decisions over the API: digest required and bound, separation codes,
// one decision each, the final approval alone carries the result.
func TestApprovalRoutes_DecideWithStableCodes(t *testing.T) {
	f := newApprovalFixture(t)
	f.kind.SetRequired(2)
	r := f.file("two")

	expect := func(resp *http.Response, status int, code string) {
		t.Helper()
		require.Equal(t, status, resp.StatusCode)
		assert.Equal(t, code, errCode(t, resp))
	}
	expect(f.call("alice", http.MethodPost, "/approvals/"+r.ID+"/approve", map[string]string{}), http.StatusBadRequest, "INVALID_REQUEST")
	expect(f.decide("alice", "approve", r, approval.Digest([]byte(`{}`))), http.StatusConflict, approval.CodeDigestMismatch)
	expect(f.decide("requester", "approve", r, r.PayloadDigest), http.StatusForbidden, approval.CodeRequesterCannotDecide)
	expect(f.decide("target", "approve", r, r.PayloadDigest), http.StatusForbidden, approval.CodeBeneficiaryCannotDecide)
	expect(f.decide("outsider", "approve", r, r.PayloadDigest), http.StatusNotFound, CodeNotFound)

	resp := f.decide("alice", "approve", r, r.PayloadDigest)
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	first := decodeBody[map[string]any](t, resp)
	assert.Equal(t, "requested", first["state"])
	assert.EqualValues(t, 1, first["approvals"])
	assert.NotContains(t, first, "result", "no result before the last approval")
	expect(f.decide("alice", "approve", r, r.PayloadDigest), http.StatusConflict, approval.CodeAlreadyDecided)

	resp = f.decide("bob", "approve", r, r.PayloadDigest)
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	final := decodeBody[map[string]any](t, resp)
	assert.Equal(t, "approved", final["state"])
	assert.Equal(t, approvaltest.Reveal(r.ID), final["result"], "the final approver gets the result")
	assert.Equal(t, approvaltest.Ref(r.ID), final["result_ref"])

	resp = f.call("bob", http.MethodGet, "/approvals/"+r.ID, nil)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), approvaltest.Reveal(r.ID), "the result is never shown again")
	expect(f.decide("owner", "approve", r, r.PayloadDigest), http.StatusConflict, approval.CodeClosed)

	// The result is in no audit entry.
	entries, _, err := f.store.AuditLog().List(ownerCtx(), storage.AuditFilter{Limit: 1000})
	require.NoError(t, err)
	for _, e := range entries {
		b, _ := json.Marshal(e.Payload)
		assert.NotContains(t, string(b), approvaltest.Reveal(r.ID), e.EventType)
	}
}

// Reject, withdraw, stale and expired over the API.
func TestApprovalRoutes_RejectWithdrawStaleExpired(t *testing.T) {
	f := newApprovalFixture(t)
	rejected := f.file("reject")
	resp := f.decide("alice", "reject", rejected, rejected.PayloadDigest)
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, approval.StateRejected, decodeBody[approvalRecord](t, resp).State)

	withdrawn := f.file("withdraw")
	resp = f.call("alice", http.MethodDelete, "/approvals/"+withdrawn.ID, nil)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, approval.CodeNotRequester, errCode(t, resp))
	resp = f.call("outsider", http.MethodDelete, "/approvals/"+withdrawn.ID, nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp = f.call("requester", http.MethodDelete, "/approvals/"+withdrawn.ID, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, approval.StateWithdrawn, decodeBody[approvalRecord](t, resp).State)

	stale := f.file("stale")
	f.kind.Bump("prn_target")
	resp = f.decide("alice", "approve", stale, stale.PayloadDigest)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, approval.CodeStale, errCode(t, resp))
	resp = f.call("alice", http.MethodGet, "/approvals/"+stale.ID, nil)
	assert.Equal(t, approval.StateStale, decodeBody[approvalRecord](t, resp).State)

	expired := f.file("expired")
	f.clock.advance(72 * time.Hour)
	resp = f.decide("alice", "approve", expired, expired.PayloadDigest)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, approval.CodeExpired, errCode(t, resp))
	assert.Equal(t, []string{expired.ID}, f.listIDs("owner", "?state=expired"))
	assert.Equal(t, []string{rejected.ID}, f.listIDs("owner", "?state=rejected&kind=test.change"))
	assert.Len(t, f.listIDs("owner", "?limit=2"), 2)
}

// The list refuses unknown, repeated or bad parameters.
func TestApprovalRoutes_ListParams(t *testing.T) {
	f := newApprovalFixture(t)
	for _, q := range []string{"?state=pending", "?bogus=1", "?limit=0", "?limit=501", "?limit=x", "?kind=a&kind=b", "?state="} {
		resp := f.call("owner", http.MethodGet, "/approvals"+q, nil)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, q)
		assert.Equal(t, CodeInvalidParam, errCode(t, resp), q)
	}
}

// A router built without the mechanism answers 404 on every route.
func TestApprovalRoutes_NotWired(t *testing.T) {
	bundle := newTestServerBundle(t)
	t.Cleanup(bundle.Close)
	ts := httptest.NewServer(NewRouterWithConfig(bundle.svc, RouterConfig{}))
	t.Cleanup(ts.Close)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/approvals"}, {http.MethodGet, "/approvals/apr_x"},
		{http.MethodPost, "/approvals/apr_x/approve"}, {http.MethodPost, "/approvals/apr_x/reject"},
		{http.MethodDelete, "/approvals/apr_x"},
	} {
		req, err := http.NewRequest(c.method, ts.URL+"/api/v1"+c.path, strings.NewReader(`{"payload_digest":"d"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, c.path)
	}
}

// An owner key narrowed below the approval scopes keeps the owner kind
// and none of its reach: it lists nothing and sees, decides and
// withdraws nothing, each a 404.
func TestApprovalRoutes_NarrowedOwnerKeyHasNoReach(t *testing.T) {
	f := newApprovalFixture(t)
	r := f.file("narrowed")
	assert.Empty(t, f.listIDs("owner-narrow", ""))
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/approvals/" + r.ID},
		{http.MethodDelete, "/approvals/" + r.ID},
	} {
		resp := f.call("owner-narrow", c.method, c.path, nil)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, c.method)
	}
	for _, verb := range []string{"approve", "reject"} {
		resp := f.decide("owner-narrow", verb, r, r.PayloadDigest)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, verb)
	}
	assert.Equal(t, []string{r.ID}, f.listIDs("owner", ""), "the full-scope owner still sees it, still pending")
}

// Deciding a stored request whose kind this instance no longer
// registers is a 409 APPROVAL_KIND_UNKNOWN: the request's state, not a
// malformed call.
func TestApprovalRoutes_DecideUnknownKindIs409(t *testing.T) {
	f := newApprovalFixture(t)
	r := &storage.ApprovalRequest{
		Kind: "gone.kind", Action: "a", RequestedBy: "prn_requester", Payload: json.RawMessage(`{}`),
		PayloadDigest: approval.Digest([]byte(`{}`)), Preconditions: json.RawMessage(`{}`), ApprovalsRequired: 1,
		RequestedAt: f.clock.now(), ExpiresAt: f.clock.now().Add(time.Hour),
	}
	require.NoError(t, f.store.Approvals().Create(ownerCtx(), r))
	for _, verb := range []string{"approve", "reject"} {
		resp := f.decide("owner", verb, r, r.PayloadDigest)
		require.Equal(t, http.StatusConflict, resp.StatusCode, verb)
		assert.Equal(t, approval.CodeKindUnknown, errCode(t, resp))
	}
}
