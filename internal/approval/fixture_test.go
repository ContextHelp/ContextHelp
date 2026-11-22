package approval_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/approval/approvaltest"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}

// ttl is the fixture's approvals.pending_ttl.
const ttl = 72 * time.Hour

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

// fixture is the mechanism over a fresh SQLite store with the test kind
// registered, and its cast: the owner, a requester, three approvers
// (holding the kind's approver scope), the target the test changes
// benefit (who holds the scope too) and an outsider (who holds none).
type fixture struct {
	t     *testing.T
	drv   storage.StorageDriver
	svc   *approval.Service
	kind  *approvaltest.Kind
	clock *clock
	// notices records what the service reported after each commit.
	notices *noticeLog

	owner, requester, alice, bob, carol, target, outsider *auth.Principal
}

func newFixture(t *testing.T, opts ...func(*approvaltest.Kind)) *fixture {
	t.Helper()
	f := &fixture{t: t, drv: storageutil.NewTestDriver(t), clock: &clock{t: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}, notices: &noticeLog{}}
	f.kind = approvaltest.New("test.change", f.drv)
	for _, o := range opts {
		o(f.kind)
	}
	reg := approval.NewRegistry()
	require.NoError(t, reg.Register(f.kind.Definition()))
	svc, err := approval.NewService(f.drv.Approvals(), f.drv.AuditLog(), reg, approval.Options{PendingTTL: ttl, Now: f.clock.now, Notify: f.notices.record})
	require.NoError(t, err)
	f.svc = svc
	f.owner = &auth.Principal{ID: storage.OwnerPrincipal, Kind: storage.PrincipalKindOwner, Roles: auth.OwnerRoles, Scopes: auth.ScopesForRoles(auth.OwnerRoles)}
	approver := []string{auth.RoleReader, auth.RoleShareApprover}
	f.requester = f.principal("prn_requester", auth.RoleAdmin)
	f.alice = f.principal("prn_alice", approver...)
	f.bob = f.principal("prn_bob", approver...)
	f.carol = f.principal("prn_carol", approver...)
	f.target = f.principal("prn_target", approver...)
	f.outsider = f.principal("prn_outsider", auth.RoleReader)
	return f
}

// principal creates a person with roles and returns it as the auth
// chain would: its scopes the roles' bundles.
func (f *fixture) principal(id string, roles ...string) *auth.Principal {
	f.t.Helper()
	require.NoError(f.t, f.drv.Auth().Principals().Create(context.Background(), &storage.Principal{
		ID: id, Kind: storage.PrincipalKindPerson, DisplayName: id, Roles: roles,
	}))
	return &auth.Principal{ID: id, Kind: storage.PrincipalKindPerson, Roles: roles, Scopes: auth.ScopesForRoles(roles)}
}

// filing is a valid filing by the requester that benefits the target.
func (f *fixture) filing(value string) approval.Filing {
	return approval.Filing{
		Kind: f.kind.Name(), Action: approvaltest.Action, Requester: f.requester, SubjectID: f.target.ID,
		Reason: "conformance of the mechanism", Payload: approvaltest.Raw(f.target.ID, value),
	}
}

// file files f.filing(value).
func (f *fixture) file(value string) *storage.ApprovalRequest {
	f.t.Helper()
	r, err := f.svc.File(context.Background(), f.filing(value))
	require.NoError(f.t, err)
	return r
}

func (f *fixture) approve(r *storage.ApprovalRequest, p *auth.Principal) (*approval.Decided, error) {
	return f.svc.Approve(context.Background(), r.ID, p, r.PayloadDigest, "ok by "+p.ID)
}

func (f *fixture) reject(r *storage.ApprovalRequest, p *auth.Principal) (*approval.Decided, error) {
	return f.svc.Reject(context.Background(), r.ID, p, r.PayloadDigest, "no by "+p.ID)
}

// stored returns request id as the store holds it.
func (f *fixture) stored(id string) *storage.ApprovalRequest {
	f.t.Helper()
	r, err := f.drv.Approvals().Get(context.Background(), id)
	require.NoError(f.t, err)
	return r
}

// events returns the audit entries of the mechanism and the test kind
// about request id, oldest first.
func (f *fixture) events(id string) []*storage.AuditEntry {
	f.t.Helper()
	all, _, err := f.drv.AuditLog().List(context.Background(), storage.AuditFilter{Limit: 1000})
	require.NoError(f.t, err)
	var out []*storage.AuditEntry
	for _, e := range all {
		if (strings.HasPrefix(e.EventType, "approval.") || e.EventType == approvaltest.EventApplied) && e.Payload["request_id"] == id {
			out = append(out, e)
		}
	}
	return out
}

func eventTypes(entries []*storage.AuditEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.EventType
	}
	return out
}

// requireCode asserts err is a mechanism refusal with code.
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, code, approval.CodeOf(err), "err: %v", err)
}
