package registry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
)

// recordingTransport answers every request itself (no network) and keeps
// the Authorization header each one carried.
type recordingTransport struct {
	mu   sync.Mutex
	auth []string
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.auth = append(rt.auth, req.Header.Get("Authorization"))
	rt.mu.Unlock()
	body, _ := json.Marshal(oneEntry)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

func (rt *recordingTransport) sent() []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return append([]string(nil), rt.auth...)
}

// fetchVia sends one GET to target through creds over a recording
// transport and returns the Authorization header it carried.
func fetchVia(t *testing.T, creds *registry.Credentials, target string) string {
	t.Helper()
	rt := &recordingTransport{}
	client := creds.Client(&http.Client{Transport: rt})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	_ = resp.Body.Close()
	got := rt.sent()
	if len(got) != 1 {
		t.Fatalf("GET %s: %d requests, want 1", target, len(got))
	}
	return got[0]
}

func bearerRegistry(name, url string) config.RegistryConfig {
	return config.RegistryConfig{
		Name: name, URL: url,
		Auth:       config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
		TrustLevel: config.RegistryTrustLevelTrusted,
	}
}

func TestCanonicalOrigin(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://registry.example", "https://registry.example"},
		{"HTTPS://Registry.Example:443/team/", "https://registry.example"},
		{"http://registry.example:80/x", "http://registry.example"},
		{"https://registry.example:8443/x", "https://registry.example:8443"},
		{"http://registry.example:443", "http://registry.example:443"},
		{"http://127.0.0.1:5000", "http://127.0.0.1:5000"},
		{"http://[::1]:8080/", "http://[::1]:8080"},
		{"https://[::1]/", "https://[::1]"},
	}
	for _, tc := range cases {
		got, err := registry.CanonicalOrigin(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("CanonicalOrigin(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"file:///tmp/registry", "https:///no-host", "registry.example", "::"} {
		if got, err := registry.CanonicalOrigin(bad); err == nil {
			t.Errorf("CanonicalOrigin(%q) = %q, want error", bad, got)
		}
	}
}

// A project-local config layer that points a logged-in registry name at
// another host must not get that registry's token.
func TestToken_RedefinedURLGetsNoToken(t *testing.T) {
	genuine := protectedRegistry(t, "Authorization", "Bearer "+fakeToken)

	var attackerLog headerLog
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerLog.add(r.URL.Path, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(oneEntry)
	}))
	defer attacker.Close()

	// Logged in against the real registry ...
	tokens := newFakeTokens("team", genuine.URL, fakeToken)
	// ... but the config now names the attacker's URL.
	cfg := bearerRegistry("team", attacker.URL)
	var warn bytes.Buffer
	creds := registry.NewCredentials([]config.RegistryConfig{cfg}, tokens).WithWarnings(&warn)
	if _, err := registry.New(&stubEntityStore{}).WithCredentials(creds).Sync(context.Background(), cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	got := attackerLog.get("/entities/index")
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("redefined registry URL received the token (%d requests)", len(got))
	}
	realOrigin, _ := registry.CanonicalOrigin(genuine.URL)
	attackerOrigin, _ := registry.CanonicalOrigin(attacker.URL)
	msg := warn.String()
	for _, want := range []string{`"team"`, realOrigin, attackerOrigin} {
		if !strings.Contains(msg, want) {
			t.Errorf("warning lacks %q: %q", want, msg)
		}
	}
	if strings.Contains(msg, fakeToken) {
		t.Error("warning contains the token value")
	}
	if strings.Count(msg, "\n") != 1 {
		t.Errorf("want a one-line warning, got %q", msg)
	}

	// Same origin as the login: sent.
	cfg = bearerRegistry("team", genuine.URL)
	creds = registry.NewCredentials([]config.RegistryConfig{cfg}, tokens)
	if _, err := registry.New(&stubEntityStore{}).WithCredentials(creds).Sync(context.Background(), cfg); err != nil {
		t.Errorf("Sync at the login origin: %v", err)
	}
}

func TestToken_PlainHTTPNonLoopbackNotSent(t *testing.T) {
	const target = "http://registry.example/entities/index"
	cfg := bearerRegistry("paid", "http://registry.example")
	var warn bytes.Buffer
	creds := registry.NewCredentials([]config.RegistryConfig{cfg},
		newFakeTokens("paid", cfg.URL, fakeToken)).WithWarnings(&warn)

	if got := fetchVia(t, creds, target); got != "" {
		t.Errorf("token sent over plain http to a non-loopback host")
	}
	if !strings.Contains(warn.String(), `"paid"`) || !strings.Contains(warn.String(), "http") {
		t.Errorf("no plain-http warning: %q", warn.String())
	}
	if strings.Contains(warn.String(), fakeToken) {
		t.Error("warning contains the token value")
	}
}

func TestToken_HTTPSAndLoopbackSent(t *testing.T) {
	cases := []struct{ name, loginURL, target string }{
		{"https", "https://registry.example", "https://registry.example/entities/index"},
		{"https default port and case", "HTTPS://Registry.Example:443/", "https://registry.example/entities/index"},
		{"https explicit port", "https://registry.example:8443", "https://registry.example:8443/x"},
		{"loopback http", "http://127.0.0.1:5000", "http://127.0.0.1:5000/x"},
		{"localhost http", "http://localhost:5000", "http://LOCALHOST:5000/x"},
		{"ipv6 loopback http", "http://[::1]:5000", "http://[::1]:5000/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := bearerRegistry("paid", tc.loginURL)
			creds := registry.NewCredentials([]config.RegistryConfig{cfg},
				newFakeTokens("paid", tc.loginURL, fakeToken)).WithWarnings(io.Discard)
			if got := fetchVia(t, creds, tc.target); got != "Bearer "+fakeToken {
				t.Errorf("token not sent to %s", tc.target)
			}
		})
	}
}

func TestToken_PortMismatchNotSent(t *testing.T) {
	// Logged in against :8443; config now says the default https port.
	cfg := bearerRegistry("paid", "https://registry.example")
	creds := registry.NewCredentials([]config.RegistryConfig{cfg},
		newFakeTokens("paid", "https://registry.example:8443", fakeToken)).WithWarnings(io.Discard)
	if got := fetchVia(t, creds, "https://registry.example/entities/index"); got != "" {
		t.Error("token bound to :8443 sent to :443")
	}
}
