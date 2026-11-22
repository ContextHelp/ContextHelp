package steward_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}

// newDriver opens the store the suite runs on: SQLite, or a fresh
// Postgres database under the integration tag (pg_test.go).
var newDriver = storageutil.NewTestDriver

// ttl is the fixture's approvals.pending_ttl.
const ttl = 72 * time.Hour

// reason is a reason long enough for the fixture's minimum.
const reason = "Legal hold for matter 2026-117: opposing counsel asked for the supplier contacts"

// familyScope selects the family-tagged objects and renders titles.
const familyScope = `{"data":"tag==family","fields":{"contact":["title"]},"capabilities":{"search":["fts"]}}`

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

// fixture is the mechanism over a fresh SQLite store with the
// steward.grant kind registered, a resolver wired the way the server
// wires it, and the cast: the owner, a requester, a steward, two
// approvers (steward-approver), a principal with no steward role and
// an outsider.
type fixture struct {
	t        *testing.T
	drv      storage.StorageDriver
	svc      *approval.Service
	grants   *steward.Service
	viewers  *sharing.Resolver
	clock    *clock
	settings *sharing.StewardSettings
	required int
	sharing  storage.SharingStore

	owner, requester, stewardP, alice, bob, plain, outsider *auth.Principal
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		t: t, drv: newDriver(t), clock: &clock{t: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}, required: 1,
		settings: &sharing.StewardSettings{Enabled: true, MaxDuration: 14 * 24 * time.Hour, WithheldClasses: []string{"personal"}},
	}
	f.sharing = f.drv.Sharing()
	f.owner = &auth.Principal{ID: storage.OwnerPrincipal, Kind: storage.PrincipalKindOwner, Roles: auth.OwnerRoles, Scopes: auth.ScopesForRoles(auth.OwnerRoles)}
	requester := []string{auth.RoleReader, auth.RoleStewardRequester}
	approver := []string{auth.RoleReader, auth.RoleStewardApprover}
	f.requester = f.person("prn_requester", requester...)
	f.stewardP = f.person("prn_steward", requester...)
	f.alice = f.person("prn_alice", approver...)
	f.bob = f.person("prn_bob", approver...)
	f.plain = f.person("prn_plain", auth.RoleReader)
	f.outsider = f.person("prn_outsider", auth.RoleReader)

	reg := approval.NewRegistry()
	kind, err := steward.Kind(f.kindOptions(f.sharing))
	require.NoError(t, err)
	require.NoError(t, reg.Register(kind))
	f.svc, err = approval.NewService(f.drv.Approvals(), f.drv.AuditLog(), reg, approval.Options{PendingTTL: ttl, Now: f.clock.now})
	require.NoError(t, err)
	f.grants, err = steward.NewService(steward.ServiceOptions{
		Sharing: f.sharing, Approvals: f.drv.Approvals(), Audit: f.drv.AuditLog(),
		Settings: func() sharing.StewardSettings { return *f.settings }, Now: f.clock.now,
	})
	require.NoError(t, err)
	f.viewers = sharing.NewResolver(f.sharing).WithRegistry(f.drv.Auth()).WithClock(f.clock.now).
		WithSteward(sharingtest.StewardOptions(func() sharing.StewardSettings { return *f.settings }))
	return f
}

// kindOptions are the options the server gives the kind, over store.
func (f *fixture) kindOptions(store storage.SharingStore) steward.KindOptions {
	return steward.KindOptions{
		Settings:       func() sharing.StewardSettings { return *f.settings },
		Required:       func() int { return f.required },
		MinReasonChars: func() int { return 30 },
		Sharing:        store,
		Auth:           f.drv.Auth(),
		Audit:          f.drv.AuditLog(),
		Now:            f.clock.now,
	}
}

// person creates a person with roles and returns it as the auth chain
// would: its scopes the roles' bundles.
func (f *fixture) person(id string, roles ...string) *auth.Principal {
	f.t.Helper()
	sharingtest.EnsurePrincipal(f.t, f.drv, id, storage.PrincipalKindPerson, roles...)
	return &auth.Principal{ID: id, Kind: storage.PrincipalKindPerson, Roles: roles, Scopes: auth.ScopesForRoles(roles)}
}

// window is [now, now+d) as filing carries it.
func (f *fixture) window(d time.Duration) (time.Time, time.Time) {
	now := f.clock.now()
	return now, now.Add(d)
}

// request is a valid request for the steward over familyScope and a
// two-day window starting now.
func (f *fixture) request() steward.Request {
	nb, exp := f.window(48 * time.Hour)
	return steward.Request{StewardID: f.stewardP.ID, MatterRef: "CASE-117", Scope: json.RawMessage(familyScope), NotBefore: nb, ExpiresAt: exp}
}

// filing wraps req as the requester files it.
func (f *fixture) filing(by *auth.Principal, req steward.Request) approval.Filing {
	f.t.Helper()
	raw, err := json.Marshal(req)
	require.NoError(f.t, err)
	return approval.Filing{
		Kind: steward.KindName, Action: steward.ActionRequest, Requester: by, SubjectID: req.StewardID,
		Reason: reason, Payload: raw,
	}
}

// file files req as the requester.
func (f *fixture) file(req steward.Request) (*storage.ApprovalRequest, error) {
	return f.svc.File(context.Background(), f.filing(f.requester, req))
}

// mustFile is file that must succeed.
func (f *fixture) mustFile(req steward.Request) *storage.ApprovalRequest {
	f.t.Helper()
	r, err := f.file(req)
	require.NoError(f.t, err)
	return r
}

func (f *fixture) approve(r *storage.ApprovalRequest, p *auth.Principal) (*approval.Decided, error) {
	return f.svc.Approve(context.Background(), r.ID, p, r.PayloadDigest, "ok by "+p.ID)
}

// approved files req and approves it with enough approvers, returning
// the grant it wrote.
func (f *fixture) approved(req steward.Request) *storage.StewardGrant {
	f.t.Helper()
	r := f.mustFile(req)
	for _, p := range []*auth.Principal{f.alice, f.bob}[:f.required] {
		_, err := f.approve(r, p)
		require.NoError(f.t, err)
	}
	g, err := f.sharing.StewardGrants().GetByRequest(context.Background(), r.ID)
	require.NoError(f.t, err)
	return g
}

// stored returns request id as the store holds it.
func (f *fixture) stored(id string) *storage.ApprovalRequest {
	f.t.Helper()
	r, err := f.drv.Approvals().Get(context.Background(), id)
	require.NoError(f.t, err)
	return r
}

// audit returns the audit entries of one event type.
func (f *fixture) audit(event string) []*storage.AuditEntry {
	f.t.Helper()
	all, _, err := f.drv.AuditLog().List(context.Background(), storage.AuditFilter{EventType: event, Limit: 100})
	require.NoError(f.t, err)
	return all
}

// exec runs a statement written with ? placeholders on the store's
// database, numbered for Postgres.
func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	if search.DialectFor(f.drv) == search.DialectPostgres {
		n := 0
		query = placeholder.ReplaceAllStringFunc(query, func(string) string { n++; return fmt.Sprintf("$%d", n) })
	}
	db := f.drv.(interface{ DB() *sql.DB }).DB()
	res, err := db.ExecContext(context.Background(), query, args...)
	require.NoError(f.t, err)
	n, _ := res.RowsAffected()
	require.EqualValues(f.t, 1, n, "one row")
}

var placeholder = regexp.MustCompile(`\?`)

// timeValue is t as the store's time columns take it in a raw
// statement: Unix milliseconds on SQLite, a timestamp on Postgres.
func (f *fixture) timeValue(t time.Time) any {
	if search.DialectFor(f.drv) == search.DialectPostgres {
		return t.UTC()
	}
	return t.UnixMilli()
}

// setRoles rewrites a principal's stored roles, as removing an IdP
// group does.
func (f *fixture) setRoles(id string, roles ...string) {
	f.t.Helper()
	raw, err := storage.RolesJSON(roles)
	require.NoError(f.t, err)
	f.exec(`UPDATE principals SET roles = ? WHERE id = ?`, raw, id)
}

// requireCode asserts err is a mechanism refusal with code.
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, code, approval.CodeOf(err), "err: %v", err)
}
