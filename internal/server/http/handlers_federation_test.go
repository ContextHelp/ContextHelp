package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// newFedTestBundle creates a test server with optional federation token.
func newFedTestBundle(t *testing.T, federationToken string) *testServerBundle {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	cfg := config.Config{
		Federation: config.FederationConfig{Token: federationToken},
	}
	svc := service.New(driver, q, pipes, engine, "", nil, cfg)
	return &testServerBundle{
		Server: httptest.NewServer(NewRouter(svc, false, nil)),
		svc:    svc,
	}
}

func makePushBody(t *testing.T, objects []storage.KnowledgeObject, edges []storage.Edge) *bytes.Buffer {
	t.Helper()
	req := federation.FederationPushRequest{Objects: objects, Edges: edges}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewBuffer(b)
}

func makeKO(id, hash string) storage.KnowledgeObject {
	return storage.KnowledgeObject{
		ID:          id,
		Type:        "note",
		RawContent:  "content-" + id,
		ContentHash: hash,
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
	}
}

// TestFederationPush_BasicPush verifies objects are accepted and accepted count returned.
func TestFederationPush_BasicPush(t *testing.T) {
	ts := newFedTestBundle(t, "")
	defer ts.Close()

	body := makePushBody(t, []storage.KnowledgeObject{makeKO("o1", "h1"), makeKO("o2", "h2")}, nil)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/federation/push", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var result map[string]int
	json.NewDecoder(resp.Body).Decode(&result)
	if result["accepted"] != 2 {
		t.Errorf("accepted: got %d, want 2", result["accepted"])
	}
}

// TestFederationPush_Dedup verifies duplicate objects are not counted twice.
func TestFederationPush_Dedup(t *testing.T) {
	ts := newFedTestBundle(t, "")
	defer ts.Close()

	obj := makeKO("o-dup", "hash-dup")

	// Pre-insert object directly.
	ts.svc.Store(ownerCtx()).Objects().Create(ownerCtx(), &obj)

	// Push same object — should be skipped.
	body := makePushBody(t, []storage.KnowledgeObject{obj}, nil)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/federation/push", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var result map[string]int
	json.NewDecoder(resp.Body).Decode(&result)
	if result["accepted"] != 0 {
		t.Errorf("accepted: got %d, want 0 (dup skipped)", result["accepted"])
	}
}

// TestFederationPush_AuthRejection verifies 401 when token is wrong.
func TestFederationPush_AuthRejection(t *testing.T) {
	ts := newFedTestBundle(t, "secret")
	defer ts.Close()

	body := makePushBody(t, []storage.KnowledgeObject{makeKO("o3", "h3")}, nil)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/federation/push", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", resp.StatusCode)
	}
}

// TestFederationPush_AuthSuccess verifies push succeeds with correct token.
func TestFederationPush_AuthSuccess(t *testing.T) {
	ts := newFedTestBundle(t, "secret")
	defer ts.Close()

	body := makePushBody(t, []storage.KnowledgeObject{makeKO("o4", "h4")}, nil)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/federation/push", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var result map[string]int
	json.NewDecoder(resp.Body).Decode(&result)
	if result["accepted"] != 1 {
		t.Errorf("accepted: got %d, want 1", result["accepted"])
	}
}

// TestFederationPush_NoAuthConfigured verifies requests work without any token when not configured.
func TestFederationPush_NoAuthConfigured(t *testing.T) {
	ts := newFedTestBundle(t, "") // no token
	defer ts.Close()

	body := makePushBody(t, []storage.KnowledgeObject{makeKO("o5", "h5")}, nil)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/federation/push", body)
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header — should still work.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
}

// newFedCredBundle models a non-private instance: the federation
// credential is mandatory on the push route.
func newFedCredBundle(t *testing.T, federationToken string) *testServerBundle {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	cfg := config.Config{
		Federation: config.FederationConfig{Token: federationToken},
	}
	svc := service.New(driver, q, pipes, engine, "", nil, cfg)
	return &testServerBundle{
		Server: httptest.NewServer(NewRouterWithConfig(svc, RouterConfig{
			RequireFederationCredential: true,
		})),
		svc: svc,
	}
}

// With a mandatory credential and no federation.token configured, the
// push route refuses every request — an authenticated principal alone
// must never be enough to push.
func TestFederationPush_MandatoryCredentialRefusesRouteWithoutToken(t *testing.T) {
	ts := newFedCredBundle(t, "")
	defer ts.Close()

	body := makePushBody(t, []storage.KnowledgeObject{makeKO("o6", "h6")}, nil)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/federation/push", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", resp.StatusCode)
	}
	var env ErrorEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if env.Error.Code != "FEDERATION_DISABLED" {
		t.Errorf("code: got %q, want FEDERATION_DISABLED", env.Error.Code)
	}
}

// With a mandatory credential the configured token still gates the
// push: wrong bearer → 401, correct bearer → 200.
func TestFederationPush_MandatoryCredentialGatesPerToken(t *testing.T) {
	ts := newFedCredBundle(t, "fed-secret")
	defer ts.Close()

	body := makePushBody(t, []storage.KnowledgeObject{makeKO("o7", "h7")}, nil)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/federation/push", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token status: got %d, want 401", resp.StatusCode)
	}

	body = makePushBody(t, []storage.KnowledgeObject{makeKO("o7", "h7")}, nil)
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/v1/federation/push", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fed-secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("correct token status: got %d, want 200", resp.StatusCode)
	}
}

// An object larger than the pusher's request cap goes out alone, and
// the receiver takes it: its default body limit is well above the cap.
func TestFederationPush_AcceptsObjectOverRequestCap(t *testing.T) {
	ts := newFedTestBundle(t, "")
	defer ts.Close()
	var mu sync.Mutex
	var sizes []int
	counting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sizes = append(sizes, len(b))
		mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(b))
		ts.Config.Handler.ServeHTTP(w, r)
	}))
	defer counting.Close()

	big := makeKO("o-big", "h-big")
	big.RawContent = strings.Repeat("x", federation.DefaultMaxRequestBytes+1<<20)
	objects := []storage.KnowledgeObject{makeKO("o-small", "h-small"), big}
	src := storageutil.NewTestDriver(t)
	p := federation.NewRemotePusherWithWatermarks("big", counting.URL, "", src.Watermarks(), src.Blobs())
	if err := p.Push(ownerCtx(), objects, nil, nil); err != nil {
		t.Fatalf("Push of an object over the request cap: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sizes) != 2 || sizes[1] <= federation.DefaultMaxRequestBytes {
		t.Errorf("request sizes = %v, want the big object alone in a second request over %d bytes",
			sizes, federation.DefaultMaxRequestBytes)
	}
	if got, err := ts.svc.Store(ownerCtx()).Objects().Get(ownerCtx(), "o-big"); err != nil || got == nil {
		t.Errorf("o-big not stored at the receiver: %v", err)
	}
}

// newFedLimitBundle is newFedTestBundle with federation.max_request_bytes
// set to limit.
func newFedLimitBundle(t *testing.T, limit int64) *testServerBundle {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	cfg := config.Config{Federation: config.FederationConfig{MaxRequestBytes: limit}}
	svc := service.New(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(), search.NewEngine(driver), "", nil, cfg)
	return &testServerBundle{Server: httptest.NewServer(NewRouter(svc, false, nil)), svc: svc}
}

// A push body over federation.max_request_bytes is refused with 413
// PAYLOAD_TOO_LARGE naming the limit, and nothing in it is stored; one
// within it goes through.
func TestFederationPush_RefusesBodyOverLimit(t *testing.T) {
	const limit = 64 << 10
	ts := newFedLimitBundle(t, limit)
	defer ts.Close()

	big := makeKO("o-over", "h-over")
	big.RawContent = strings.Repeat("x", 4*limit)
	resp, err := http.Post(ts.URL+"/api/v1/federation/push", "application/json",
		makePushBody(t, []storage.KnowledgeObject{big}, nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status: got %d, want 413", resp.StatusCode)
	}
	var env ErrorEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if env.Error.Code != "PAYLOAD_TOO_LARGE" || !strings.Contains(env.Error.Message, "federation.max_request_bytes") ||
		!strings.Contains(env.Error.Message, "65536") {
		t.Errorf("error = %+v, want PAYLOAD_TOO_LARGE naming federation.max_request_bytes and 65536", env.Error)
	}
	if got, _ := ts.svc.Store(ownerCtx()).Objects().Get(ownerCtx(), "o-over"); got != nil {
		t.Error("o-over stored from a refused body")
	}

	ok, err := http.Post(ts.URL+"/api/v1/federation/push", "application/json",
		makePushBody(t, []storage.KnowledgeObject{makeKO("o-under", "h-under")}, nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Errorf("body within the limit: status %d, want 200", ok.StatusCode)
	}
}

// The pusher sees the receiver's 413 for an object over its limit sent
// alone — not a broken connection it would retry as a target failure —
// so the object fails on its own and the rest of the batch lands.
func TestFederationPush_PusherSeesLimitAsObjectFailure(t *testing.T) {
	const limit = 64 << 10
	ts := newFedLimitBundle(t, limit)
	defer ts.Close()

	big := makeKO("o-big", "h-big")
	big.RawContent = strings.Repeat("x", 8<<20)
	src := storageutil.NewTestDriver(t)
	p := federation.NewRemotePusherWithWatermarks("limited", ts.URL, "", src.Watermarks(), src.Blobs())
	err := p.Push(ownerCtx(), []storage.KnowledgeObject{makeKO("o-first", "h-first"), big}, nil, nil)
	var oe *federation.ObjectError
	if !errors.As(err, &oe) || oe.ID != "o-big" || !strings.Contains(err.Error(), "413") {
		t.Fatalf("Push err = %v, want an ObjectError for o-big naming HTTP 413", err)
	}
	if got, _ := ts.svc.Store(ownerCtx()).Objects().Get(ownerCtx(), "o-first"); got == nil {
		t.Error("o-first, in the request before o-big, not stored")
	}
}

// Unset, the limit is the default, which takes any batch a pusher
// builds.
func TestFederationPush_DefaultLimitCoversPusherBatches(t *testing.T) {
	if got := (config.FederationConfig{}).RequestLimit(); got != config.DefaultFederationMaxRequestBytes {
		t.Errorf("RequestLimit() unset = %d, want %d", got, config.DefaultFederationMaxRequestBytes)
	}
	if config.DefaultFederationMaxRequestBytes <= int64(federation.DefaultMaxRequestBytes) {
		t.Errorf("default limit %d does not exceed the pusher's request cap %d",
			config.DefaultFederationMaxRequestBytes, federation.DefaultMaxRequestBytes)
	}
}
