package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	ts.svc.Store.Objects().Create(context.Background(), &obj)

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
