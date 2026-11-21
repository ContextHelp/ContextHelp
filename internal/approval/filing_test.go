package approval_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/approval/approvaltest"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Filing freezes the kind's payload in canonical form with its digest,
// records the preconditions the kind fingerprints, freezes N, and
// starts the pending TTL; one audit entry records it.
func TestFile_FreezesPayloadPreconditionsAndN(t *testing.T) {
	f := newFixture(t)
	f.kind.SetRequired(2)
	fl := f.filing("blue")
	fl.Payload = json.RawMessage(` {"value": "blue", "target": "prn_target"} `)
	r, err := f.svc.File(context.Background(), fl)
	require.NoError(t, err)

	got := f.stored(r.ID)
	canonical := `{"target":"prn_target","value":"blue"}`
	assert.Equal(t, canonical, string(got.Payload))
	assert.Equal(t, approval.Digest(json.RawMessage(canonical)), got.PayloadDigest)
	assert.JSONEq(t, `{"target":"prn_target","version":0}`, string(got.Preconditions))
	assert.Equal(t, 2, got.ApprovalsRequired)
	assert.Equal(t, storage.ApprovalRequested, got.Status)
	assert.Equal(t, f.requester.ID, got.RequestedBy)
	assert.Equal(t, f.target.ID, got.SubjectID)
	assert.Equal(t, "conformance of the mechanism", got.Reason)
	assert.True(t, got.RequestedAt.Equal(f.clock.now()))
	assert.True(t, got.ExpiresAt.Equal(f.clock.now().Add(ttl)))

	// The same operation in another key order freezes to the same digest.
	again, err := f.svc.File(context.Background(), f.filing("blue"))
	require.NoError(t, err)
	assert.Equal(t, got.PayloadDigest, again.PayloadDigest)

	events := f.events(r.ID)
	require.Len(t, events, 1)
	e := events[0]
	assert.Equal(t, approval.EventFiled, e.EventType)
	assert.Equal(t, f.requester.ID, e.Actor)
	assert.Equal(t, got.PayloadDigest, e.Payload["payload_digest"])
	assert.Equal(t, f.kind.Name(), e.Payload["kind"])
	assert.NotContains(t, e.Payload, "payload", "an entry carries the digest, never the payload")
}

// N is frozen when filed: changing the kind's setting afterwards, down
// or up, never changes what a pending request needs.
func TestFile_NIsFrozenAtFiling(t *testing.T) {
	f := newFixture(t)
	f.kind.SetRequired(2)
	two := f.file("frozen-two")
	f.kind.SetRequired(1)
	_, err := f.approve(two, f.alice)
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalRequested, f.stored(two.ID).Status, "lowering N never weakens a pending request")

	one := f.file("frozen-one")
	f.kind.SetRequired(3)
	d, err := f.approve(one, f.alice)
	require.NoError(t, err)
	assert.Equal(t, approval.StateApproved, d.State, "raising N never strands a pending request")
}

// The mechanism refuses a filing before the kind sees it: the owner
// files nothing, an unknown kind or action, a short reason; the kind's
// validator refuses a malformed payload; nothing is stored or logged.
func TestFile_Refusals(t *testing.T) {
	f := newFixture(t, func(k *approvaltest.Kind) { k.MinReason = 10 })
	cases := map[string]struct {
		mutate func(*approval.Filing)
		code   string
	}{
		"owner":           {func(fl *approval.Filing) { fl.Requester = f.owner }, approval.CodeOwnerCannotRequest},
		"no requester":    {func(fl *approval.Filing) { fl.Requester = nil }, approval.CodeInvalid},
		"unknown kind":    {func(fl *approval.Filing) { fl.Kind = "steward.grant" }, approval.CodeKindUnknown},
		"unknown action":  {func(fl *approval.Filing) { fl.Action = "test.change.delete" }, approval.CodeInvalid},
		"short reason":    {func(fl *approval.Filing) { fl.Reason = "  too short " }, approval.CodeReasonTooShort},
		"invalid payload": {func(fl *approval.Filing) { fl.Payload = json.RawMessage(`{"value":"x"}`) }, approval.CodeInvalid},
		"unknown field":   {func(fl *approval.Filing) { fl.Payload = json.RawMessage(`{"target":"prn_target","admin":true}`) }, approval.CodeInvalid},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fl := f.filing("refused")
			c.mutate(&fl)
			_, err := f.svc.File(context.Background(), fl)
			requireCode(t, err, c.code)
		})
	}
	// An unknown subject fails in the store (foreign key), not silently.
	fl := f.filing("refused")
	fl.SubjectID = "prn_nobody"
	_, err := f.svc.File(context.Background(), fl)
	assert.True(t, errors.Is(err, storage.ErrNotFound), "unknown subject: %v", err)

	list, err := f.drv.Approvals().List(context.Background(), storage.ApprovalFilter{})
	require.NoError(t, err)
	assert.Empty(t, list, "refused filings store nothing")
	all, _, err := f.drv.AuditLog().List(context.Background(), storage.AuditFilter{EventType: approval.EventFiled})
	require.NoError(t, err)
	assert.Empty(t, all, "refused filings log nothing")
}

// A kind whose N setting is below 1 fails the filing: N>=1 always.
func TestFile_NBelowOneIsAnError(t *testing.T) {
	f := newFixture(t)
	f.kind.SetRequired(0)
	_, err := f.svc.File(context.Background(), f.filing("zero"))
	require.Error(t, err)
}

// The registry refuses incomplete kinds and duplicate names.
func TestRegistry_Register(t *testing.T) {
	drv := newFixture(t).drv
	valid := approvaltest.New("test.valid", drv).Definition()
	reg := approval.NewRegistry()
	require.NoError(t, reg.Register(valid))
	assert.Error(t, reg.Register(valid), "duplicate name")
	for name, mutate := range map[string]func(*approval.Kind){
		"no name":        func(k *approval.Kind) { k.Name = " " },
		"no action":      func(k *approval.Kind) { k.Actions = nil },
		"empty action":   func(k *approval.Kind) { k.Actions = []string{""} },
		"no validate":    func(k *approval.Kind) { k.Validate = nil },
		"no fingerprint": func(k *approval.Kind) { k.Fingerprint = nil },
		"no eligible":    func(k *approval.Kind) { k.Eligible = nil },
		"no apply":       func(k *approval.Kind) { k.Apply = nil },
		"negative min":   func(k *approval.Kind) { k.MinReason = -1 },
	} {
		k := approvaltest.New("test."+name, drv).Definition()
		k.Name = "test.other"
		mutate(&k)
		assert.Error(t, reg.Register(k), name)
	}
	assert.Equal(t, []string{"test.valid"}, reg.Names())
	_, ok := reg.Lookup("test.valid")
	assert.True(t, ok)
	_, ok = reg.Lookup("test.other")
	assert.False(t, ok)

	_, err := approval.NewService(drv.Approvals(), drv.AuditLog(), reg, approval.Options{})
	assert.Error(t, err, "a pending TTL is required")
	_, err = approval.NewService(nil, drv.AuditLog(), reg, approval.Options{PendingTTL: ttl})
	assert.Error(t, err)
}

// ByScope admits holders of the scope, the owner included only while it
// holds it; OwnerOnly admits the owner holding the scope, nobody else.
func TestEligibilityHelpers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	narrowed := &auth.Principal{ID: storage.OwnerPrincipal, Kind: storage.PrincipalKindOwner, Scopes: []auth.Scope{auth.ScopeReadObjects, auth.ScopeWriteSelf}}
	byScope := approval.ByScope(auth.ScopeApproveSteward)
	for p, want := range map[*auth.Principal]bool{f.owner: true, narrowed: false, f.alice: false, f.outsider: false} {
		ok, err := byScope(ctx, nil, p)
		require.NoError(t, err)
		assert.Equal(t, want, ok, p.ID)
	}
	steward := &auth.Principal{ID: "prn_s", Kind: storage.PrincipalKindPerson, Scopes: []auth.Scope{auth.ScopeApproveSteward}}
	ok, _ := byScope(ctx, nil, steward)
	assert.True(t, ok)
	ownerOnly := approval.OwnerOnly(auth.ScopeApproveSteward)
	for p, want := range map[*auth.Principal]bool{f.owner: true, narrowed: false, f.alice: false, steward: false} {
		ok, err := ownerOnly(ctx, nil, p)
		require.NoError(t, err)
		assert.Equal(t, want, ok, p.ID)
	}
}
