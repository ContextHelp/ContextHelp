package dpkmstest_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

func upgradeHeader(t *testing.T, srv *dpkmstest.Server, role string) (int, string, bool) {
	t.Helper()
	resp := srv.Request(t, http.MethodGet, "/api/v1/objects/obj_missing", role, nil)
	defer resp.Body.Close()
	vs, ok := resp.Header[upgrade.HeaderName]
	if !ok {
		return resp.StatusCode, "", false
	}
	return resp.StatusCode, vs[0], true
}

// WithUpgrade puts the fixture mid-upgrade: its API responses carry the
// upgrade header while the manager is in progress or failed, and drop it
// once the run completes.
func TestWithUpgradeSetsHeader(t *testing.T) {
	m := upgrade.NewManager("")
	srv := dpkmstest.Start(t, storageutil.NewTestDriver(t), dpkmstest.WithStaticTokens(), dpkmstest.WithUpgrade(m))

	if _, v, ok := upgradeHeader(t, srv, dpkmstest.RoleReader); ok {
		t.Fatalf("idle instance sent %s: %q", upgrade.HeaderName, v)
	}

	if err := m.Start(upgrade.BucketReindexAuto, 4); err != nil {
		t.Fatal(err)
	}
	code, v, ok := upgradeHeader(t, srv, dpkmstest.RoleReader)
	if !ok || code != http.StatusNotFound {
		t.Fatalf("in-progress instance: status %d, header %q (set %v)", code, v, ok)
	}
	if st, err := upgrade.DecodeHeader(v); err != nil || st.State != upgrade.StateInProgress || st.Total != 4 {
		t.Fatalf("header %q decodes to %+v, %v", v, st, err)
	}
	if _, v, ok := upgradeHeader(t, srv, ""); ok {
		t.Fatalf("unauthenticated caller got %s: %q", upgrade.HeaderName, v)
	}

	if err := m.Fail(errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	if _, v, _ := upgradeHeader(t, srv, dpkmstest.RoleReader); v == "" {
		t.Fatal("failed run must keep the header")
	}

	if err := m.Start(upgrade.BucketReindexAuto, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.Complete(); err != nil {
		t.Fatal(err)
	}
	if _, v, ok := upgradeHeader(t, srv, dpkmstest.RoleReader); ok {
		t.Fatalf("completed run still sends %s: %q", upgrade.HeaderName, v)
	}
}

// Without WithUpgrade the fixture never sends the header.
func TestNoUpgradeNoHeader(t *testing.T) {
	srv := dpkmstest.Start(t, storageutil.NewTestDriver(t))
	if _, v, ok := upgradeHeader(t, srv, ""); ok {
		t.Fatalf("fixture sent %s: %q", upgrade.HeaderName, v)
	}
}
