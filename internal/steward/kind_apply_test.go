package steward_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The N-th approval writes the grant row in the same transaction as
// the approval: nothing before it, the frozen scope and window after
// it, the request pointing at it and an audit entry naming the
// approvers.
func TestFinalApprovalWritesTheGrant(t *testing.T) {
	f := newFixture(t)
	f.required = 2
	r := f.mustFile(f.request())

	d, err := f.approve(r, f.alice)
	require.NoError(t, err)
	assert.Equal(t, approval.StateRequested, d.State)
	_, err = f.sharing.StewardGrants().GetByRequest(t.Context(), r.ID)
	assert.ErrorIs(t, err, storage.ErrNotFound, "one approval of two writes nothing")

	f.clock.advance(time.Hour)
	d, err = f.approve(r, f.bob)
	require.NoError(t, err)
	assert.Equal(t, approval.StateApproved, d.State)

	g, err := f.sharing.StewardGrants().GetByRequest(t.Context(), r.ID)
	require.NoError(t, err)
	p, err := steward.DecodePayload(r.Payload)
	require.NoError(t, err)
	assert.Equal(t, d.Request.ResultRef, g.ID, "the request names what it produced")
	assert.Equal(t, storage.StewardGrantApproved, g.Status)
	assert.Equal(t, f.stewardP.ID, g.StewardID)
	assert.Equal(t, "CASE-117", g.MatterRef)
	assert.JSONEq(t, string(p.Scope), string(g.Scope))
	assert.Equal(t, p.ScopeDigest, g.ScopeDigest)
	assert.True(t, g.NotBefore.Equal(p.NotBefore) && g.ExpiresAt.Equal(p.ExpiresAt))
	assert.True(t, g.ApprovedAt.Equal(f.clock.now()), "approved_at is the final approval's moment")
	assert.Empty(t, g.RenewsID)

	created := f.audit(steward.EventGrantCreated)
	require.Len(t, created, 1)
	assert.Equal(t, f.requester.ID, created[0].Actor, "the grant is recorded with the requester as the actor")
	assert.Equal(t, g.ID, created[0].Payload["grant_id"])
	assert.Equal(t, r.ID, created[0].Payload["request_id"])
	assert.Equal(t, f.stewardP.ID, created[0].Payload["steward_id"])
	assert.Equal(t, p.ScopeDigest, created[0].Payload["scope_digest"])
	assert.ElementsMatch(t, []any{f.alice.ID, f.bob.ID}, created[0].Payload["approvers"])
	assert.NotContains(t, created[0].Payload, "reason", "the reason stays with the request")
}

// failingCreate makes the grant write fail.
type failingCreate struct {
	storage.SharingStore
	err error
}

func (s failingCreate) StewardGrants() storage.StewardGrantStore {
	return failingCreateGrants{StewardGrantStore: s.SharingStore.StewardGrants(), err: s.err}
}

type failingCreateGrants struct {
	storage.StewardGrantStore
	err error
}

func (s failingCreateGrants) Create(context.Context, *storage.StewardGrant) error { return s.err }

// A grant that cannot be written takes the approval with it: the
// request stays pending, with no decision, no grant and no audit entry
// of an approval.
func TestFailedGrantWriteRollsBackTheApproval(t *testing.T) {
	f := newFixture(t)
	reg := approval.NewRegistry()
	boom := errors.New("disk full")
	kind, err := steward.Kind(f.kindOptions(failingCreate{SharingStore: f.sharing, err: boom}))
	require.NoError(t, err)
	require.NoError(t, reg.Register(kind))
	svc, err := approval.NewService(f.drv.Approvals(), f.drv.AuditLog(), reg, approval.Options{PendingTTL: ttl, Now: f.clock.now})
	require.NoError(t, err)

	r, err := svc.File(t.Context(), f.filing(f.requester, f.request()))
	require.NoError(t, err)
	_, err = svc.Approve(t.Context(), r.ID, f.alice, r.PayloadDigest, "ok")
	require.ErrorIs(t, err, boom)

	got := f.stored(r.ID)
	assert.Equal(t, storage.ApprovalRequested, got.Status)
	assert.Empty(t, got.Decisions, "the decision rolled back with the grant")
	assert.Empty(t, got.ResultRef)
	_, err = f.sharing.StewardGrants().GetByRequest(t.Context(), r.ID)
	assert.ErrorIs(t, err, storage.ErrNotFound)
	assert.Empty(t, f.audit(steward.EventGrantCreated))
	assert.Empty(t, f.audit(approval.EventApproved))
}

// The requester is never an eligible approver of its own request, even
// holding the approver bundle; neither is the steward; a holder of the
// bundle is; the owner is; a principal without it cannot even see the
// request.
func TestApproverSeparation(t *testing.T) {
	f := newFixture(t)

	// A requester who also holds steward-approver.
	both := f.person("prn_both", auth.RoleReader, auth.RoleStewardRequester, auth.RoleStewardApprover)
	r, err := f.svc.File(t.Context(), f.filing(both, f.request()))
	require.NoError(t, err)
	_, err = f.approve(r, both)
	requireCode(t, err, approval.CodeRequesterCannotDecide)

	// A steward who also holds steward-approver.
	f.setRoles(f.stewardP.ID, auth.RoleReader, auth.RoleStewardRequester, auth.RoleStewardApprover)
	stewardApprover := &auth.Principal{ID: f.stewardP.ID, Kind: storage.PrincipalKindPerson, Roles: []string{auth.RoleStewardApprover}, Scopes: []auth.Scope{auth.ScopeApproveSteward}}
	r2 := f.mustFile(f.request())
	_, err = f.approve(r2, stewardApprover)
	requireCode(t, err, approval.CodeBeneficiaryCannotDecide)

	_, err = f.approve(r2, f.outsider)
	assert.ErrorIs(t, err, storage.ErrNotFound, "a principal that may not decide it cannot see it")
	_, err = f.approve(r2, f.plain)
	assert.ErrorIs(t, err, storage.ErrNotFound)

	// A device holding approve:steward is no approver, and cannot see the
	// request: only a person or the owner decides.
	sharingtest.EnsurePrincipal(t, f.drv, "prn_device_approver", storage.PrincipalKindDevice, auth.RoleStewardApprover)
	device := &auth.Principal{ID: "prn_device_approver", Kind: storage.PrincipalKindDevice, Roles: []string{auth.RoleStewardApprover}, Scopes: []auth.Scope{auth.ScopeApproveSteward}}
	_, err = f.approve(r2, device)
	assert.ErrorIs(t, err, storage.ErrNotFound, "a device approver is not eligible")

	_, err = f.approve(r2, f.owner)
	require.NoError(t, err, "the owner is an eligible approver")
	g, err := f.sharing.StewardGrants().GetByRequest(t.Context(), r2.ID)
	require.NoError(t, err)
	assert.Equal(t, f.stewardP.ID, g.StewardID)
}

// Digest binding: an approval names the digest of the request it
// approves. A different scope is a different request with a different
// digest, so approving one never approves the other, and what is
// written is the scope that was approved.
func TestApprovalBindsToTheScopeDigest(t *testing.T) {
	f := newFixture(t)
	narrow := f.mustFile(f.request())
	wide := f.request()
	wide.Scope = json.RawMessage(`{"data":"type!=__none__","fields":{"*":["*"]}}`)
	wideReq := f.mustFile(wide)
	require.NotEqual(t, narrow.PayloadDigest, wideReq.PayloadDigest)

	_, err := f.svc.Approve(t.Context(), narrow.ID, f.alice, wideReq.PayloadDigest, "meant the other")
	requireCode(t, err, approval.CodeDigestMismatch)
	_, err = f.sharing.StewardGrants().GetByRequest(t.Context(), narrow.ID)
	assert.ErrorIs(t, err, storage.ErrNotFound)

	_, err = f.approve(narrow, f.alice)
	require.NoError(t, err)
	g, err := f.sharing.StewardGrants().GetByRequest(t.Context(), narrow.ID)
	require.NoError(t, err)
	assert.JSONEq(t, `{"policies":[`+familyScope+`]}`, string(g.Scope), "the grant is the narrow scope, whatever else was filed")
	_, err = f.sharing.StewardGrants().GetByRequest(t.Context(), wideReq.ID)
	assert.ErrorIs(t, err, storage.ErrNotFound)
}

// With N = 2, a payload rewritten consistently between the approvals
// (the scope of a wider request copied in, the scope digest and the
// payload digest recomputed, so every self-check of the apply holds) is
// not applied: the first approval bound to the narrow payload, so the
// request ends stale and no grant is written.
func TestApprovalsBindAcrossDecisions(t *testing.T) {
	f := newFixture(t)
	f.required = 2
	r := f.mustFile(f.request())
	_, err := f.approve(r, f.alice)
	require.NoError(t, err)

	wide := f.request()
	wide.Scope = json.RawMessage(`{"data":"type!=__none__","fields":{"*":["*"]}}`)
	wp, err := steward.DecodePayload(f.mustFile(wide).Payload)
	require.NoError(t, err)
	p, err := steward.DecodePayload(r.Payload)
	require.NoError(t, err)
	p.Scope, p.ScopeDigest = wp.Scope, wp.ScopeDigest
	moved, err := json.Marshal(p)
	require.NoError(t, err)
	canon, err := approval.Canonicalize(moved)
	require.NoError(t, err)
	digest := approval.Digest(canon)
	require.NotEqual(t, r.PayloadDigest, digest)
	f.exec(`UPDATE approval_requests SET payload = ?, payload_digest = ? WHERE id = ?`, string(canon), digest, r.ID)

	_, err = f.svc.Approve(t.Context(), r.ID, f.bob, digest, "ok")
	requireCode(t, err, approval.CodeStale)
	assert.Equal(t, storage.ApprovalStale, f.stored(r.ID).Status)
	_, gerr := f.sharing.StewardGrants().GetByRequest(t.Context(), r.ID)
	assert.ErrorIs(t, gerr, storage.ErrNotFound, "no grant from a payload one approver never saw")
}

// A payload edited behind the application's back, with its digest
// left alone, is not applied: the final approval recomputes the digest
// the approvers bound to.
func TestApplyRefusesAPayloadThatMovedUnderItsDigest(t *testing.T) {
	f := newFixture(t)
	r := f.mustFile(f.request())
	p, err := steward.DecodePayload(r.Payload)
	require.NoError(t, err)
	p.Scope = json.RawMessage(`{"policies":[{"data":"type!=__none__","fields":{"*":["*"]}}]}`)
	p.ScopeDigest, err = sharing.StewardScopeDigest(p.Scope)
	require.NoError(t, err)
	tampered, err := json.Marshal(p)
	require.NoError(t, err)
	f.exec(`UPDATE approval_requests SET payload = ? WHERE id = ?`, string(tampered), r.ID)

	_, err = f.approve(r, f.alice)
	require.Error(t, err)
	assert.Equal(t, storage.ApprovalRequested, f.stored(r.ID).Status, "nothing applied, nothing decided")
	_, gerr := f.sharing.StewardGrants().GetByRequest(t.Context(), r.ID)
	assert.ErrorIs(t, gerr, storage.ErrNotFound)

	// The scope inside the payload moved but the payload's own digest
	// follows it: the scope digest the kind froze no longer matches.
	f2 := newFixture(t)
	r2 := f2.mustFile(f2.request())
	p2, _ := steward.DecodePayload(r2.Payload)
	p2.Scope = json.RawMessage(`{"policies":[{"data":"type!=__none__"}]}`)
	moved, _ := json.Marshal(p2)
	canon, err := approval.Canonicalize(moved)
	require.NoError(t, err)
	f2.exec(`UPDATE approval_requests SET payload = ?, payload_digest = ? WHERE id = ?`, string(canon), approval.Digest(canon), r2.ID)
	_, err = f2.svc.Approve(t.Context(), r2.ID, f2.alice, approval.Digest(canon), "ok")
	require.Error(t, err, "the scope no longer matches the digest the kind froze with it")
	_, gerr = f2.sharing.StewardGrants().GetByRequest(t.Context(), r2.ID)
	assert.ErrorIs(t, gerr, storage.ErrNotFound)
}

// The kind's preconditions: what the payload was frozen against must
// hold at the last approval, else the request ends stale and nothing
// is written.
func TestPreconditionsEndTheRequestStale(t *testing.T) {
	for name, c := range map[string]struct {
		change func(f *fixture)
	}{
		"the steward lost the role": {func(f *fixture) { f.setRoles(f.stewardP.ID, auth.RoleReader) }},
		"the steward was suspended": {func(f *fixture) {
			require.NoError(f.t, f.drv.Auth().Principals().Suspend(f.t.Context(), f.stewardP.ID))
		}},
		"the feature was turned off":    {func(f *fixture) { f.settings.Enabled = false }},
		"the cap fell below the window": {func(f *fixture) { f.settings.MaxDuration = time.Hour }},
		"the window passed":             {func(f *fixture) { f.clock.advance(49 * time.Hour) }},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			r := f.mustFile(f.request())
			c.change(f)
			_, err := f.approve(r, f.alice)
			requireCode(t, err, approval.CodeStale)
			got := f.stored(r.ID)
			assert.Equal(t, storage.ApprovalStale, got.Status)
			_, gerr := f.sharing.StewardGrants().GetByRequest(t.Context(), r.ID)
			assert.ErrorIs(t, gerr, storage.ErrNotFound, "a stale request writes no grant")
			assert.Empty(t, f.audit(steward.EventGrantCreated))
		})
	}
}

// A request is not stale for a change that does not touch it.
func TestPreconditionsHoldThroughUnrelatedChanges(t *testing.T) {
	f := newFixture(t)
	r := f.mustFile(f.request())
	f.settings.AllowExport = true
	f.settings.WithheldClasses = []string{"personal", "financial"}
	f.clock.advance(time.Hour)
	f.setRoles(f.stewardP.ID, auth.RoleReader, auth.RoleStewardRequester, auth.RoleShareApprover)
	_, err := f.approve(r, f.alice)
	require.NoError(t, err)
	g, err := f.sharing.StewardGrants().GetByRequest(t.Context(), r.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.StewardGrantApproved, g.Status)
}

// End to end: filed, approved by two, used, and an elevated request
// after the window is refused.
func TestApprovedGrantIsUsableUntilItEnds(t *testing.T) {
	f := newFixture(t)
	f.seedCorpus()
	f.required = 2
	g := f.approved(f.request())

	v, err := f.elevated(g)
	require.NoError(t, err)
	assert.Equal(t, want("c2"), f.visible(v))

	f.clock.advance(48*time.Hour - time.Millisecond)
	_, err = f.elevated(g)
	require.NoError(t, err, "one tick before the end of the window")
	f.clock.advance(time.Millisecond)
	_, err = f.elevated(g)
	var refusal *sharing.StewardRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, sharing.StewardReasonGrantExpired, refusal.Reason)
}
