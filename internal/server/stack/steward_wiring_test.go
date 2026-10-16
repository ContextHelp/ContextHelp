package stack

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// The built stack registers steward.grant on the approval mechanism and
// wires the viewer resolver for steward grants from the config: off by
// default (no request can be filed and no grant is used), on with
// steward.enabled, N frozen from approvals.steward.required.
func TestBuildWiresStewardGrants(t *testing.T) {
	ctx := context.Background()
	driver := storageutil.NewTestDriver(t)
	cfg := &config.Config{
		Steward:   config.StewardConfig{Enabled: false, WithheldClasses: []string{"personal"}},
		Approvals: config.ApprovalsConfig{Steward: config.ApprovalKindConfig{Required: 2}},
	}
	st := build(t, cfg, driver)
	if st.Approvals == nil || st.Stewards == nil {
		t.Fatalf("Approvals = %v, Stewards = %v; want both wired", st.Approvals, st.Stewards)
	}
	if got, want := st.Approvals.Kinds().Names(), []string{sharechange.KindName, steward.KindName}; !slices.Equal(got, want) {
		t.Fatalf("registered kinds = %v, want %v", got, want)
	}

	sharingtest.EnsurePrincipal(t, driver, "prn_requester", storage.PrincipalKindPerson, authn.RoleReader, authn.RoleStewardRequester)
	sharingtest.EnsurePrincipal(t, driver, "prn_steward", storage.PrincipalKindPerson, authn.RoleReader, authn.RoleStewardRequester)
	requester := &authn.Principal{ID: "prn_requester", Kind: storage.PrincipalKindPerson, Scopes: authn.ScopesForRoles([]string{authn.RoleReader, authn.RoleStewardRequester})}
	now := time.Now()
	payload, err := json.Marshal(steward.Request{StewardID: "prn_steward", Scope: json.RawMessage(`{"data":"type==contact"}`), ExpiresAt: now.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	filing := approval.Filing{
		Kind: steward.KindName, Action: steward.ActionRequest, Requester: requester, SubjectID: "prn_steward",
		Reason: "Legal hold for matter 2026-117: supplier contacts requested by counsel", Payload: payload,
	}

	// Off: nothing can be filed, nothing is used.
	if _, err := st.Approvals.File(ctx, filing); approval.CodeOf(err) != steward.CodeDisabled {
		t.Fatalf("filing with steward.enabled off: %v, want %s", err, steward.CodeDisabled)
	}
	g := sharingtest.GrantSteward(t, driver, "prn_steward", `{"policies":[{"data":"type==contact"}]}`, now.Add(-time.Hour), now.Add(time.Hour))
	viewers := st.Service.Viewers()
	_, err = viewers.Resolve(ctx, "prn_steward", storage.PrincipalKindPerson, "", sharing.AsSteward(g.ID))
	var refusal *sharing.StewardRefusal
	if !errors.As(err, &refusal) || refusal.Reason != sharing.StewardReasonStewardDisabled {
		t.Fatalf("elevation with steward.enabled off: %v, want %s", err, sharing.StewardReasonStewardDisabled)
	}

	// On: filed with N from the config, and the grant is used.
	cfg.Steward.Enabled = true
	r, err := st.Approvals.File(ctx, filing)
	if err != nil {
		t.Fatalf("filing with steward.enabled on: %v", err)
	}
	if r.ApprovalsRequired != 2 {
		t.Errorf("approvals_required = %d, want 2 (approvals.steward.required)", r.ApprovalsRequired)
	}
	v, err := viewers.Resolve(ctx, "prn_steward", storage.PrincipalKindPerson, "", sharing.AsSteward(g.ID))
	if err != nil || v.StewardGrantID() != g.ID {
		t.Fatalf("elevation with steward.enabled on: viewer %q, err %v; want the grant's viewer", v.StewardGrantID(), err)
	}
	// The grant half reads the same settings: a cap below the grant's
	// age expires it, and lifting the cap brings it back.
	cfg.Steward.MaxDuration = time.Minute
	if got := st.Stewards.State(g); got != storage.StewardStateExpired {
		t.Errorf("state under a one-minute cap = %s, want expired", got)
	}
	cfg.Steward.MaxDuration = 0
	if got := st.Stewards.State(g); got != storage.StewardStateActive {
		t.Errorf("state under the default cap = %s, want active", got)
	}
	// The owner revokes it.
	owner := &authn.Principal{ID: storage.OwnerPrincipal, Kind: storage.PrincipalKindOwner, Scopes: authn.AllScopes}
	if _, err := st.Stewards.Revoke(ctx, g.ID, owner, "done"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, err = viewers.Resolve(ctx, "prn_steward", storage.PrincipalKindPerson, "", sharing.AsSteward(g.ID))
	if !errors.As(err, &refusal) || refusal.Reason != sharing.StewardReasonGrantRevoked {
		t.Fatalf("elevation after the revocation: %v, want %s", err, sharing.StewardReasonGrantRevoked)
	}
}

// ApprovalKinds registers steward.grant and freezes the approver count
// from approvals.steward.required: 1 when unset, the configured value
// otherwise.
func TestApprovalKindsStewardApprovers(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  config.ApprovalsConfig
		want int
	}{
		"unset":      {config.ApprovalsConfig{}, 1},
		"configured": {config.ApprovalsConfig{Steward: config.ApprovalKindConfig{Required: 3}}, 3},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{Approvals: tc.cfg}
			reg, err := ApprovalKinds(storageutil.NewTestDriver(t), cfg)
			if err != nil {
				t.Fatalf("ApprovalKinds: %v", err)
			}
			if got := reg.Names(); !slices.Equal(got, []string{sharechange.KindName, steward.KindName}) {
				t.Fatalf("kinds = %v, want [%s %s]", got, sharechange.KindName, steward.KindName)
			}
			k, _ := reg.Lookup(steward.KindName)
			if n := k.Required(); n != tc.want {
				t.Errorf("approver count = %d, want %d", n, tc.want)
			}
		})
	}
}

// The approver count and the reason length are read from the config on
// every filing, like the steward settings: a change applies to the next
// request without rebuilding the registry.
func TestApprovalKindsReadTheConfigLive(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	cfg := &config.Config{Steward: config.StewardConfig{Enabled: true}}
	reg, err := ApprovalKinds(driver, cfg)
	if err != nil {
		t.Fatalf("ApprovalKinds: %v", err)
	}
	k, _ := reg.Lookup(steward.KindName)
	if n := k.Required(); n != 1 {
		t.Fatalf("approver count = %d, want 1", n)
	}
	cfg.Approvals.Steward.Required = 4
	if n := k.Required(); n != 4 {
		t.Errorf("approver count after the change = %d, want 4", n)
	}

	sharingtest.EnsurePrincipal(t, driver, "prn_requester", storage.PrincipalKindPerson, authn.RoleReader, authn.RoleStewardRequester)
	svc, err := approval.NewService(driver.Approvals(), driver.AuditLog(), reg, approval.Options{PendingTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	requester := &authn.Principal{ID: "prn_requester", Kind: storage.PrincipalKindPerson, Scopes: authn.ScopesForRoles([]string{authn.RoleReader, authn.RoleStewardRequester})}
	payload, _ := json.Marshal(steward.Request{StewardID: "prn_requester", Scope: json.RawMessage(`{"data":"type==contact"}`), ExpiresAt: time.Now().Add(24 * time.Hour)})
	filing := approval.Filing{
		Kind: steward.KindName, Action: steward.ActionRequest, Requester: requester, SubjectID: "prn_requester",
		Reason: "Legal hold for matter 2026-117: counsel", Payload: payload,
	}
	cfg.Steward.MinReasonChars = 200
	if _, err := svc.File(context.Background(), filing); approval.CodeOf(err) != approval.CodeReasonTooShort {
		t.Fatalf("filing under a raised minimum reason: %v, want %s", err, approval.CodeReasonTooShort)
	}
	cfg.Steward.MinReasonChars = 0
	if _, err := svc.File(context.Background(), filing); err != nil {
		t.Fatalf("filing under the default minimum: %v", err)
	}
}
