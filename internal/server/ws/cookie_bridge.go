package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
)

// CookieEntry represents a single browser cookie.
type CookieEntry struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"httpOnly"`
	SameSite string `json:"sameSite"`
}

// SyncMessage is the message format sent by the extension.
type SyncMessage struct {
	Type    string        `json:"type"` // "cookie_sync"
	Domain  string        `json:"domain"`
	Cookies []CookieEntry `json:"cookies"`
}

// CookieCache holds session cookies synced from the extension.
// Entries expire after 1 hour.
type CookieCache struct {
	mu      sync.RWMutex
	entries map[string]cachedEntry
}

type cachedEntry struct {
	cookies   []CookieEntry
	expiresAt time.Time
}

// NewCookieCache creates an empty CookieCache.
func NewCookieCache() *CookieCache {
	return &CookieCache{
		entries: make(map[string]cachedEntry),
	}
}

// Set stores cookies for a domain with a 1-hour TTL.
func (c *CookieCache) Set(domain string, cookies []CookieEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[domain] = cachedEntry{
		cookies:   cookies,
		expiresAt: time.Now().Add(time.Hour),
	}
}

// Get retrieves cookies for a domain, returning nil if absent or expired.
func (c *CookieCache) Get(domain string) []CookieEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[domain]
	if !ok || time.Now().After(e.expiresAt) {
		return nil
	}
	return e.cookies
}

// upgrader re-checks the extension origin, so the bridge never falls
// back to gorilla's default (Origin absent or matching Host).
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return httpserver.IsExtensionOrigin(r.Header.Get("Origin"))
	},
}

// DefaultCookieBridgePort is the preferred port for the cookie bridge server.
const DefaultCookieBridgePort = 9377

// CookieBridgeServer is a standalone WebSocket server for browser-extension cookie sync.
type CookieBridgeServer struct {
	cache  *CookieCache
	server *http.Server
}

// NewCookieBridgeServer creates a CookieBridgeServer. Serve it on a
// listener the caller has already bound (preferred port
// DefaultCookieBridgePort). hosts lists the Host headers the bridge
// answers to (the loopback names on the port it bound); nil refuses
// every request.
func NewCookieBridgeServer(cache *CookieCache, hosts *httpserver.HostAllowlist) *CookieBridgeServer {
	mux := http.NewServeMux()
	srv := &CookieBridgeServer{cache: cache}
	mux.Handle("/", guardBridge(hosts, http.HandlerFunc(srv.handleWS)))
	srv.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv
}

// guardBridge answers 403 before any upgrade unless the request names an
// allowed Host and comes from a browser-extension origin.
//
// The bridge listens on loopback outside the main router, and the peer
// address is always loopback, so it says nothing about the sender: any
// web page open in the browser can open a WebSocket to 127.0.0.1 (the
// same-origin policy does not apply to WebSockets). Browsers always send
// Origin on the handshake and pages cannot forge it, so Origin is the
// check. A request without Origin is not from a browser; the extension
// is the bridge's only client, so it is refused too. The Host check
// defeats DNS rebinding, as on the main server.
func guardBridge(hosts *httpserver.HostAllowlist, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hosts == nil || !hosts.Allows(r.Host, r.TLS != nil) {
			httpserver.WriteError(w, http.StatusForbidden, "HOST_NOT_ALLOWED",
				fmt.Sprintf("host %q is not allowed: the cookie bridge answers only to its loopback address", r.Host))
			return
		}
		if !httpserver.IsExtensionOrigin(r.Header.Get("Origin")) {
			httpserver.WriteError(w, http.StatusForbidden, "CROSS_ORIGIN_REQUEST",
				"cross-origin request refused: the cookie bridge accepts only the ctxt browser extension")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cookieBridgeShutdownTimeout bounds Serve's graceful shutdown after ctx
// is cancelled.
const cookieBridgeShutdownTimeout = 5 * time.Second

// Serve serves the WebSocket endpoint on ln and blocks until ctx is
// cancelled or serving fails. The server owns ln from here on.
func (s *CookieBridgeServer) Serve(ctx context.Context, ln net.Listener) error {
	errCh := make(chan error, 1)
	go func() { errCh <- s.server.Serve(ln) }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("cookie bridge: serve: %w", err)
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), cookieBridgeShutdownTimeout)
		defer cancel()
		if err := s.server.Shutdown(shutCtx); err != nil {
			_ = s.server.Close()
			return fmt.Errorf("cookie bridge: shutdown: %w", err)
		}
		return nil
	}
}

func (s *CookieBridgeServer) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws: upgrade error: %v", err)
		return
	}
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var sync SyncMessage
		if err := json.Unmarshal(msg, &sync); err != nil {
			continue
		}
		if sync.Type == "cookie_sync" && sync.Domain != "" {
			s.cache.Set(sync.Domain, sync.Cookies)
		}
	}
}
