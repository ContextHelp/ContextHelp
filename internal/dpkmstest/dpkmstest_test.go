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

// TestWithStaticTokensProtectsEveryRole: a protected instance refuses a
// missing or foreign token and accepts each role's own.
func TestWithStaticTokensProtectsEveryRole(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	srv := dpkmstest.Start(t, driver, dpkmstest.WithStaticTokens())
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
	}

	other := dpkmstest.Start(t, storageutil.NewTestDriver(t), dpkmstest.WithStaticTokens())
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

type oneTokenProvider struct{ token string }

func (oneTokenProvider) Name() string { return "dpkmstest-custom" }

func (p oneTokenProvider) Authenticate(_ context.Context, c authn.Credential) (*authn.Principal, error) {
	if c.Empty() {
		return nil, authn.ErrNoCredential
	}
	if c.Token != p.token {
		return nil, authn.ErrInvalidCredential
	}
	return &authn.Principal{ID: "custom", Roles: []string{dpkmstest.RoleReader}, Scopes: authn.ScopesForRoles([]string{dpkmstest.RoleReader})}, nil
}

// TestWithAuthProviderIsTheSeam: an injected provider, not the static
// table, decides who gets in.
func TestWithAuthProviderIsTheSeam(t *testing.T) {
	srv := dpkmstest.Start(t, storageutil.NewTestDriver(t), dpkmstest.WithAuthProvider(oneTokenProvider{token: "let-me-in"}))
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
		t.Error("custom provider: Token(admin) should be empty; the test owns credentials")
	}
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
	dpkmstest.Start(t, storageutil.NewTestDriver(t), dpkmstest.WithStaticTokens())
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
