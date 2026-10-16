package sharechange_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}

// driverFunc opens a fresh store for one fixture: SQLite here, Postgres
// in the integration-tagged runs of the same tests (pg_test.go).
type driverFunc func(*testing.T) storage.StorageDriver

var sqliteDriver driverFunc = storageutil.NewTestDriver

// clock is a settable clock on a millisecond boundary.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// at returns the instant d from now.
func (c *clock) at(d time.Duration) *time.Time {
	t := c.now().Add(d)
	return &t
}

// fx is the sharing registry with the approval mechanism and the
// share.change kind over a fresh store, and its cast:
//
//   - owner: the instance owner;
//   - admin: holds the admin role (admin:shares), never approve:shares;
//   - zed: an admin that also holds share-approver;
//   - alice, bob: share approvers;
//   - dana: a share approver the changes of the cast also benefit;
//   - erin: a reader with no scope to approve anything.
//
// The audience "team", the policies base and other, and dana's API
// key are the changes' targets.
type fx struct {
	t     *testing.T
	drv   storage.StorageDriver
	svc   *sharechange.Service
	appr  *approval.Service
	clock *clock

	owner, admin, admin2, zed, alice, bob, dana, erin *auth.Principal

	team   *storage.Audience
	base   *storage.SharePolicy
	other  *storage.SharePolicy
	erinID string
	key    *storage.Identity
	// granted is the grant fx.grant created last.
	granted *storage.ShareGrant
}

type fxOption func(*fxConfig)

type fxConfig struct {
	required func() int
	drv      func(storage.StorageDriver) storage.SharingStore
	noAppr   bool
}

// withRequired sets approvals.share.required.
func withRequired(n int) fxOption { return func(c *fxConfig) { c.required = func() int { return n } } }

// withSharing wraps the store the service classifies and writes with.
func withSharing(wrap func(storage.StorageDriver) storage.SharingStore) fxOption {
	return func(c *fxConfig) { c.drv = wrap }
}

// withoutApprovals builds the service with no approval mechanism.
func withoutApprovals() fxOption { return func(c *fxConfig) { c.noAppr = true } }

const pendingTTL = 72 * time.Hour

func newFx(t *testing.T, newDriver driverFunc, opts ...fxOption) *fx {
	t.Helper()
	var cfg fxConfig
	for _, o := range opts {
		o(&cfg)
	}
	f := &fx{t: t, drv: newDriver(t), clock: &clock{t: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}}
	f.owner = auth.LocalPrincipal()
	approver := []string{auth.RoleReader, auth.RoleShareApprover}
	f.admin = f.person("prn_admin", auth.RoleAdmin)
	f.admin2 = f.person("prn_admin2", auth.RoleAdmin)
	f.zed = f.person("prn_zed", auth.RoleAdmin, auth.RoleShareApprover)
	f.alice = f.person("prn_alice", approver...)
	f.bob = f.person("prn_bob", approver...)
	f.dana = f.person("prn_dana", approver...)
	f.erin = f.person("prn_erin", auth.RoleReader)
	f.erinID = f.erin.ID

	sharingStore := f.drv.Sharing()
	if cfg.drv != nil {
		sharingStore = cfg.drv(f.drv)
	}
	deps := sharechange.Deps{
		Sharing: sharingStore, Auth: f.drv.Auth(), Audit: f.drv.AuditLog(), Now: f.clock.now, Required: cfg.required,
	}
	kind, err := sharechange.NewKind(deps)
	require.NoError(t, err)
	reg := approval.NewRegistry()
	require.NoError(t, reg.Register(kind))
	f.appr, err = approval.NewService(f.drv.Approvals(), f.drv.AuditLog(), reg, approval.Options{PendingTTL: pendingTTL, Now: f.clock.now})
	require.NoError(t, err)
	f.svc = &sharechange.Service{Deps: deps, Approvals: f.appr}
	if cfg.noAppr {
		f.svc.Approvals = nil
	}

	ctx := context.Background()
	st := f.drv.Sharing()
	f.team = &storage.Audience{Name: "team", CreatedAt: f.clock.now()}
	require.NoError(t, st.Audiences().Create(ctx, f.team))
	f.base = f.policy("base", "type==contact", map[string][]string{"contact": {"title", "org"}})
	f.other = f.policy("other", "type==note", map[string][]string{"note": {"title"}})
	f.key = &storage.Identity{PrincipalID: f.dana.ID, Method: storage.IdentityMethodAPIKey, Subject: "dana-key", CreatedAt: f.clock.now()}
	require.NoError(t, f.drv.Auth().Identities().Create(ctx, f.key))
	return f
}

// person creates a person principal with roles and returns it as the
// auth chain would present it.
func (f *fx) person(id string, roles ...string) *auth.Principal {
	f.t.Helper()
	require.NoError(f.t, f.drv.Auth().Principals().Create(context.Background(), &storage.Principal{
		ID: id, Kind: storage.PrincipalKindPerson, DisplayName: id, Roles: roles,
	}))
	return &auth.Principal{ID: id, Kind: storage.PrincipalKindPerson, Roles: roles, Scopes: auth.ScopesForRoles(roles)}
}

// policy stores a policy row directly (the owner's, outside any route).
func (f *fx) policy(name, data string, fields map[string][]string) *storage.SharePolicy {
	f.t.Helper()
	spec, err := sharing.Policy{Name: name, Data: data, Fields: fields}.Spec()
	require.NoError(f.t, err)
	row := &storage.SharePolicy{Name: name, Spec: spec, UpdatedAt: f.clock.now()}
	require.NoError(f.t, f.drv.Sharing().Policies().Create(context.Background(), row))
	return row
}

// widerBase is base with one more field: a widening edit.
func widerBase() sharing.Policy {
	return sharing.Policy{Name: "base", Data: "type==contact", Fields: map[string][]string{"contact": {"title", "org", "email"}}}
}

// narrowerBase is base without a field: a narrowing edit.
func narrowerBase() sharing.Policy {
	return sharing.Policy{Name: "base", Data: "type==contact", Fields: map[string][]string{"contact": {"title"}}}
}

// join puts principal in team through the store (the owner's direct
// write), until expires.
func (f *fx) join(principal string, expires *time.Time) {
	f.t.Helper()
	m := &storage.AudienceMember{AudienceID: f.team.ID, PrincipalID: principal, ExpiresAt: expires, AddedBy: storage.OwnerPrincipal, AddedAt: f.clock.now()}
	require.NoError(f.t, f.drv.Sharing().Audiences().AddMember(context.Background(), m))
}

// grant grants team the policy through the store and returns the grant.
func (f *fx) grant(p *storage.SharePolicy) *storage.ShareGrant {
	f.t.Helper()
	g := &storage.ShareGrant{AudienceID: f.team.ID, PolicyID: p.ID, CreatedBy: storage.OwnerPrincipal, CreatedAt: f.clock.now()}
	require.NoError(f.t, f.drv.Sharing().Grants().Create(context.Background(), g))
	f.granted = g
	return g
}

// submit runs c as p and requires no error.
func (f *fx) submit(p *auth.Principal, c sharing.Change) *sharechange.Outcome {
	f.t.Helper()
	out, err := f.svc.Submit(context.Background(), p, c)
	require.NoError(f.t, err)
	return out
}

// pending submits c as the admin and requires it to be filed.
func (f *fx) pending(p *auth.Principal, c sharing.Change) *storage.ApprovalRequest {
	f.t.Helper()
	out := f.submit(p, c)
	require.Nil(f.t, out.Applied, "%s was applied: %+v", c.Action(), out.Class)
	require.NotNil(f.t, out.Request, "%s was not filed: %+v", c.Action(), out.Class)
	require.Equal(f.t, sharing.Widening, out.Class.Class)
	return out.Request
}

func (f *fx) approve(r *storage.ApprovalRequest, p *auth.Principal) (*approval.Decided, error) {
	return f.appr.Approve(context.Background(), r.ID, p, r.PayloadDigest, "ok by "+p.ID)
}

func (f *fx) reject(r *storage.ApprovalRequest, p *auth.Principal) (*approval.Decided, error) {
	return f.appr.Reject(context.Background(), r.ID, p, r.PayloadDigest, "no by "+p.ID)
}

// stored returns request id as the store holds it.
func (f *fx) stored(id string) *storage.ApprovalRequest {
	f.t.Helper()
	r, err := f.drv.Approvals().Get(context.Background(), id)
	require.NoError(f.t, err)
	return r
}

// audit lists every audit entry, oldest first.
func (f *fx) audit() []*storage.AuditEntry {
	f.t.Helper()
	all, _, err := f.drv.AuditLog().List(context.Background(), storage.AuditFilter{Limit: 10000})
	require.NoError(f.t, err)
	return all
}

// shareEvents lists the audit entries of the sharing changes themselves
// (share.*): the record that something was changed.
func (f *fx) shareEvents() []*storage.AuditEntry {
	var out []*storage.AuditEntry
	for _, e := range f.audit() {
		if strings.HasPrefix(e.EventType, "share.") {
			out = append(out, e)
		}
	}
	return out
}

// eventsOf lists the audit entries of event type kind.
func (f *fx) eventsOf(kind string) []*storage.AuditEntry {
	var out []*storage.AuditEntry
	for _, e := range f.audit() {
		if e.EventType == kind {
			out = append(out, e)
		}
	}
	return out
}

// snapshot renders every row of the sharing registry the cast can
// change, so a test compares what a change left untouched.
func (f *fx) snapshot() string {
	f.t.Helper()
	ctx := context.Background()
	st := f.drv.Sharing()
	var b strings.Builder
	auds, err := st.Audiences().List(ctx)
	require.NoError(f.t, err)
	for _, a := range auds {
		ms, err := st.Audiences().ListMembers(ctx, a.ID)
		require.NoError(f.t, err)
		sort.Slice(ms, func(i, j int) bool { return ms[i].PrincipalID < ms[j].PrincipalID })
		fmt.Fprintf(&b, "audience %s %s\n", a.ID, a.Name)
		for _, m := range ms {
			exp := "-"
			if m.ExpiresAt != nil {
				exp = m.ExpiresAt.UTC().Format(time.RFC3339Nano)
			}
			fmt.Fprintf(&b, "  member %s until %s by %s\n", m.PrincipalID, exp, m.AddedBy)
		}
	}
	pols, err := st.Policies().List(ctx)
	require.NoError(f.t, err)
	for _, p := range pols {
		fmt.Fprintf(&b, "policy %s %s v%d %s\n", p.ID, p.Name, p.Version, p.Spec)
	}
	grants, err := st.Grants().List(ctx, storage.GrantFilter{IncludeRevoked: true})
	require.NoError(f.t, err)
	for _, g := range grants {
		fmt.Fprintf(&b, "grant %s %s->%s revoked=%v\n", g.ID, g.AudienceID, g.PolicyID, g.RevokedAt != nil)
	}
	for _, id := range []string{f.erin.ID, f.alice.ID, f.dana.ID, f.admin.ID} {
		classes, err := st.DenyClasses(ctx, id)
		require.NoError(f.t, err)
		fmt.Fprintf(&b, "ceiling %s %v\n", id, classes)
	}
	bound, err := st.IdentityPolicy(ctx, f.key.ID)
	require.NoError(f.t, err)
	fmt.Fprintf(&b, "key %s -> %q\n", f.key.ID, bound)
	return b.String()
}

// requireCode asserts err is a mechanism refusal with code.
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, code, approval.CodeOf(err), "err: %v", err)
}
