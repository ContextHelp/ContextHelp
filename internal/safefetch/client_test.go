package safefetch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Every server here is an httptest server on loopback, and every host
// name resolves through fakeResolver: no test reaches a real host or a
// real DNS server.
//
// The seam: httptest can only bind loopback, which the default policy
// blocks. treatV4LoopbackAsPublic drops 127.0.0.0/8 from the guard's
// blocked-range table, so a server on 127.0.0.1 stands in for a public
// site while one on [::1] (still blocked) stands in for an internal
// service. Enforcement itself is untouched: the same Control hook judges
// both.

// fakeResolver answers from a fixed table; a host with several entries
// in seq gets them in turn, one per lookup (DNS rebinding).
type fakeResolver struct {
	mu      sync.Mutex
	table   map[string][]netip.Addr
	seq     map[string][][]netip.Addr
	lookups map[string]int
}

func (r *fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lookups == nil {
		r.lookups = map[string]int{}
	}
	n := r.lookups[host]
	r.lookups[host]++
	if answers, ok := r.seq[host]; ok && n < len(answers) {
		return answers[n], nil
	}
	if ips, ok := r.table[host]; ok {
		return ips, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, len(s))
	for i, a := range s {
		out[i] = netip.MustParseAddr(a)
	}
	return out
}

func treatV4LoopbackAsPublic(g *Guard) *Guard {
	var r []blockedRange
	for _, b := range g.ranges {
		if b.prefix != netip.MustParsePrefix("127.0.0.0/8") {
			r = append(r, b)
		}
	}
	g.ranges = r
	return g
}

// countingServer counts the requests that reach it.
type countingServer struct {
	*httptest.Server
	hits atomic.Int32
}

func newServer(t *testing.T, h http.HandlerFunc) *countingServer {
	t.Helper()
	cs := &countingServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.hits.Add(1)
		if h != nil {
			h(w, r)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(cs.Close)
	return cs
}

// newV6Server starts a server on [::1], the stand-in internal service.
func newV6Server(t *testing.T) *countingServer {
	t.Helper()
	l, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	cs := &countingServer{}
	cs.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cs.hits.Add(1)
		fmt.Fprint(w, "internal secret")
	}))
	_ = cs.Listener.Close()
	cs.Listener = l
	cs.Start()
	t.Cleanup(cs.Close)
	return cs
}

func port(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Port()
}

func get(t *testing.T, c *http.Client, rawURL string) (*http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	return resp, err
}

func TestClient_BlocksLoopbackLiteral(t *testing.T) {
	srv := newServer(t, nil)
	_, err := get(t, Default().Client(5*time.Second), srv.URL)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("GET %s: err = %v, want ErrBlocked", srv.URL, err)
	}
	if n := srv.hits.Load(); n != 0 {
		t.Fatalf("blocked server received %d requests", n)
	}
}

func TestClient_BlocksIPv6LoopbackLiteral(t *testing.T) {
	srv := newV6Server(t)
	_, err := get(t, Default().Client(5*time.Second), srv.URL)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("GET %s: err = %v, want ErrBlocked", srv.URL, err)
	}
	if n := srv.hits.Load(); n != 0 {
		t.Fatalf("blocked server received %d requests", n)
	}
}

func TestClient_BlocksHostnameResolvingToPrivate(t *testing.T) {
	srv := newServer(t, nil)
	res := &fakeResolver{table: map[string][]netip.Addr{
		"intranet.example": addrs("127.0.0.1"),
		"mapped.example":   addrs("::ffff:127.0.0.1"),
	}}
	g := New(Options{Resolver: res})
	for _, host := range []string{"intranet.example", "mapped.example"} {
		_, err := get(t, g.Client(5*time.Second), "http://"+host+":"+port(t, srv.URL)+"/")
		if !errors.Is(err, ErrBlocked) {
			t.Fatalf("GET %s: err = %v, want ErrBlocked", host, err)
		}
	}
	if n := srv.hits.Load(); n != 0 {
		t.Fatalf("blocked server received %d requests", n)
	}
}

func TestClient_PublicFetchSucceeds(t *testing.T) {
	srv := newServer(t, nil)
	res := &fakeResolver{table: map[string][]netip.Addr{"public.example": addrs("127.0.0.1")}}
	g := treatV4LoopbackAsPublic(New(Options{Resolver: res}))
	resp, err := get(t, g.Client(5*time.Second), "http://public.example:"+port(t, srv.URL)+"/")
	if err != nil {
		t.Fatalf("public fetch: %v", err)
	}
	if resp.StatusCode != http.StatusOK || srv.hits.Load() != 1 {
		t.Fatalf("status %d, hits %d", resp.StatusCode, srv.hits.Load())
	}
}

func TestClient_SkipsBlockedAnswerAmongPublicOnes(t *testing.T) {
	pub := newServer(t, nil)
	// [::1] is refused before connect; 127.0.0.1 is "public" under the seam.
	res := &fakeResolver{table: map[string][]netip.Addr{"mixed.example": addrs("::1", "127.0.0.1")}}
	g := treatV4LoopbackAsPublic(New(Options{Resolver: res}))
	if _, err := get(t, g.Client(5*time.Second), "http://mixed.example:"+port(t, pub.URL)+"/"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if pub.hits.Load() != 1 {
		t.Fatalf("public hits = %d, want 1", pub.hits.Load())
	}
}

func TestClient_RedirectFromPublicToPrivateBlocked(t *testing.T) {
	internal := newV6Server(t)
	for _, target := range []string{
		internal.URL + "/latest/meta-data/",                                       // literal private address
		"http://metadata.example:" + port(t, internal.URL) + "/latest/meta-data/", // name resolving to it
	} {
		t.Run(target, func(t *testing.T) {
			pub := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target, http.StatusFound)
			})
			res := &fakeResolver{table: map[string][]netip.Addr{
				"public.example":   addrs("127.0.0.1"),
				"metadata.example": addrs("::1"),
			}}
			g := treatV4LoopbackAsPublic(New(Options{Resolver: res}))
			_, err := get(t, g.Client(5*time.Second), "http://public.example:"+port(t, pub.URL)+"/")
			if !errors.Is(err, ErrBlocked) {
				t.Fatalf("redirect to %s: err = %v, want ErrBlocked", target, err)
			}
			if pub.hits.Load() != 1 {
				t.Fatalf("public server hits = %d, want 1", pub.hits.Load())
			}
		})
	}
	if n := internal.hits.Load(); n != 0 {
		t.Fatalf("internal server received %d requests", n)
	}
}

func TestClient_DNSRebindingCheckedPerDial(t *testing.T) {
	// Connection: close forces each request onto a fresh dial, and so a
	// fresh lookup.
	pub := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Connection", "close")
		fmt.Fprint(w, "ok")
	})
	res := &fakeResolver{seq: map[string][][]netip.Addr{
		"rebind.example": {addrs("127.0.0.1"), addrs("::1")},
	}}
	g := treatV4LoopbackAsPublic(New(Options{Resolver: res}))
	c := g.Client(5 * time.Second)
	target := "http://rebind.example:" + port(t, pub.URL) + "/"

	if _, err := get(t, c, target); err != nil {
		t.Fatalf("first fetch (public answer): %v", err)
	}
	if _, err := get(t, c, target); !errors.Is(err, ErrBlocked) {
		t.Fatalf("second fetch (rebound to ::1): err = %v, want ErrBlocked", err)
	}
	if pub.hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", pub.hits.Load())
	}
}

func TestClient_AllowlistsPermit(t *testing.T) {
	srv := newServer(t, nil)
	res := &fakeResolver{table: map[string][]netip.Addr{"wiki.corp.example": addrs("127.0.0.1")}}
	for name, p := range map[string]Policy{
		"allow_private": {AllowPrivate: true},
		"allowed_cidrs": {AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}},
		"allowed_hosts": {AllowedHosts: []string{"wiki.corp.example"}},
	} {
		t.Run(name, func(t *testing.T) {
			g := New(Options{Policy: p, Resolver: res})
			if _, err := get(t, g.Client(5*time.Second), "http://wiki.corp.example:"+port(t, srv.URL)+"/"); err != nil {
				t.Fatalf("allowlisted fetch: %v", err)
			}
		})
	}
	// An allowed host does not open other names on the same address.
	res.table["other.example"] = addrs("127.0.0.1")
	g := New(Options{Policy: Policy{AllowedHosts: []string{"wiki.corp.example"}}, Resolver: res})
	if _, err := get(t, g.Client(5*time.Second), "http://other.example:"+port(t, srv.URL)+"/"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("other host: err = %v, want ErrBlocked", err)
	}
}

func TestClient_SchemesLimitedToHTTP(t *testing.T) {
	c := Default().Client(5 * time.Second)
	for _, u := range []string{"file:///etc/passwd", "ftp://ftp.example/x", "gopher://x.example/"} {
		if _, err := get(t, c, u); !errors.Is(err, ErrScheme) {
			t.Errorf("GET %s: err = %v, want ErrScheme", u, err)
		}
	}

	pub := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	})
	g := treatV4LoopbackAsPublic(Default())
	if _, err := get(t, g.Client(5*time.Second), pub.URL); !errors.Is(err, ErrScheme) {
		t.Errorf("redirect to file://: err = %v, want ErrScheme", err)
	}
}

func TestClient_RedirectLimit(t *testing.T) {
	loop := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/again", http.StatusFound)
	})
	g := treatV4LoopbackAsPublic(New(Options{MaxRedirects: 3}))
	_, err := get(t, g.Client(5*time.Second), loop.URL)
	if err == nil {
		t.Fatal("redirect loop: want error")
	}
	if n := loop.hits.Load(); n != 4 {
		t.Fatalf("hits = %d, want 4 (first request + 3 redirects)", n)
	}
}

func TestCheckURL(t *testing.T) {
	res := &fakeResolver{table: map[string][]netip.Addr{
		"intranet.example":  addrs("10.0.0.7"),
		"split.example":     addrs("93.184.216.34", "169.254.169.254"),
		"public.example":    addrs("93.184.216.34"),
		"wiki.corp.example": addrs("10.0.0.8"),
	}}
	g := New(Options{Resolver: res, Policy: Policy{AllowedHosts: []string{"wiki.corp.example"}}})
	ctx := context.Background()
	for _, u := range []string{
		"http://127.0.0.1:8000/", "http://[::1]/", "http://169.254.169.254/latest/meta-data/",
		"http://[::ffff:10.0.0.1]/", "https://intranet.example/", "https://split.example/",
	} {
		if err := g.CheckURL(ctx, u); !errors.Is(err, ErrBlocked) {
			t.Errorf("CheckURL(%s) = %v, want ErrBlocked", u, err)
		}
	}
	for _, u := range []string{"https://public.example/a", "http://93.184.216.34/", "https://wiki.corp.example/"} {
		if err := g.CheckURL(ctx, u); err != nil {
			t.Errorf("CheckURL(%s) = %v, want nil", u, err)
		}
	}
	if err := g.CheckURL(ctx, "file:///etc/passwd"); !errors.Is(err, ErrScheme) {
		t.Errorf("file URL: %v, want ErrScheme", err)
	}
}

func TestControl_RefusesBeforeConnect(t *testing.T) {
	g := Default()
	ctrl := g.control("")
	for _, a := range []string{"169.254.169.254:80", "10.0.0.1:443", "[::1]:80", "[fe80::1%en0]:80", "[::ffff:127.0.0.1]:80"} {
		if err := ctrl("tcp", a, nil); !errors.Is(err, ErrBlocked) {
			t.Errorf("control(%s) = %v, want ErrBlocked", a, err)
		}
	}
	if err := ctrl("tcp", "93.184.216.34:443", nil); err != nil {
		t.Errorf("control(public) = %v", err)
	}
}
