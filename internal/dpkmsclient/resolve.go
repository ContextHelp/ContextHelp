package dpkmsclient

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
)

// DefaultURL is the endpoint a client with nothing configured talks to.
const DefaultURL = "http://127.0.0.1:8080"

// Layer names the resolution step that chose an endpoint.
type Layer string

// The resolution layers, highest precedence first (ADR-077 §3).
const (
	LayerServerFlag   Layer = "--server"
	LayerInstanceFlag Layer = "--instance"
	LayerInstanceEnv  Layer = "CTXT_INSTANCE"
	LayerCurrent      Layer = "current-instance"
	LayerURLs         Layer = "server.urls"
	LayerURL          Layer = "server.url"
	LayerDefault      Layer = "default"
)

// Selection is what one invocation asked for. Empty fields select
// nothing.
type Selection struct {
	// Server is the --server URL.
	Server string
	// Instance is the --instance or CTXT_INSTANCE value: a server.urls
	// entry name, or a running local instance's name or port.
	Instance string
	// InstanceLayer says where Instance came from: LayerInstanceFlag or
	// LayerInstanceEnv.
	InstanceLayer Layer
	// Current is the selection `ctxt instance use` persisted.
	Current string
}

// Resolved is the one endpoint an invocation talks to.
type Resolved struct {
	Endpoint
	// Name is the selected server.urls entry's name, or the running
	// local instance's pidfile name; "" when neither applies.
	Name string
	// Key identifies the endpoint for client-side state: the name of
	// the server.urls entry at this URL, else the normalized URL.
	Key string
	// Layer is the resolution step that chose the endpoint.
	Layer Layer
	// Local reports an endpoint found through a local pidfile.
	Local bool
}

// Resolve picks exactly one endpoint for sel, in ADR-077 §3 order:
// --server, then --instance or CTXT_INSTANCE, then the current-instance
// state, then the first server.urls entry, then server.url, then
// DefaultURL.
//
// A name resolves to the server.urls entry with that name, else to a
// running local instance (from locals) by pidfile name or port, as
// http://127.0.0.1:<port>. A name that matches neither is a PREREQUISITE
// error (exit 70) listing what is configured; it never falls through to
// a lower layer. Other server.urls entries are reachable only by name or
// --server: there is no failover.
//
// Tokens come only from config: the token of the server.urls entry at
// the endpoint's URL, else server.token. locals is called only when a
// name matches no entry.
func Resolve(sc config.ServerConfig, sel Selection, locals func() ([]pidfile.Info, error)) (Resolved, error) {
	switch {
	case sel.Server != "":
		return atURL(sc, sel.Server, LayerServerFlag), nil
	case sel.Instance != "":
		layer := sel.InstanceLayer
		if layer == "" {
			layer = LayerInstanceFlag
		}
		return byName(sc, sel.Instance, layer, locals)
	case sel.Current != "":
		return byName(sc, sel.Current, LayerCurrent, locals)
	case len(sc.URLs) > 0:
		return entry(sc, sc.URLs[0], LayerURLs), nil
	case sc.URL != "":
		return atURL(sc, sc.URL, LayerURL), nil
	default:
		return atURL(sc, DefaultURL, LayerDefault), nil
	}
}

// entry resolves a server.urls entry.
func entry(sc config.ServerConfig, e config.ServerEndpoint, layer Layer) Resolved {
	r := Resolved{Endpoint: Endpoint{URL: e.URL, Token: e.Token}, Name: e.Name, Key: e.Name, Layer: layer}
	if r.Token == "" {
		r.Token = sc.Token
	}
	if r.Key == "" {
		r.Key = NormalizeURL(e.URL)
	}
	return r
}

// atURL resolves a bare URL, borrowing the token and name of the
// server.urls entry at the same normalized URL, if any.
func atURL(sc config.ServerConfig, raw string, layer Layer) Resolved {
	norm := NormalizeURL(raw)
	for _, e := range sc.URLs {
		if NormalizeURL(e.URL) == norm {
			r := entry(sc, e, layer)
			r.URL = raw
			return r
		}
	}
	return Resolved{Endpoint: Endpoint{URL: raw, Token: sc.Token}, Key: norm, Layer: layer}
}

// byName resolves a name or port through the server.urls names, then
// the running local instances.
func byName(sc config.ServerConfig, name string, layer Layer, locals func() ([]pidfile.Info, error)) (Resolved, error) {
	for _, e := range sc.URLs {
		if e.Name == name {
			return entry(sc, e, layer), nil
		}
	}
	infos, err := locals()
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve instance %q: list local dpkms instances: %w", name, err)
	}
	for _, info := range infos {
		if info.Name == name || strconv.Itoa(info.Port) == name {
			r := atURL(sc, fmt.Sprintf("http://127.0.0.1:%d", info.Port), layer)
			r.Name, r.Local = info.Name, true
			return r, nil
		}
	}
	return Resolved{}, unknownName(sc, name, layer, infos)
}

// unknownName is the PREREQUISITE error for a name nothing answers to.
func unknownName(sc config.ServerConfig, name string, layer Layer, infos []pidfile.Info) error {
	var named []string
	for _, e := range sc.URLs {
		if e.Name != "" {
			named = append(named, e.Name)
		}
	}
	running := make([]string, 0, len(infos))
	for _, info := range infos {
		running = append(running, fmt.Sprintf("%s (port %d)", info.Name, info.Port))
	}
	sort.Strings(running)
	list := func(xs []string) string {
		if len(xs) == 0 {
			return "none"
		}
		return strings.Join(xs, ", ")
	}

	msg := fmt.Sprintf("no dpkms instance named %q: not a server.urls name or a running local instance", name)
	fix := fmt.Sprintf("configured names: %s; running local instances: %s. Run `ctxt instance list`",
		list(named), list(running))
	if layer == LayerCurrent {
		msg = fmt.Sprintf("current instance %q (set by `ctxt instance use`) is not a server.urls name or a running local instance", name)
		fix += ", select another with `ctxt instance use <name>`, or clear it with `ctxt instance use -`"
	}
	e := output.PrerequisiteError(msg)
	e.SuggestedFix = fix
	return e
}

// NormalizeURL canonicalizes a base URL for comparison and keying:
// lowercase scheme and host, no default port, no trailing slash. A URL
// that does not parse comes back without its trailing slash.
func NormalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host + strings.TrimRight(u.EscapedPath(), "/")
}
