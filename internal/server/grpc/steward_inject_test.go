package grpc

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/auth/authtest"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// No metadata a client sends elevates an RPC: gRPC carries no browser
// session, the only source of a grant id. A key whose principal holds a
// usable steward grant keeps its ordinary view whatever it names; the
// same principal with the grant in its Meta (what a session would put
// there) is elevated, so the grant itself is good.
func TestAuthUnaryIgnoresStewardMetadata(t *testing.T) {
	r := newStewardRig(t)
	ctx := context.Background()
	const device = "prn_key_steward"
	sharingtest.EnsurePrincipal(t, r.drv, device, storage.PrincipalKindDevice, authn.RoleReader, authn.RoleStewardRequester)
	t0 := r.now.Add(-time.Hour)
	grant := sharingtest.GrantSteward(t, r.drv, device, `{"policies":[{"data":"type==contact"}]}`, t0, t0.Add(48*time.Hour))
	keys := authn.NewKeysOver(r.drv.Auth(), nil)
	secret, _, err := keys.Mint(ctx, authn.MintKey{PrincipalID: device, Name: "device"})
	if err != nil {
		t.Fatal(err)
	}
	keyed := authn.NewChain(authn.NewAPIKeyMethod(keys, nil))

	run := func(chain *authn.Chain, md metadata.MD) sharing.Viewer {
		t.Helper()
		var seen sharing.Viewer
		ic := authUnaryInterceptor(chain, nil, r.viewers, nil)
		_, err := ic(metadata.NewIncomingContext(ctx, md), nil, unaryInfo(pb.QueryService_ListObjects_FullMethodName),
			func(hctx context.Context, _ any) (any, error) {
				seen, _ = sharing.FromContext(hctx)
				return "ok", nil
			})
		if err != nil {
			t.Fatalf("interceptor: %v", err)
		}
		return seen
	}

	named := &authn.Principal{ID: device, Kind: storage.PrincipalKindDevice, Roles: []string{authn.RoleReader, authn.RoleStewardRequester},
		Meta: map[string]string{authn.MetaStewardGrantID: grant.ID}}
	named.Scopes = authn.ScopesForRoles(named.Roles)
	if v := run(authtest.Fixed(named), metadata.Pairs("authorization", "Bearer x")); v.StewardGrantID() != grant.ID {
		t.Fatalf("control: a principal naming the grant is elevated to %q, want %s", v.StewardGrantID(), grant.ID)
	}

	for _, key := range []string{
		"steward_grant_id", "steward-grant-id", "x-steward-grant-id", "x-ctxt-steward-grant", "x-ctxt-view-as", "grpcgateway-steward-grant-id",
	} {
		md := metadata.Pairs("authorization", "Bearer "+secret, key, grant.ID)
		if v := run(keyed, md); v.StewardGrantID() != "" || v.StrictBackstop() {
			t.Errorf("metadata %q elevated the RPC to %q", key, v.StewardGrantID())
		}
	}
}

// Every refused elevation is answered alike on both transports: the
// gRPC code is the counterpart of the HTTP status (PermissionDenied for
// 403, Unavailable for 503), and the stable code leads the message.
func TestStewardRefusalCodesAgreeWithHTTP(t *testing.T) {
	counterpart := map[int]codes.Code{http.StatusForbidden: codes.PermissionDenied, http.StatusServiceUnavailable: codes.Unavailable}
	for _, c := range sharingtest.StewardRefusals() {
		err := stewardRefusalStatus(c.Refusal)
		st, _ := status.FromError(err)
		if st.Code() != counterpart[c.Status] {
			t.Errorf("%s: code %s, want %s (HTTP %d)", c.Refusal.Reason, st.Code(), counterpart[c.Status], c.Status)
		}
		if !strings.HasPrefix(st.Message(), c.Refusal.Code()+":") {
			t.Errorf("%s: message %q does not lead with %s", c.Refusal.Reason, st.Message(), c.Refusal.Code())
		}
	}
}
