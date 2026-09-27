package cmd

import (
	"net/http"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
)

// serverURLFlagUsage is the --server-url help text.
const serverURLFlagUsage = "dpkms server URL for client commands; overrides server.url and server.urls (default " +
	idxbridge.DefaultBaseURL + " when nothing is configured)"

// serverEndpoint resolves the dpkms instance the client commands
// (healthcheck, pipeline, step) talk to, the same way ctxt's client
// commands do: an explicit --server-url pins that URL, reusing the token of
// a matching server.urls entry or server.token; otherwise the primary of
// server.urls, then server.url (config file or -c), then the default.
// Tokens only ever come from config.
func serverEndpoint() idxbridge.Endpoint {
	eps := configuredEndpoints()
	f := rootCmd.PersistentFlags().Lookup("server-url")
	if f == nil || !f.Changed || f.Value.String() == "" {
		return eps[0]
	}
	raw := f.Value.String()
	base := strings.TrimRight(raw, "/")
	for _, ep := range eps {
		if strings.TrimRight(ep.URL, "/") == base {
			return idxbridge.Endpoint{URL: raw, Token: ep.Token}
		}
	}
	tok := ""
	if cfg != nil {
		tok = cfg.Server.Token
	}
	return idxbridge.Endpoint{URL: raw, Token: tok}
}

// configuredEndpoints lists the configured instances, primary first:
// server.urls (server.token fills entries without their own), else
// server.url, else the default.
func configuredEndpoints() []idxbridge.Endpoint {
	if cfg == nil {
		return []idxbridge.Endpoint{{URL: idxbridge.DefaultBaseURL}}
	}
	sc := cfg.Server
	if len(sc.URLs) > 0 {
		eps := make([]idxbridge.Endpoint, len(sc.URLs))
		for i, u := range sc.URLs {
			tok := u.Token
			if tok == "" {
				tok = sc.Token
			}
			eps[i] = idxbridge.Endpoint{URL: u.URL, Token: tok}
		}
		return eps
	}
	if sc.URL != "" {
		return []idxbridge.Endpoint{{URL: sc.URL, Token: sc.Token}}
	}
	return []idxbridge.Endpoint{{URL: idxbridge.DefaultBaseURL, Token: sc.Token}}
}

// bearerTransport attaches a bearer token to every request.
type bearerTransport struct {
	token string
}

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(r)
}
