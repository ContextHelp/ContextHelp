package sharechange_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// A key bound to a policy sees its principal's view intersected with
// the policy, so editing the policy changes what the key sees even when
// no grant of it is in force (ADR-083 §10 B).
func (f *fx) bindKey() {
	f.t.Helper()
	require.NoError(f.t, f.drv.Sharing().BindIdentityPolicy(context.Background(), f.key.ID, f.base.ID))
}

func TestKeyBound_PolicyEditThatShowsMoreWaitsWithNoGrantInForce(t *testing.T) {
	for name, edit := range map[string]sharing.Policy{
		"filter": {Name: "base", Data: "type==contact,type==note", Fields: map[string][]string{"contact": {"title", "org"}}},
		"fields": widerBase(),
		"export": {Name: "base", Data: "type==contact", Fields: map[string][]string{"contact": {"title", "org"}}, Capabilities: sharing.Capabilities{Export: true}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFx(t, sqliteDriver)
			f.bindKey()
			before := f.snapshot()
			out := f.submit(f.admin, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: edit})
			require.Nil(t, out.Applied, "a widening edit of a bound policy applied: %+v", out.Class)
			require.NotNil(t, out.Request)
			assert.Equal(t, sharing.Widening, out.Class.Class)
			assert.Contains(t, out.Class.Beneficiaries, f.dana.ID)
			assert.Equal(t, before, f.snapshot(), "pending writes nothing")
		})
	}
}

func TestKeyBound_NarrowingEditApplies(t *testing.T) {
	f := newFx(t, sqliteDriver)
	f.bindKey()
	out := f.submit(f.admin, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: narrowerBase()})
	require.NotNil(t, out.Applied)
	assert.Equal(t, sharing.Narrowing, out.Class.Class)
}

// The principal of the bound key benefits from the edit: it cannot
// approve it, however much it holds approve:shares.
func TestKeyBound_PrincipalOfTheKeyCannotApprove(t *testing.T) {
	f := newFx(t, sqliteDriver)
	f.bindKey()
	r := f.pending(f.admin, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: widerBase()})
	before := f.snapshot()
	_, err := f.approve(r, f.dana)
	requireCode(t, err, approval.CodeBeneficiaryCannotDecide)
	assert.Equal(t, before, f.snapshot())
	_, err = f.approve(r, f.alice)
	require.NoError(t, err)
}

// Binding or unbinding a key while the edit is pending moves what the
// request was filed against: it ends stale and applies nothing.
func TestKeyBound_BindingChangeMakesAPendingEditStale(t *testing.T) {
	for name, move := range map[string]func(f *fx){
		"unbind": func(f *fx) {
			require.NoError(f.t, f.drv.Sharing().BindIdentityPolicy(context.Background(), f.key.ID, ""))
		},
		"bind another key": func(f *fx) {
			k := &storage.Identity{PrincipalID: f.bob.ID, Method: storage.IdentityMethodAPIKey, Subject: "bob-key", CreatedAt: f.clock.now()}
			require.NoError(f.t, f.drv.Auth().Identities().Create(context.Background(), k))
			require.NoError(f.t, f.drv.Sharing().BindIdentityPolicy(context.Background(), k.ID, f.base.ID))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFx(t, sqliteDriver)
			f.bindKey()
			r := f.pending(f.admin, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: widerBase()})
			move(f)
			before := f.snapshot()
			_, err := f.approve(r, f.alice)
			requireCode(t, err, approval.CodeStale)
			assert.Equal(t, before, f.snapshot(), "a stale request applies nothing")
		})
	}
}
