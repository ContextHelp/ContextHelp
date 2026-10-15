package providers

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"hop.top/kit/go/storage/secret/memory"
)

// connStates tracks each server-side connection's latest state.
type connStates struct {
	mu     sync.Mutex
	states map[net.Conn]http.ConnState
}

func (c *connStates) track(conn net.Conn, s http.ConnState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.states[conn] = s
}

func (c *connStates) active() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, s := range c.states {
		if s == http.StateActive {
			n++
		}
	}
	return n
}

// An Ollama endpoint that answers the auto-mode probe with an error is
// skipped, and the probe's response body is closed. The endpoint here
// streams its error body until the client hangs up, so the connection
// stays active exactly as long as the body is left open (at most the
// probe client's 2s timeout).
func TestTryOllamaVision_ClosesRejectedProbeBody(t *testing.T) {
	conns := &connStates{states: map[net.Conn]http.ConnState{}}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		rc := http.NewResponseController(w)
		stop := time.After(5 * time.Second)
		for {
			if _, err := w.Write([]byte("ollama is warming up\n")); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}))
	srv.Config.ConnState = conns.track
	srv.Start()
	t.Cleanup(srv.Close)

	var cfg config.ProvidersConfig
	cfg.Vision.Endpoint = srv.URL
	if p := NewFactory(cfg, memory.New()).tryOllamaVision(); p != nil {
		t.Fatalf("tryOllamaVision on a 503 endpoint = %T, want nil", p)
	}

	// Well inside the probe client's 2s timeout, which would close a
	// leaked body on its own.
	deadline := time.Now().Add(time.Second)
	for conns.active() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := conns.active(); n > 0 {
		t.Errorf("%d probe connection(s) still active: the rejected response body was not closed", n)
	}
}
