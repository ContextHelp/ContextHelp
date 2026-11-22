package sharechange_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// recordedAlerts collects what a security emitter dispatched.
type recordedAlerts struct {
	mu   sync.Mutex
	list []security.Alert
}

func (a *recordedAlerts) record(_ context.Context, al security.Alert) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.list = append(a.list, al)
}

func (a *recordedAlerts) wait(n int) []security.Alert {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		got := len(a.list)
		a.mu.Unlock()
		if got >= n {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]security.Alert(nil), a.list...)
}

func alertKinds(list []security.Alert) []string {
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.Kind
	}
	sort.Strings(out)
	return out
}

// Each transition of a share.change request reaches the security
// emitter with its actor and ids, never the payload: filed, each
// decision, applied, rejected, withdrawn and stale.
func TestEvents_ShareChangeTransitions(t *testing.T) {
	sec := security.New(security.DefaultConfig(), nil)
	t.Cleanup(func() { _ = sec.Close(context.Background()) })
	rec := &recordedAlerts{}
	sec.AddHandler(rec.record)
	f := newFx(t, sqliteDriver, withNotify(sharechange.ApprovalNotices(sec)))
	ctx := context.Background()

	applied := f.pending(f.admin, sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID})
	_, err := f.approve(applied, f.alice)
	require.NoError(t, err)

	rejected := f.pending(f.admin, sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.erin.ID})
	_, err = f.reject(rejected, f.alice)
	require.NoError(t, err)

	withdrawn := f.pending(f.admin, sharing.GrantCreate{AudienceID: f.team.ID, PolicyID: f.base.ID})
	_, err = f.appr.Withdraw(ctx, withdrawn.ID, f.admin)
	require.NoError(t, err)

	stale := f.pending(f.admin, sharing.GrantCreate{AudienceID: f.team.ID, PolicyID: f.other.ID})
	edit := f.submit(f.admin, sharing.PolicyUpdate{PolicyID: f.other.ID, Policy: sharing.Policy{
		Name: "other", Data: "type==note", Fields: map[string][]string{"note": {"title", "body"}},
	}})
	require.NotNil(t, edit.Applied, "no grant is in force yet: the edit applies and moves the pending grant's policy")
	_, err = f.approve(stale, f.alice)
	requireCode(t, err, approval.CodeStale)

	got := rec.wait(10)
	assert.Equal(t, []string{
		security.EventShareChangeApplied,
		security.EventShareChangeDecided, security.EventShareChangeDecided, security.EventShareChangeDecided,
		security.EventShareChangeFiled, security.EventShareChangeFiled, security.EventShareChangeFiled, security.EventShareChangeFiled,
		security.EventShareChangeRejected, security.EventShareChangeStale, security.EventShareChangeWithdrawn,
	}, alertKinds(got))
	for _, a := range got {
		assert.NotEmpty(t, a.Meta["request_id"], a.Kind)
		assert.NotEmpty(t, a.Meta["action"], a.Kind)
		assert.Equal(t, f.admin.ID, a.Meta["requested_by"], a.Kind)
	}
}

// failCommit is a SharingStore whose transaction runs and then reports
// that it failed, the way a failed commit does.
type failCommit struct{ storage.SharingStore }

func (s failCommit) InTx(ctx context.Context, fn func(context.Context) error) error {
	if err := s.SharingStore.InTx(ctx, fn); err != nil {
		return err
	}
	return errors.New("commit failed")
}

// A notice leaves only once the transaction of the change committed:
// the filing of a change whose transaction fails emits nothing, and one
// that commits emits its filing once.
func TestSubmit_NoticeOfAFilingWaitsForTheCommit(t *testing.T) {
	var mu sync.Mutex
	var notices []string
	notify := func(_ context.Context, n approval.Notice) {
		mu.Lock()
		defer mu.Unlock()
		notices = append(notices, n.Event)
	}
	seen := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), notices...)
	}
	change := func(f *fx) sharing.Change { return sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID} }

	failing := newFx(t, sqliteDriver, withNotify(notify), withSharing(func(d storage.StorageDriver) storage.SharingStore {
		return failCommit{d.Sharing()}
	}))
	_, err := failing.svc.Submit(context.Background(), failing.admin, change(failing))
	require.Error(t, err)
	assert.Empty(t, seen(), "a failed commit emits nothing")

	ok := newFx(t, sqliteDriver, withNotify(notify))
	ok.pending(ok.admin, change(ok))
	assert.Equal(t, []string{approval.EventFiled}, seen())
}
