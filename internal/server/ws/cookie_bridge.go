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

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Only accept connections from localhost or extension origins.
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		return host == "127.0.0.1" || host == "::1" || host == "localhost"
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
// DefaultCookieBridgePort).
func NewCookieBridgeServer(cache *CookieCache) *CookieBridgeServer {
	mux := http.NewServeMux()
	srv := &CookieBridgeServer{cache: cache}
	mux.HandleFunc("/", srv.handleWS)
	srv.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv
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
