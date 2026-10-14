package stack

import (
	"context"
	"testing"

	kitbus "hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// TestShutdownDrainsSecurityAuditRows: the audit rows of auth failures
// seen before Shutdown are in the store when it returns, and failures
// after it write nothing, so the caller can close the store next.
func TestShutdownDrainsSecurityAuditRows(t *testing.T) {
	cfg := stubProviders(&config.Config{})
	cfg.Server.Access = config.AccessProtected
	cfg.Server.Auth = staticAuthCfg()
	access, provider, err := ResolveInboundAuth(cfg, false)
	if err != nil {
		t.Fatalf("ResolveInboundAuth: %v", err)
	}
	driver := storageutil.NewTestDriver(t)
	bus := kitbus.New()
	t.Cleanup(func() { _ = bus.Close(context.Background()) })
	st, err := Build(Inputs{Config: cfg, Driver: driver, Access: access, Auth: provider, PolicyBus: bus})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(st.Close)

	// The default threshold alerts on the third failure.
	for i := 0; i < 3; i++ {
		if code := status(t, st, "wrong-token"); code != 401 {
			t.Fatalf("status = %d, want 401", code)
		}
	}
	if err := st.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	rows := func() int {
		t.Helper()
		_, n, err := driver.AuditLog().List(context.Background(), storage.AuditFilter{EventType: security.EventAuthFailure})
		if err != nil {
			t.Fatalf("list audit: %v", err)
		}
		return n
	}
	if n := rows(); n != 1 {
		t.Fatalf("security audit rows after Shutdown = %d, want 1", n)
	}

	status(t, st, "wrong-token")
	st.Security.RecordAuthFailure(context.Background(), "after-shutdown")
	if n := rows(); n != 1 {
		t.Fatalf("security audit rows after a post-Shutdown failure = %d, want 1", n)
	}
	if err := st.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}
