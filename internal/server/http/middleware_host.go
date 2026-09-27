package http

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// HostAllowlist rejects requests whose Host header names a host the
// instance does not answer to. It defeats DNS rebinding: a hostile page
// that points its own name at 127.0.0.1 reaches the listener as
// same-origin, but its requests still carry the hostile name in Host.
type HostAllowlist struct {
	exact   map[string]bool // host:port, as net.JoinHostPort writes it
	anyPort map[string]bool // host on any port
}

// NewHostAllowlist builds an allowlist from entries in the
// server.allowed_hosts syntax (see config.ParseAllowedHost): a host
// matches on any port, host:port matches that port only.
func NewHostAllowlist(entries []string) (*HostAllowlist, error) {
	a := &HostAllowlist{exact: map[string]bool{}, anyPort: map[string]bool{}}
	for _, e := range entries {
		host, port, err := config.ParseAllowedHost(e)
		if err != nil {
			return nil, fmt.Errorf("allowed_hosts: %w", err)
		}
		if port == "" {
			a.anyPort[host] = true
			continue
		}
		a.exact[net.JoinHostPort(host, port)] = true
	}
	return a, nil
}

// Allows reports whether hostHeader is on the list. A Host without a
// port means the scheme's default port.
func (a *HostAllowlist) Allows(hostHeader string, tls bool) bool {
	host, port, err := net.SplitHostPort(hostHeader)
	if err != nil {
		host = strings.TrimSuffix(strings.TrimPrefix(hostHeader, "["), "]")
		port = "80"
		if tls {
			port = "443"
		}
	}
	host = strings.ToLower(host)
	if host == "" {
		return false
	}
	return a.anyPort[host] || a.exact[net.JoinHostPort(host, port)]
}

// Middleware answers 403 HOST_NOT_ALLOWED to any request whose Host is
// not on the list, before any route, static asset or upgrade runs.
func (a *HostAllowlist) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Allows(r.Host, r.TLS != nil) {
			WriteError(w, http.StatusForbidden, "HOST_NOT_ALLOWED",
				fmt.Sprintf("host %q is not allowed; to reach this instance by that name, add it to server.allowed_hosts", r.Host))
			return
		}
		next.ServeHTTP(w, r)
	})
}
