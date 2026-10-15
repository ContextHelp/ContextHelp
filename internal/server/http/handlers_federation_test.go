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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// newFedTestBundle creates a private test server: no authentication,
// the push route open.
func newFedTestBundle(t *testing.T) *testServerBundle {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	svc := service.New(driver, q, pipes, engine, "", nil, config.Config{})
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
	ts := newFedTestBundle(t)
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
	ts := newFedTestBundle(t)
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

// TestFederationPush_PrivateNeedsNoCredential verifies a private instance accepts a push with no credential.
func TestFederationPush_PrivateNeedsNoCredential(t *testing.T) {
	ts := newFedTestBundle(t) // no token
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

// fedKeys are the API keys of a non-private federation bundle: the
// owner's and a peer principal's.
type fedKeys struct{ owner, peer string }

// newFedPeerBundle models a non-private instance: the push route takes
// a peer principal's key and nothing else.
func newFedPeerBundle(t *testing.T) (*testServerBundle, fedKeys) {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	svc := service.New(driver, q, pipes, engine, "", nil, config.Config{})
	ctx := ownerCtx()
	require.NoError(t, driver.Auth().Principals().Create(ctx, &storage.Principal{
		ID: "prn_peer", Kind: storage.PrincipalKindPeer, DisplayName: "office", Roles: []string{authn.RoleWriter},
	}))
	keys := authn.NewKeysOver(driver.Auth(), nil)
	var k fedKeys
	var err error
	k.owner, _, err = keys.Mint(ctx, authn.MintKey{PrincipalID: storage.OwnerPrincipal})
	require.NoError(t, err)
	k.peer, _, err = keys.Mint(ctx, authn.MintKey{PrincipalID: "prn_peer"})
	require.NoError(t, err)
	return &testServerBundle{
		Server: httptest.NewServer(NewRouterWithConfig(svc, RouterConfig{
			Auth:                 authn.NewChain(authn.NewAPIKeyMethod(keys, nil)),
			RequirePeerPrincipal: true,
		})),
		svc: svc,
	}, k
}

// pushAs sends one object to the push route with token (none when
// empty) and returns the response.
func pushAs(t *testing.T, ts *testServerBundle, token, id string) *http.Response {
	t.Helper()
	body := makePushBody(t, []storage.KnowledgeObject{makeKO(id, "h-"+id)}, nil)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/federation/push", body)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// On a non-private instance the push route refuses a request with no
// credential (401), one carrying the owner's key (403 PEER_REQUIRED: an
// authenticated principal alone is never enough) and accepts a peer
// principal's key.
func TestFederationPush_NonPrivateNeedsAPeerKey(t *testing.T) {
	ts, k := newFedPeerBundle(t)
	defer ts.Close()

	resp := pushAs(t, ts, "", "o6")
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	resp = pushAs(t, ts, "ctxt_wrong", "o6")
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	resp = pushAs(t, ts, k.owner, "o6")
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	var env ErrorEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	assert.Equal(t, "PEER_REQUIRED", env.Error.Code)

	resp = pushAs(t, ts, k.peer, "o7")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result map[string]int
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, 1, result["accepted"])
}

// An object larger than the pusher's request cap goes out alone, and
// the receiver takes it: its default body limit is well above the cap.
func TestFederationPush_AcceptsObjectOverRequestCap(t *testing.T) {
	ts := newFedTestBundle(t)
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
