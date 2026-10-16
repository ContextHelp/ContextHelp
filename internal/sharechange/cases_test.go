package sharechange_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// wideCase is one widening row of ADR-083 §10 B's route table: how to
// reach the state the change widens, the change, what moves the state
// it was classified against, and who benefits.
type wideCase struct {
	name string
	// setup puts the registry in the state the change widens.
	setup func(f *fx)
	// change is the change as the route builds it.
	change func(f *fx) sharing.Change
	// move changes what the request's preconditions read, as an owner's
	// direct change would: the last approval finds the request stale.
	move func(f *fx)
	// event is the audit event the applied change is recorded under.
	event string
	// applied checks the registry after the change was applied by actor.
	applied func(t *testing.T, f *fx, actor string)
	// beneficiary benefits from the change and holds approve:shares;
	// other is a share approver it does not benefit.
	beneficiary, other func(f *fx) *auth.Principal
	// subject is the principal the request is about: "" when none.
	subject func(f *fx) string
}

func dana(f *fx) *auth.Principal  { return f.dana }
func alice(f *fx) *auth.Principal { return f.alice }
func bob(f *fx) *auth.Principal   { return f.bob }

func subjectDana(f *fx) string  { return f.dana.ID }
func subjectAlice(f *fx) string { return f.alice.ID }
func noSubject(*fx) string      { return "" }

// ownerApplies applies c as the owner and requires it to apply at once.
func (f *fx) ownerApplies(c sharing.Change) {
	f.t.Helper()
	out := f.submit(f.owner, c)
	require.NotNil(f.t, out.Applied, "%s: the owner's change was not applied", c.Action())
	require.Nil(f.t, out.Request)
}

func wideCases() []wideCase {
	return []wideCase{
		{
			name:   "PUT member, new membership",
			change: func(f *fx) sharing.Change { return sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID} },
			move:   func(f *fx) { f.join(f.dana.ID, nil) },
			event:  sharechange.EventMemberAdded,
			applied: func(t *testing.T, f *fx, actor string) {
				ms, err := f.drv.Sharing().Audiences().ListMembers(context.Background(), f.team.ID)
				require.NoError(t, err)
				require.Len(t, ms, 1)
				assert.Equal(t, f.dana.ID, ms[0].PrincipalID)
				assert.Equal(t, actor, ms[0].AddedBy, "the membership is the requester's change")
			},
			subject: subjectDana, beneficiary: dana, other: alice,
		},
		{
			name:  "PUT member, expiry extended",
			setup: func(f *fx) { f.join(f.dana.ID, f.clock.at(time.Hour)) },
			change: func(f *fx) sharing.Change {
				return sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID, ExpiresAt: f.clock.at(2 * time.Hour)}
			},
			move:  func(f *fx) { f.join(f.dana.ID, f.clock.at(90*time.Minute)) },
			event: sharechange.EventMemberAdded,
			applied: func(t *testing.T, f *fx, actor string) {
				ms, err := f.drv.Sharing().Audiences().ListMembers(context.Background(), f.team.ID)
				require.NoError(t, err)
				require.Len(t, ms, 1)
				require.NotNil(t, ms[0].ExpiresAt)
				assert.Equal(t, f.clock.at(2*time.Hour).UTC(), ms[0].ExpiresAt.UTC())
			},
			subject: subjectDana, beneficiary: dana, other: alice,
		},
		{
			name:  "PUT policy, widened with a grant in force",
			setup: func(f *fx) { f.grant(f.base); f.join(f.bob.ID, nil) },
			change: func(f *fx) sharing.Change {
				return sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: widerBase()}
			},
			move:  func(f *fx) { f.ownerApplies(sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: narrowerBase()}) },
			event: sharechange.EventPolicyUpdated,
			applied: func(t *testing.T, f *fx, actor string) {
				row, err := f.drv.Sharing().Policies().Get(context.Background(), f.base.ID)
				require.NoError(t, err)
				assert.Equal(t, 2, row.Version)
				assert.Contains(t, string(row.Spec), "email")
			},
			subject: noSubject, beneficiary: bob, other: alice,
		},
		{
			name:   "POST grant",
			setup:  func(f *fx) { f.join(f.bob.ID, nil) },
			change: func(f *fx) sharing.Change { return sharing.GrantCreate{AudienceID: f.team.ID, PolicyID: f.base.ID} },
			// A policy edit with no grant in force applies at once; the
			// request was filed against the policy's earlier version.
			move: func(f *fx) {
				out := f.submit(f.admin, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: widerBase()})
				require.NotNil(f.t, out.Applied, "an edit of an ungranted policy applies at once")
			},
			event: sharechange.EventGrantCreated,
			applied: func(t *testing.T, f *fx, actor string) {
				gs, err := f.drv.Sharing().Grants().List(context.Background(), storage.GrantFilter{AudienceID: f.team.ID})
				require.NoError(t, err)
				require.Len(t, gs, 1)
				assert.Equal(t, f.base.ID, gs[0].PolicyID)
				assert.Equal(t, actor, gs[0].CreatedBy)
			},
			subject: noSubject, beneficiary: bob, other: alice,
		},
		{
			name: "PUT ceiling, lowered",
			setup: func(f *fx) {
				require.NoError(f.t, f.drv.Sharing().SetDenyClasses(context.Background(), f.alice.ID, []string{storage.ClassPersonal}))
			},
			change: func(f *fx) sharing.Change { return sharing.CeilingSet{PrincipalID: f.alice.ID} },
			move: func(f *fx) {
				f.ownerApplies(sharing.CeilingSet{PrincipalID: f.alice.ID, DenyClasses: []string{storage.ClassPersonal, storage.ClassContactDetails}})
			},
			event: sharechange.EventCeilingSet,
			applied: func(t *testing.T, f *fx, actor string) {
				got, err := f.drv.Sharing().DenyClasses(context.Background(), f.alice.ID)
				require.NoError(t, err)
				assert.Empty(t, got)
			},
			subject: subjectAlice, beneficiary: alice, other: bob,
		},
		{
			name: "PUT key policy, bound to unbound",
			setup: func(f *fx) {
				require.NoError(f.t, f.drv.Sharing().BindIdentityPolicy(context.Background(), f.key.ID, f.base.ID))
			},
			change: func(f *fx) sharing.Change {
				return sharing.KeyPolicyBind{KeyID: "key_dana", IdentityID: f.key.ID, PrincipalID: f.dana.ID}
			},
			move: func(f *fx) {
				f.ownerApplies(sharing.KeyPolicyBind{KeyID: "key_dana", IdentityID: f.key.ID, PrincipalID: f.dana.ID, PolicyID: f.other.ID})
			},
			event: sharechange.EventKeyPolicyBound,
			applied: func(t *testing.T, f *fx, actor string) {
				got, err := f.drv.Sharing().IdentityPolicy(context.Background(), f.key.ID)
				require.NoError(t, err)
				assert.Empty(t, got)
			},
			subject: subjectDana, beneficiary: dana, other: alice,
		},
	}
}
