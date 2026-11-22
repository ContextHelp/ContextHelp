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

// An approver is checked against the beneficiaries when they approve.
// One who becomes a beneficiary before the final approval (here alice
// joins the audience a pending grant reaches) has approved a change for
// themselves: the request ends stale, nothing is applied, and the
// requester files it again.
func TestApprove_ApproverWhoBecameABeneficiaryMakesTheRequestStale(t *testing.T) {
	f := newFx(t, sqliteDriver, withRequired(2))
	r := f.pending(f.admin, sharing.GrantCreate{AudienceID: f.team.ID, PolicyID: f.base.ID})
	d, err := f.approve(r, f.alice)
	require.NoError(t, err)
	assert.Equal(t, approval.StateRequested, d.State)

	f.join(f.alice.ID, nil) // the owner adds the first approver to the audience
	before := f.snapshot()
	_, err = f.approve(r, f.bob)
	requireCode(t, err, approval.CodeStale)
	assert.Equal(t, storage.ApprovalStale, f.stored(r.ID).Status)
	assert.Equal(t, before, f.snapshot(), "a stale request applies nothing")
	gs, err := f.drv.Sharing().Grants().List(context.Background(), storage.GrantFilter{})
	require.NoError(t, err)
	assert.Empty(t, gs, "the grant was not made")
}

// lockOrder records the order the registry is locked and read in.
type lockOrder struct {
	storage.SharingStore
	calls *[]string
}

func (l lockOrder) LockRegistry(ctx context.Context) error {
	*l.calls = append(*l.calls, "lock")
	return l.SharingStore.LockRegistry(ctx)
}

func (l lockOrder) Audiences() storage.AudienceStore {
	*l.calls = append(*l.calls, "read")
	return l.SharingStore.Audiences()
}

// The beneficiaries a decision is separated by are read under the
// registry lock, before any registry read.
func TestDecide_BeneficiariesAreReadUnderTheRegistryLock(t *testing.T) {
	var calls []string
	f := newFx(t, sqliteDriver, withSharing(func(d storage.StorageDriver) storage.SharingStore {
		return lockOrder{SharingStore: d.Sharing(), calls: &calls}
	}))
	r := f.pending(f.admin, sharing.GrantCreate{AudienceID: f.team.ID, PolicyID: f.base.ID})
	calls = nil
	_, err := f.approve(r, f.alice)
	require.NoError(t, err)
	require.NotEmpty(t, calls)
	assert.Equal(t, "lock", calls[0], "the first registry access of a decision is the lock: %v", calls)
}
