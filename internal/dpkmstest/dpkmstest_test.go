package dpkmstest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil, "dpkms"))
}

func seed(t *testing.T, driver storage.StorageDriver, id string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	if err := driver.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: id, Type: "text", Summaries: []string{"seeded " + id}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func get(t *testing.T, srv *dpkmstest.Server, path, role string) (int, map[string]any) {
	t.Helper()
	resp := srv.Request(t, http.MethodGet, path, role, nil)
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

// TestStartServesDriverOverAPI: an object seeded through the driver reads
// back through GET /api/v1/objects/{id}; an unknown id is a 404.
func TestStartServesDriverOverAPI(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	srv := dpkmstest.Start(t, driver)
	if !strings.HasPrefix(srv.URL, "http://127.0.0.1:") {
		t.Fatalf("URL = %q; want an ephemeral loopback port", srv.URL)
	}
	seed(t, driver, "obj_fixture_1")

	code, body := get(t, srv, "/api/v1/objects/obj_fixture_1", "")
	if code != http.StatusOK || body["id"] != "obj_fixture_1" {
		t.Fatalf("GET seeded object: %d %v", code, body)
	}
	if code, _ := get(t, srv, "/api/v1/objects/obj_missing", ""); code != http.StatusNotFound {
		t.Errorf("GET unknown object: %d, want 404", code)
	}
}

// TestWithKeysProtectsEveryRole: a protected instance refuses a
// missing or foreign key and accepts each role's own, each bound to its
// own registry principal.
func TestWithKeysProtectsEveryRole(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	srv := dpkmstest.Start(t, driver, dpkmstest.WithKeys())
	seed(t, driver, "obj_fixture_2")

	if code, _ := get(t, srv, "/api/v1/objects/obj_fixture_2", ""); code != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", code)
	}
	seen := map[string]bool{}
	for _, role := range dpkmstest.Roles {
		tok := srv.Token(role)
		if tok == "" || seen[tok] {
			t.Fatalf("role %s: token %q is empty or shared", role, tok)
		}
		seen[tok] = true
		if code, body := get(t, srv, "/api/v1/objects/obj_fixture_2", role); code != http.StatusOK {
			t.Errorf("role %s: %d %v, want 200", role, code, body)
		}
		code, who := get(t, srv, "/api/v1/whoami", role)
		if code != http.StatusOK || who["principal"] != srv.Principal(role) || who["provider"] != authn.MethodAPIKey {
			t.Errorf("role %s whoami: %d %v, want principal %s via api_key", role, code, who, srv.Principal(role))
		}
	}

	other := dpkmstest.Start(t, storageutil.NewTestDriver(t), dpkmstest.WithKeys())
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/api/v1/objects", nil)
	req.Header.Set("Authorization", "Bearer "+other.Token(dpkmstest.RoleAdmin))
	foreign, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	foreign.Body.Close()
	if foreign.StatusCode != http.StatusUnauthorized {
		t.Errorf("another instance's token: %d, want 401", foreign.StatusCode)
	}
}

// oneTokenMethod is a carried method that admits one token as a reader.
type oneTokenMethod struct{ token string }

func (oneTokenMethod) Name() string  { return "dpkmstest-custom" }
func (oneTokenMethod) Ambient() bool { return false }

func (m oneTokenMethod) Authenticate(_ context.Context, c authn.Credential) (*authn.Principal, bool, error) {
	if c.Token == "" {
		return nil, false, nil
	}
	if c.Token != m.token {
		return nil, false, authn.ErrInvalidCredential
	}
	return &authn.Principal{ID: "custom", Roles: []string{dpkmstest.RoleReader}, Scopes: authn.ScopesForRoles([]string{dpkmstest.RoleReader})}, true, nil
}

// TestWithAuthMethodsIsTheSeam: an injected chain, not the registry,
// decides who gets in.
func TestWithAuthMethodsIsTheSeam(t *testing.T) {
	srv := dpkmstest.Start(t, storageutil.NewTestDriver(t), dpkmstest.WithAuthMethods(oneTokenMethod{token: "let-me-in"}))
	for tok, want := range map[string]int{"": http.StatusUnauthorized, "nope": http.StatusUnauthorized, "let-me-in": http.StatusOK} {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/api/v1/objects", nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("token %q: %d, want %d", tok, resp.StatusCode, want)
		}
	}
	if srv.Token(dpkmstest.RoleAdmin) != "" {
		t.Error("custom methods: Token(admin) should be empty; the test owns credentials")
	}
}

// signIn mints a login code with role's key and exchanges it the way
// the web UI does, returning the session cookie.
func signIn(t *testing.T, srv *dpkmstest.Server, role string) *http.Cookie {
	t.Helper()
	resp := srv.Request(t, http.MethodPost, "/api/v1/ui/login-codes", role, nil)
	var lc struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lc); err != nil || resp.StatusCode != http.StatusCreated || lc.Code == "" {
		t.Fatalf("mint login code: %d %v", resp.StatusCode, err)
	}
	resp.Body.Close()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/ui/auth/session", strings.NewReader(`{"code":"`+lc.Code+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-Ctxt-CSRF", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("exchange: %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if strings.HasPrefix(c.Name, "__Host-dpkms_") {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

// withCookie sends GET path with the session cookie and the browser
// headers the web UI sends.
func withCookie(t *testing.T, srv *dpkmstest.Server, path string, c *http.Cookie) *http.Response {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+path, nil)
	req.AddCookie(c)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// TestRevocationEndToEnd: over the real API, revoking a key fails its
// next use and ends the browser session it signed in, clearing the
// cookie; revoking a principal fails its remaining key and session too;
// another principal is untouched.
func TestRevocationEndToEnd(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	srv := dpkmstest.Start(t, driver, dpkmstest.WithKeys())
	ctx := context.Background()

	readerCookie := signIn(t, srv, dpkmstest.RoleReader)
	if resp := withCookie(t, srv, "/api/v1/objects", readerCookie); resp.StatusCode != http.StatusOK {
		t.Fatalf("reader session: %d", resp.StatusCode)
	}

	// Revoke the reader's key: the key and its session end.
	keyID, err := keyIDOf(ctx, srv, dpkmstest.RoleReader)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Stack.Keys.Revoke(ctx, keyID); err != nil {
		t.Fatal(err)
	}
	if code, _ := get(t, srv, "/api/v1/objects", dpkmstest.RoleReader); code != http.StatusUnauthorized {
		t.Errorf("revoked key: %d, want 401", code)
	}
	resp := withCookie(t, srv, "/api/v1/objects", readerCookie)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("session of a revoked key: %d, want 401", resp.StatusCode)
	}
	if !clearsCookie(resp, readerCookie.Name) {
		t.Error("the dead cookie was not cleared")
	}

	// Revoke the writer principal: its key and session end; the owner's do not.
	writerCookie := signIn(t, srv, dpkmstest.RoleWriter)
	ownerCookie := signIn(t, srv, dpkmstest.RoleAdmin)
	if err := driver.Auth().Principals().Revoke(ctx, dpkmstest.PrincipalWriter, time.Now()); err != nil {
		t.Fatal(err)
	}
	if code, _ := get(t, srv, "/api/v1/objects", dpkmstest.RoleWriter); code != http.StatusUnauthorized {
		t.Errorf("key of a revoked principal: %d, want 401", code)
	}
	if resp := withCookie(t, srv, "/api/v1/objects", writerCookie); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("session of a revoked principal: %d, want 401", resp.StatusCode)
	}
	if code, _ := get(t, srv, "/api/v1/objects", dpkmstest.RoleAdmin); code != http.StatusOK {
		t.Errorf("owner key after another principal's revocation: %d, want 200", code)
	}
	if resp := withCookie(t, srv, "/api/v1/objects", ownerCookie); resp.StatusCode != http.StatusOK {
		t.Errorf("owner session after another principal's revocation: %d, want 200", resp.StatusCode)
	}
}

// keyIDOf returns the id of role's key, as the api_key method resolves it.
func keyIDOf(ctx context.Context, srv *dpkmstest.Server, role string) (string, error) {
	res, err := srv.Stack.RouteAuth.Authenticate(ctx, authn.Credential{Scheme: authn.SchemeBearer, Token: srv.Token(role)})
	if err != nil {
		return "", err
	}
	return res.Principal.Meta[authn.MetaKeyID], nil
}

// clearsCookie reports whether resp sets the cookie named name to expire.
func clearsCookie(resp *http.Response, name string) bool {
	for _, c := range resp.Cookies() {
		if c.Name == name && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

// TestUnreachableRefusesConnections: the unreachable variant's URL has
// nothing listening.
func TestUnreachableRefusesConnections(t *testing.T) {
	srv := dpkmstest.Start(t, storageutil.NewTestDriver(t), dpkmstest.Unreachable())
	if srv.Stack != nil {
		t.Fatal("unreachable instance built a stack")
	}
	resp, err := http.Get(srv.URL + "/health")
	if resp != nil {
		resp.Body.Close()
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("GET %s: err = %v; want connection refused", srv.URL, err)
	}
}

// TestStartReachesNoModelServer: building the stack probes no local
// Ollama. The default provider backends ("auto") would; the guard blocks
// the Ollama port so a probe is recorded, never delivered.
func TestStartReachesNoModelServer(t *testing.T) {
	addrs := []string{"localhost:11434", "127.0.0.1:11434"}
	for _, a := range addrs {
		t.Cleanup(testguard.Active.Block(a))
	}
	dpkmstest.Start(t, storageutil.NewTestDriver(t), dpkmstest.WithKeys())
	for _, a := range addrs {
		if n := testguard.Active.Take(a); n != 0 {
			t.Errorf("building the stack sent %d request(s) to %s", n, a)
		}
	}
}

// fatalTB records Fatal instead of ending the goroutine, so a test can
// assert Start refuses to run.
type fatalTB struct {
	testing.TB
	msg string
}

type fatalStop struct{}

func (f *fatalTB) Helper() {}

func (f *fatalTB) Fatal(args ...any) {
	for _, a := range args {
		if s, ok := a.(string); ok {
			f.msg += s
		}
	}
	panic(fatalStop{})
}

func (f *fatalTB) Fatalf(format string, _ ...any) {
	f.msg += format
	panic(fatalStop{})
}

// TestStartRequiresTestguard: a package that never installed the guard
// cannot start an instance.
func TestStartRequiresTestguard(t *testing.T) {
	saved := testguard.Active
	testguard.Active = nil
	defer func() { testguard.Active = saved }()

	f := &fatalTB{TB: t}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(fatalStop); !ok {
					panic(r)
				}
			}
		}()
		dpkmstest.Start(f, storageutil.NewTestDriver(t))
	}()
	if !strings.Contains(f.msg, "testguard") {
		t.Fatalf("Start without testguard: fatal message %q; want a refusal naming testguard", f.msg)
	}
}
