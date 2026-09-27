package stack

import (
	"net/http"
	"net/http/httptest"
	"testing"

	kitbus "hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// A private instance always checks Host, allowing only the loopback
// names on the port actually bound, plus server.allowed_hosts.
func TestHostAllowlistPrivateDefault(t *testing.T) {
	hosts, err := HostAllowlist(config.AccessPrivate, 18947, nil)
	if err != nil {
		t.Fatalf("hostAllowlist: %v", err)
	}
	if hosts == nil {
		t.Fatal("private instance must check Host")
	}
	for _, h := range []string{"127.0.0.1:18947", "localhost:18947"} {
		if !hosts.Allows(h, false) {
			t.Errorf("%s must be allowed", h)
		}
	}
	for _, h := range []string{"evil.example:18947", "127.0.0.1:8080", "localhost:8080", "dpkms.lan:18947"} {
		if hosts.Allows(h, false) {
			t.Errorf("%s must be rejected", h)
		}
	}
}

func TestHostAllowlistPrivateExtra(t *testing.T) {
	hosts, err := HostAllowlist(config.AccessPrivate, 18947, []string{"dpkms.lan"})
	if err != nil {
		t.Fatalf("hostAllowlist: %v", err)
	}
	for _, h := range []string{"127.0.0.1:18947", "dpkms.lan:18947"} {
		if !hosts.Allows(h, false) {
			t.Errorf("%s must be allowed", h)
		}
	}
}

// Protected and public instances are reached by names dpkms cannot
// know, so they check Host only once server.allowed_hosts is set.
func TestHostAllowlistNonPrivate(t *testing.T) {
	for _, access := range []string{config.AccessProtected, config.AccessPublic} {
		hosts, err := HostAllowlist(access, 18947, nil)
		if err != nil {
			t.Fatalf("%s: hostAllowlist: %v", access, err)
		}
		if hosts != nil {
			t.Errorf("%s without server.allowed_hosts must not check Host", access)
		}

		hosts, err = HostAllowlist(access, 18947, []string{"dpkms.example.net"})
		if err != nil {
			t.Fatalf("%s: hostAllowlist: %v", access, err)
		}
		if hosts == nil {
			t.Fatalf("%s with server.allowed_hosts must check Host", access)
		}
		for _, h := range []string{"dpkms.example.net", "127.0.0.1:18947", "localhost:18947"} {
			if !hosts.Allows(h, false) {
				t.Errorf("%s: %s must be allowed", access, h)
			}
		}
		if hosts.Allows("192.168.1.20:18947", false) {
			t.Errorf("%s: unlisted host must be rejected", access)
		}
	}
}

func TestHostAllowlistBadEntry(t *testing.T) {
	if _, err := HostAllowlist(config.AccessPrivate, 18947, []string{"http://x"}); err == nil {
		t.Fatal("malformed entry must refuse serve")
	}
}

// Build checks Host on the router it hands serve once it knows the bound
// HTTP port: a private stack answers its loopback names on that port
// only, every route included; without a port there is no listener to
// name and no check.
func TestBuildChecksHostForBoundPort(t *testing.T) {
	get := func(r http.Handler, host string) int {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	newStack := func(port int) *Stack {
		st, err := Build(Inputs{
			Config:    stubProviders(&config.Config{}),
			Driver:    storageutil.NewTestDriver(t),
			PolicyBus: kitbus.New(),
			HTTPPort:  port,
		})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		t.Cleanup(st.Close)
		return st
	}

	st := newStack(18947)
	if got := get(st.Router, "127.0.0.1:18947"); got != http.StatusOK {
		t.Errorf("bound loopback name: status %d, want 200", got)
	}
	for _, h := range []string{"evil.example:18947", "127.0.0.1:8080"} {
		if got := get(st.Router, h); got != http.StatusForbidden {
			t.Errorf("Host %s: status %d, want 403", h, got)
		}
	}

	if got := get(newStack(0).Router, "evil.example"); got != http.StatusOK {
		t.Errorf("no bound port: status %d, want 200 (no Host check)", got)
	}
}
