package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// TestSyncRegistryEntities_ReportsHeldEntities: the entity sync route
// reports what an untrusted registry's entities became (held for
// approval) and the registry's effective trust level, not just a zero
// upsert count.
func TestSyncRegistryEntities_ReportsHeldEntities(t *testing.T) {
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/entities/index" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"slug":"ai.bert","title":"BERT","namespace":"ai","version_hash":"v1"},` +
			`{"slug":"ai.gpt","title":"GPT","namespace":"ai","version_hash":"v1"}]`))
	}))
	t.Cleanup(reg.Close)

	driver := storageutil.NewTestDriver(t)
	cfg := config.Config{Registries: []config.RegistryConfig{{
		Name: "pub", URL: reg.URL, SyncMode: config.RegistrySyncModeThin,
	}}}
	svc := service.NewWithOptions(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(),
		search.NewEngine(driver), "", nil, nil, cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/entities/registry-sync",
		strings.NewReader(`{"url":"`+reg.URL+`"}`))
	rec := httptest.NewRecorder()
	SyncRegistryEntities(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	var body struct {
		Upserted        int    `json:"upserted"`
		PendingApproval int    `json:"pending_approval"`
		Blocked         int    `json:"blocked"`
		TrustLevel      string `json:"trust_level"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	if body.Upserted != 0 || body.PendingApproval != 2 || body.Blocked != 0 || body.TrustLevel != "untrusted" {
		t.Errorf("body = %s, want 0 upserted, 2 pending_approval, 0 blocked, trust_level untrusted", rec.Body)
	}
}
