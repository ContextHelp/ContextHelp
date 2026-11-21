package http

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The provisioning routes leave a receipt for a non-owner: authn and the
// guard's decision as route_access, admitted or refused, with no
// response checks.
func TestReceiptAccessRoutes(t *testing.T) {
	rig := newReceiptRig(t, false)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, http.StatusOK, map[string]any{}) })
	serve := func(rc RouterConfig) chi.Router {
		rc.Auth, rc.Audit = &authn.Chain{}, rig.writer
		a := accessRoutes{rc: rc}
		r := chi.NewRouter()
		r.Get("/bos/v1/access/{business}/state", a.route(http.MethodGet, "/access/{business}/state", ok))
		return r
	}
	call := func(r chi.Router, p *authn.Principal, v sharing.Viewer) int {
		ctx := sharing.WithViewer(authn.Attach(context.Background(), p), v)
		return rig.serveOn(ctx, r, "/bos/v1/access/biz_1/state").Code
	}
	peer := &authn.Principal{ID: "prn_bos", Kind: storage.PrincipalKindPeer, Provider: authn.MethodAPIKey}
	cfg := RouterConfig{AccessPrincipal: "prn_bos", AccessScope: "bos://core/biz_1"}

	require.Equal(t, http.StatusOK, call(serve(cfg), peer, restrictedViewer(t)))
	require.Equal(t, http.StatusForbidden, call(serve(cfg), receiptPrincipal, restrictedViewer(t)))
	require.Equal(t, http.StatusForbidden, call(serve(RouterConfig{}), peer, restrictedViewer(t)))
	require.Equal(t, http.StatusOK, call(serve(cfg), peer, sharing.Unrestricted), "the owner is not recorded")

	es := rig.entries()
	require.Len(t, es, 3)
	assert.Equal(t, storage.AccessDecisionAllowed, es[0].Decision)
	assert.Equal(t, []string{"authn.api_key=PASS:authenticated", "route_access=PASS:authority_matched"}, checkTrail(es[0]))
	assert.Equal(t, "GET /bos/v1/access/{business}/state", es[0].Route)
	assert.Equal(t, storage.AccessDecisionDenied, es[1].Decision)
	assert.Equal(t, []string{"authn.api_key=PASS:authenticated", "route_access=FAIL:not_authority"}, checkTrail(es[1]))
	assert.Equal(t, storage.AccessDecisionDenied, es[2].Decision)
	assert.Equal(t, []string{"authn.api_key=PASS:authenticated", "route_access=FAIL:authority_unconfigured"}, checkTrail(es[2]))
}
