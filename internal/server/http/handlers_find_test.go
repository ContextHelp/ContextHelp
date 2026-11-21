package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A router built without a semantic source has no default model: hybrid
// find answers full-text only and reports no_default_model; with
// fallback_to_fts off it is 422 SEMANTIC_UNAVAILABLE.
func TestFind_NoSemanticSource(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/v1/find", "application/json", strings.NewReader(`{"query":"anything"}`))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Mode        string            `json:"mode"`
		Objects     []json.RawMessage `json:"objects"`
		Diagnostics struct {
			Semantic struct {
				Status string `json:"status"`
			} `json:"semantic"`
		} `json:"diagnostics"`
	}
	err = json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, decode %v", resp.StatusCode, err)
	}
	if body.Mode != "hybrid" || body.Objects == nil || body.Diagnostics.Semantic.Status != "no_default_model" {
		t.Fatalf("body = %+v", body)
	}

	resp, err = http.Post(ts.URL+"/api/v1/find", "application/json",
		strings.NewReader(`{"query":"anything","search":{"fallback_to_fts":false}}`))
	if err != nil {
		t.Fatal(err)
	}
	var env ErrorEnvelope
	err = json.NewDecoder(resp.Body).Decode(&env)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusUnprocessableEntity || env.Error.Code != "SEMANTIC_UNAVAILABLE" ||
		env.Error.Details["status"] != "no_default_model" {
		t.Fatalf("fallback off: status %d, envelope %+v, decode %v", resp.StatusCode, env, err)
	}
}

// A present but empty filter.tagged is a 400 like an empty `tagged` on
// GET /objects; an absent one selects every tag.
func TestFind_EmptyTaggedIsInvalid(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	for body, want := range map[string]int{
		`{"query":"x","filter":{"tagged":""}}`:    http.StatusBadRequest,
		`{"query":"x","filter":{"tagged":"  "}}`:  http.StatusBadRequest,
		`{"query":"x","filter":{"tagged":"a,,"}}`: http.StatusBadRequest,
		`{"query":"x","filter":{}}`:               http.StatusOK,
		`{"query":"x","filter":{"tagged":"a"}}`:   http.StatusOK,
	} {
		resp, err := http.Post(ts.URL+"/api/v1/find", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: status %d, want %d", body, resp.StatusCode, want)
		}
	}
}
