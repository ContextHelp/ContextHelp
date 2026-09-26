package cmd

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
	"github.com/spf13/viper"
)

// TestLiveServerGuardCoversClientCommands: the dpkms client commands dial
// through the guarded transport, so one that falls back to its
// http://localhost:8080 default is refused. Proven on a blocked sentinel
// listener, never on the real default port.
func TestLiveServerGuardCoversClientCommands(t *testing.T) {
	var conns atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.Config.ConnState = func(_ net.Conn, st http.ConnState) {
		if st == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()
	addr := srv.Listener.Addr().String()
	defer testguard.Active.Block(addr)()

	prev := viper.GetString("server.url")
	viper.Set("server.url", srv.URL)
	defer viper.Set("server.url", prev)

	var buf bytes.Buffer
	healthcheckCmd.SetOut(&buf)
	healthcheckCmd.SetErr(&buf)
	_ = runHealthcheck(healthcheckCmd, nil)

	trips := testguard.Active.Take(addr)
	if n := conns.Load(); n != 0 {
		t.Fatalf("sentinel accepted %d connection(s); dpkms client requests bypass the guard", n)
	}
	if trips == 0 {
		t.Error("healthcheck never dialed the configured server; the test proves nothing")
	}
}
