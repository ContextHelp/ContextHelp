package sharechange_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The owner's changes are never pending: they apply at once, are
// recorded under the owner, and file nothing, whatever their class.
func TestSubmit_OwnerChangesAreNeverPending(t *testing.T) { ownerNeverPending(t, sqliteDriver) }

func ownerNeverPending(t *testing.T, newDriver driverFunc) {
	for _, c := range wideCases() {
		t.Run(c.name, func(t *testing.T) {
			f := newFx(t, newDriver)
			if c.setup != nil {
				c.setup(f)
			}
			before := f.snapshot()
			out := f.submit(f.owner, c.change(f))
			require.NotNil(t, out.Applied, "the owner's widening change applies at once")
			assert.Nil(t, out.Request)
			assert.NotEqual(t, before, f.snapshot())
			c.applied(t, f, storage.OwnerPrincipal)
			events := f.shareEvents()
			require.NotEmpty(t, events)
			assert.Equal(t, c.event, events[len(events)-1].EventType)
			assert.Equal(t, storage.OwnerPrincipal, events[len(events)-1].Actor)
			assert.Empty(t, f.eventsOf("approval.request.filed"), "the owner files nothing")
		})
	}
}

// A widening change by a non-owner administrator changes nothing: the
// registry rows are untouched, no share.* entry is written, and the
// audit log shows only that the request was filed (ADR-083 §10 B).
func TestSubmit_WideningByAdminIsPendingAndChangesNothing(t *testing.T) {
	widening(t, sqliteDriver)
}

func widening(t *testing.T, newDriver driverFunc) {
	for _, c := range wideCases() {
		t.Run(c.name, func(t *testing.T) {
			f := newFx(t, newDriver)
			if c.setup != nil {
				c.setup(f)
			}
			before := f.snapshot()
			change := c.change(f)
			r := f.pending(f.admin, change)

			assert.Equal(t, sharechange.KindName, r.Kind)
			assert.Equal(t, change.Action(), r.Action)
			assert.Equal(t, f.admin.ID, r.RequestedBy)
			assert.Equal(t, c.subject(f), r.SubjectID)
			assert.Equal(t, 1, r.ApprovalsRequired, "approvals.share.required defaults to 1")
			assert.Equal(t, storage.ApprovalRequested, r.Status)
			assert.Equal(t, f.clock.now().Add(pendingTTL), r.ExpiresAt)
			assert.JSONEq(t, string(mustEncode(t, change)), string(r.Payload), "the payload is the change, frozen")
			if subject := c.subject(f); subject != "" {
				// The principal the change is about sees the request, and may not decide it.
				p := map[string]*auth.Principal{f.dana.ID: f.dana, f.alice.ID: f.alice}[subject]
				it, err := f.appr.Get(context.Background(), r.ID, p)
				require.NoError(t, err, "the subject sees the request")
				assert.False(t, it.CanDecide)
			}

			assert.Equal(t, before, f.snapshot(), "a pending change writes nothing to the registry")
			assert.Empty(t, f.shareEvents(), "a pending change is on the record only as a request")
			filed := f.eventsOf("approval.request.filed")
			require.Len(t, filed, 1)
			assert.Equal(t, f.admin.ID, filed[0].Actor)
			assert.Equal(t, r.ID, filed[0].Payload["request_id"])
			assert.Equal(t, sharechange.KindName, filed[0].Payload["kind"])
		})
	}
}

func mustEncode(t *testing.T, c sharing.Change) []byte {
	t.Helper()
	b, err := sharechange.Encode(c)
	require.NoError(t, err)
	return b
}

// A change that narrows or changes no view applies at once even from a
// non-owner administrator, recorded under it (ADR-083 §10 B).
func TestSubmit_NarrowingAndNeutralApplyAtOnceFromAdmin(t *testing.T) {
	narrowing(t, sqliteDriver)
}

func narrowing(t *testing.T, newDriver driverFunc) {
	cases := []struct {
		name   string
		setup  func(f *fx)
		change func(f *fx) sharing.Change
		class  sharing.Class
		event  string
	}{
		{"POST audience", nil, func(*fx) sharing.Change { return sharing.AudienceCreate{Name: "partners"} }, sharing.Neutral, sharechange.EventAudienceCreated},
		{"POST policy", nil, func(*fx) sharing.Change { return sharing.PolicyCreate{Policy: widerBase2()} }, sharing.Neutral, sharechange.EventPolicyCreated},
		{"POST template", nil, func(*fx) sharing.Change { return sharing.TemplateInstall{Name: sharing.TemplatePartners} }, sharing.Neutral, sharechange.EventPolicyCreated},
		{"PUT member, expiry shortened",
			func(f *fx) { f.join(f.erin.ID, f.clock.at(2*time.Hour)) },
			func(f *fx) sharing.Change {
				return sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.erin.ID, ExpiresAt: f.clock.at(time.Hour)}
			}, sharing.Narrowing, sharechange.EventMemberAdded},
		{"DELETE member",
			func(f *fx) { f.join(f.erin.ID, nil) },
			func(f *fx) sharing.Change { return sharing.MemberRemove{AudienceID: f.team.ID, PrincipalID: f.erin.ID} },
			sharing.Narrowing, sharechange.EventMemberRemoved},
		{"PUT policy, no grant in force",
			nil,
			func(f *fx) sharing.Change { return sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: widerBase()} },
			sharing.Neutral, sharechange.EventPolicyUpdated},
		{"PUT policy, narrowed with a grant in force",
			func(f *fx) { f.grant(f.base); f.join(f.erin.ID, nil) },
			func(f *fx) sharing.Change { return sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: narrowerBase()} },
			sharing.Narrowing, sharechange.EventPolicyUpdated},
		{"DELETE grant",
			func(f *fx) { f.grant(f.base) },
			func(f *fx) sharing.Change { return sharing.GrantRevoke{GrantID: f.granted.ID} },
			sharing.Narrowing, sharechange.EventGrantRevoked},
		{"PUT ceiling, raised",
			nil,
			func(f *fx) sharing.Change {
				return sharing.CeilingSet{PrincipalID: f.erin.ID, DenyClasses: []string{storage.ClassPersonal}}
			}, sharing.Narrowing, sharechange.EventCeilingSet},
		{"PUT key policy, unbound to bound",
			nil,
			func(f *fx) sharing.Change {
				return sharing.KeyPolicyBind{KeyID: "key_dana", IdentityID: f.key.ID, PrincipalID: f.dana.ID, PolicyID: f.base.ID}
			}, sharing.Narrowing, sharechange.EventKeyPolicyBound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFx(t, newDriver)
			if c.setup != nil {
				c.setup(f)
			}
			before := f.snapshot()
			out := f.submit(f.admin, c.change(f))
			require.NotNil(t, out.Applied, "%s: %+v", c.name, out.Class)
			assert.Nil(t, out.Request)
			assert.Equal(t, c.class, out.Class.Class, out.Class.Reason)
			assert.NotEqual(t, before, f.snapshot())
			events := f.shareEvents()
			require.NotEmpty(t, events)
			last := events[len(events)-1]
			assert.Equal(t, c.event, last.EventType)
			assert.Equal(t, f.admin.ID, last.Actor, "the change is on the record under the administrator that made it")
			assert.Empty(t, f.eventsOf("approval.request.filed"))
		})
	}
}

// widerBase2 is a policy no other fixture row has.
func widerBase2() sharing.Policy {
	return sharing.Policy{Name: "partners-lite", Data: "type==contact", Fields: map[string][]string{"contact": {"title"}}}
}

// Anything the classifier cannot show to be neutral or narrowing is
// widening: a stored spec that does not decode, a new filter that is
// not a restriction of the old one.
func TestSubmit_UnclassifiableIsWidening(t *testing.T) { unclassifiable(t, sqliteDriver) }

func unclassifiable(t *testing.T, newDriver driverFunc) {
	t.Run("stored spec does not parse", func(t *testing.T) {
		f := newFx(t, newDriver)
		f.grant(f.base)
		f.join(f.erin.ID, nil)
		// The store accepts any JSON object; the compiler does not.
		_, err := f.drv.Sharing().Policies().Update(context.Background(), f.base.ID, []byte(`{"bogus":1}`), f.clock.now())
		require.NoError(t, err)
		r := f.pending(f.admin, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: narrowerBase()})
		assert.Equal(t, sharing.ActionPolicyUpdate, r.Action)
	})
	t.Run("new filter is not a restriction of the old", func(t *testing.T) {
		f := newFx(t, newDriver)
		f.grant(f.base)
		f.join(f.erin.ID, nil)
		f.pending(f.admin, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: sharing.Policy{
			Name: "base", Data: "type==contact,type==note", Fields: map[string][]string{"contact": {"title"}},
		}})
	})
}

// A registry read that fails while classifying is widening; the request
// then cannot be filed against a state that cannot be read, so the
// change ends in an error with nothing written.
func TestSubmit_UnreadableRegistryWritesNothing(t *testing.T) { unreadable(t, sqliteDriver) }

func unreadable(t *testing.T, newDriver driverFunc) {
	var broken bool
	f := newFx(t, newDriver, withSharing(func(d storage.StorageDriver) storage.SharingStore {
		return &hookStore{SharingStore: d.Sharing(), onGrantsList: func() error {
			if broken {
				return errors.New("grants unavailable")
			}
			return nil
		}}
	}))
	f.grant(f.base)
	f.join(f.erin.ID, nil)
	before := f.snapshot()
	broken = true
	_, err := f.svc.Submit(context.Background(), f.admin, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: narrowerBase()})
	require.Error(t, err)
	broken = false
	assert.Equal(t, before, f.snapshot(), "nothing was applied")
	reqs, err := f.drv.Approvals().List(context.Background(), storage.ApprovalFilter{})
	require.NoError(t, err)
	assert.Empty(t, reqs, "nothing was filed")
}

// Without an approval mechanism a widening change by a non-owner has
// nobody to wait for: it is refused and nothing is written. A change
// that does not widen still applies.
func TestSubmit_WideningWithoutApprovalMechanismIsRefused(t *testing.T) {
	f := newFx(t, sqliteDriver, withoutApprovals())
	before := f.snapshot()
	_, err := f.svc.Submit(context.Background(), f.admin, sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID})
	require.ErrorIs(t, err, sharechange.ErrNoApprovals)
	assert.Equal(t, before, f.snapshot())

	out := f.submit(f.admin, sharing.AudienceCreate{Name: "partners"})
	assert.NotNil(t, out.Applied)
	out = f.submit(f.owner, sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID})
	assert.NotNil(t, out.Applied, "the owner needs no mechanism")
}

// Classification and write are one step under the registry lock: a
// change another caller makes while a classification is in progress
// waits until the classified change is written, so the classification
// is of the state it was written against. Here the owner grants the
// policy while an administrator's edit of it is being classified; the
// edit sees no grant and applies, and the grant lands after it.
func TestSubmit_ConcurrentChangeCannotSlipBetweenClassifyAndWrite(t *testing.T) {
	concurrentSlip(t, sqliteDriver)
}

func concurrentSlip(t *testing.T, newDriver driverFunc) {
	var f *fx
	var granted chan error
	var fired bool
	hook := func() error {
		if fired {
			return nil
		}
		fired = true
		// The classifier has just listed the policy's grants: none. A
		// concurrent owner grant now races the administrator's write.
		granted = make(chan error, 1)
		go func() {
			_, err := f.svc.Submit(context.Background(), f.owner, sharing.GrantCreate{AudienceID: f.team.ID, PolicyID: f.base.ID})
			granted <- err
		}()
		select {
		case err := <-granted:
			granted <- err
			t.Errorf("the owner's grant committed (%v) while a classification of the same policy was in progress", err)
		case <-time.After(400 * time.Millisecond):
		}
		return nil
	}
	f = newFx(t, newDriver, withSharing(func(d storage.StorageDriver) storage.SharingStore {
		return &hookStore{SharingStore: d.Sharing(), onGrantsList: hook}
	}))
	f.join(f.erin.ID, nil)

	out := f.submit(f.admin, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: widerBase()})
	require.NotNil(t, out.Applied, "no grant was in force when the edit was classified")
	require.NotNil(t, granted)
	select {
	case err := <-granted:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the owner's grant never ran after the edit committed")
	}
	gs, err := f.drv.Sharing().Grants().List(context.Background(), storage.GrantFilter{PolicyID: f.base.ID})
	require.NoError(t, err)
	assert.Len(t, gs, 1)
	row, err := f.drv.Sharing().Policies().Get(context.Background(), f.base.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, row.Version, "the grant landed on the edited policy, after it")
}

// A change another caller makes while an approval is being applied
// waits for it too: the edit of a policy that races the grant of it
// being approved is classified against the registry with the grant in
// force, so it goes pending instead of widening what the approver just
// granted. (SQLite's write lock gives this whatever the kind does; on
// Postgres only the registry lock the preconditions are read under does.)
func TestApprove_ConcurrentEditWaitsAndIsClassifiedAfterTheApplication(t *testing.T) {
	approveRace(t, sqliteDriver)
}

func approveRace(t *testing.T, newDriver driverFunc) {
	type result struct {
		out *sharechange.Outcome
		err error
	}
	var f *fx
	var edit chan result
	var fired bool
	hook := func() {
		if fired {
			return
		}
		fired = true
		// The grant has just been written in the approval's
		// transaction, which has not committed.
		edit = make(chan result, 1)
		go func() {
			out, err := f.svc.Submit(context.Background(), f.admin2, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: widerBase()})
			edit <- result{out, err}
		}()
		select {
		case r := <-edit:
			edit <- r
			t.Errorf("the concurrent edit finished (%v) while the approval was still applying", r.err)
		case <-time.After(400 * time.Millisecond):
		}
	}
	f = newFx(t, newDriver, withSharing(func(d storage.StorageDriver) storage.SharingStore {
		return &hookStore{SharingStore: d.Sharing(), onGrantCreate: hook}
	}))
	f.join(f.erin.ID, nil)
	r := f.pending(f.admin, sharing.GrantCreate{AudienceID: f.team.ID, PolicyID: f.base.ID})
	_, err := f.approve(r, f.alice)
	require.NoError(t, err)
	require.NotNil(t, edit)

	select {
	case res := <-edit:
		require.NoError(t, res.err)
		assert.Nil(t, res.out.Applied, "the edit found the grant in force")
		require.NotNil(t, res.out.Request, "the edit of a granted policy went pending")
		assert.Equal(t, sharing.Widening, res.out.Class.Class)
	case <-time.After(10 * time.Second):
		t.Fatal("the concurrent edit never ran after the approval committed")
	}
	row, err := f.drv.Sharing().Policies().Get(context.Background(), f.base.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, row.Version, "the granted policy was not widened")
}

// hookStore is a SharingStore whose grant store runs hooks: after a
// listing, after a grant is written.
type hookStore struct {
	storage.SharingStore
	onGrantsList  func() error
	onGrantCreate func()
}

func (h *hookStore) Grants() storage.ShareGrantStore {
	return &hookGrants{ShareGrantStore: h.SharingStore.Grants(), hook: h.onGrantsList, onCreate: h.onGrantCreate}
}

type hookGrants struct {
	storage.ShareGrantStore
	hook     func() error
	onCreate func()
}

func (g *hookGrants) Create(ctx context.Context, gr *storage.ShareGrant) error {
	if err := g.ShareGrantStore.Create(ctx, gr); err != nil {
		return err
	}
	if g.onCreate != nil {
		g.onCreate()
	}
	return nil
}

func (g *hookGrants) List(ctx context.Context, f storage.GrantFilter) ([]*storage.ShareGrant, error) {
	out, err := g.ShareGrantStore.List(ctx, f)
	if err != nil {
		return nil, err
	}
	if g.hook != nil {
		if err := g.hook(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
