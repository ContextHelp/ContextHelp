package leaktest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Administering an instance is not access to its data (ADR-082 §10, ADR-083 §10):
// a principal holding the admin bundle sees what its own grants show,
// and only the local owner bypasses. The fixture holds three such
// administrators, none in any audience nor under any grant, beside
// the viewers whose rows and the owner-only rows they must never see:
// a person and a device, each presenting a bearer key, and a person
// signed in to the web UI.
const (
	// AdminKeyID is the person administrator that presents a bearer key.
	AdminKeyID = "prn_leak_admin_key"
	// AdminDeviceID is the device administrator that presents a bearer key.
	AdminDeviceID = "prn_leak_admin_device"
	// AdminSessionID is the administrator that browses the web UI.
	AdminSessionID = "prn_leak_admin_session"
)

// Fixture principal names of the administrators.
const (
	// NameAdminKey is a person holding the admin role and one bearer
	// key with the whole bundle: admin:principals, admin:shares and
	// admin:audit included.
	NameAdminKey = "admin-key"
	// NameAdminDevice is NameAdminKey as a device: the same role and
	// bundle on a principal of kind device. A viewer is resolved from
	// grants, never from the kind, so both are shown the same.
	NameAdminDevice = "admin-device"
	// NameAdminSession is a person holding the admin role, signed in to
	// the web UI: its session carries the UI scope set intersected with
	// the bundle (so admin:shares but neither admin:principals nor
	// admin:audit), over HTTP only (gRPC carries no cookies).
	NameAdminSession = "admin-session"
)

// seedAdmins creates the three administrators.
func (f *Fixture) seedAdmins(t testing.TB) {
	t.Helper()
	ctx := context.Background()
	driver := f.Server.Driver
	keys := authn.NewKeysOver(driver.Auth(), nil)
	compile := func(name, id, identity string) sharing.Viewer {
		v, err := sharing.CompileFor(ctx, driver.Sharing(), id, identity, time.Now())
		if err != nil {
			t.Fatalf("leaktest: compile %s viewer: %v", name, err)
		}
		return v
	}

	for _, a := range []struct{ name, id, kind string }{
		{NameAdminKey, AdminKeyID, storage.PrincipalKindPerson},
		{NameAdminDevice, AdminDeviceID, storage.PrincipalKindDevice},
	} {
		sharingtest.EnsurePrincipal(t, driver, a.id, a.kind, authn.RoleAdmin)
		secret, _, err := keys.Mint(ctx, authn.MintKey{PrincipalID: a.id, Name: "leaktest " + a.name})
		if err != nil {
			t.Fatalf("leaktest: mint %s key: %v", a.name, err)
		}
		f.admins = append(f.admins, Principal{
			Name: a.name, ID: a.id, Token: secret, viewer: compile(a.name, a.id, ""),
		})
	}

	sharingtest.EnsurePrincipal(t, driver, AdminSessionID, storage.PrincipalKindPerson, authn.RoleAdmin)
	idn := &storage.Identity{PrincipalID: AdminSessionID, Method: storage.IdentityMethodLink, Subject: "leaktest-admin-session", Name: "leaktest"}
	if err := driver.Auth().Identities().Create(ctx, idn); err != nil {
		t.Fatalf("leaktest: bind %s identity: %v", NameAdminSession, err)
	}
	sessions := f.Server.Stack.UISessions
	if sessions == nil {
		t.Fatalf("leaktest: the instance offers no browser sign-in")
	}
	minted, err := sessions.MintFor(ctx, AdminSessionID, idn.ID, authn.ClientInfo{UserAgent: "leaktest"})
	if err != nil {
		t.Fatalf("leaktest: mint %s session: %v", NameAdminSession, err)
	}
	host := strings.TrimPrefix(f.Server.URL, "http://")
	// A request Cookie header carries name=value only; attributes belong
	// to Set-Cookie.
	cookie := httpserver.SessionCookieName(host) + "=" + minted.Secret
	f.admins = append(f.admins, Principal{
		Name: NameAdminSession, ID: AdminSessionID, HTTPOnly: true,
		// The browser's own headers: a write from the UI's page is
		// same-origin and carries the CSRF header.
		Header: http.Header{
			"Cookie":              {cookie},
			"Sec-Fetch-Site":      {"same-origin"},
			httpserver.HeaderCSRF: {"1"},
		},
		viewer: compile(NameAdminSession, AdminSessionID, idn.ID),
	})
}

// Admins lists the fixture's administrators: holders of the admin
// bundle under no grant.
func (f *Fixture) Admins() []Principal { return append([]Principal(nil), f.admins...) }

// Admin returns the administrator named name (NameAdminKey,
// NameAdminDevice or NameAdminSession).
func (f *Fixture) Admin(name string) Principal {
	for _, p := range f.admins {
		if p.Name == name {
			return p
		}
	}
	return Principal{}
}

// Reaches reports whether p can call c at all: a browser session
// speaks HTTP only.
func (p Principal) Reaches(c Case) bool {
	return !p.HTTPOnly || !strings.HasPrefix(c.Route, "/")
}
