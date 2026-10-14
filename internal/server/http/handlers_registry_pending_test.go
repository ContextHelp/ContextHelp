package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Only admin reaches the approval queue: reader and writer get 403
// naming sync:registries on list, approve and reject alike.
func TestPendingEntityRoutesAdminOnly(t *testing.T) {
	ts := roleServer(t)
	routes := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/entities/registry-pending", nil},
		{http.MethodPost, "/api/v1/entities/registry-pending/approve", map[string]any{"ids": []string{"x"}}},
		{http.MethodPost, "/api/v1/entities/registry-pending/reject", map[string]any{"ids": []string{"x"}}},
	}
	for _, rt := range routes {
		for _, tok := range []string{
			"tok-reader-fixture-0123456789abcdef0123456789",
			"tok-writer-fixture-0123456789abcdef0123456789",
		} {
			resp, body := roleDo(t, rt.method, ts.URL+rt.path, tok, rt.body)
			assertScopeDenied(t, resp, body, authn.ScopeSyncRegistries)
		}
		// Admin gets past the scope check: the handler answers (an
		// empty queue, or 404 for an ID that is not queued).
		resp, body := roleDo(t, rt.method, ts.URL+rt.path, "tok-admin-fixture-0123456789abcdef0123456789", rt.body)
		assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, resp.StatusCode, "%s %s: %s", rt.method, rt.path, body)
	}
}

// The queue over HTTP: list what an untrusted registry's sync queued,
// approve one (written, gone from the queue), reject the other.
func TestPendingEntityRoutesFlow(t *testing.T) {
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/entities/index" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`[{"slug":"ai.bert","title":"BERT","namespace":"ai","version_hash":"v1"},` +
			`{"slug":"ai.gpt","title":"GPT","namespace":"ai","version_hash":"v1"}]`))
	}))
	t.Cleanup(reg.Close)
	driver := storageutil.NewTestDriver(t)
	cfg := config.Config{Registries: []config.RegistryConfig{{Name: "pub", URL: reg.URL}}}
	svc := service.NewWithOptions(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(),
		search.NewEngine(driver), "", nil, nil, cfg)
	_, err := svc.SyncRegistryEntities(t.Context(), reg.URL)
	require.NoError(t, err)

	do := func(h http.HandlerFunc, method, target, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
		var out map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
		return rec.Code, out
	}

	code, list := do(ListPendingEntities(svc), http.MethodGet, "/api/v1/entities/registry-pending?registry=pub", "")
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 2, list["total"])

	bert := storage.PendingEntityID("pub", "ai.bert", "v1")
	gpt := storage.PendingEntityID("pub", "ai.gpt", "v1")
	code, out := do(ApprovePendingEntities(svc), http.MethodPost, "/", `{"ids":["`+bert+`"]}`)
	require.Equal(t, http.StatusOK, code, out)
	assert.EqualValues(t, 1, out["total"])
	_, err = driver.Entities().Get(t.Context(), "ai.bert")
	assert.NoError(t, err, "approved entity not written")

	code, out = do(RejectPendingEntities(svc), http.MethodPost, "/", `{"ids":["`+gpt+`"]}`)
	require.Equal(t, http.StatusOK, code, out)

	code, list = do(ListPendingEntities(svc), http.MethodGet, "/api/v1/entities/registry-pending", "")
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 0, list["total"])

	code, _ = do(ApprovePendingEntities(svc), http.MethodPost, "/", `{"all":true}`)
	assert.Equal(t, http.StatusBadRequest, code, "all without registry")
	code, _ = do(RejectPendingEntities(svc), http.MethodPost, "/", `{"ids":["`+gpt+`"]}`)
	assert.Equal(t, http.StatusNotFound, code, "reject of an ID no longer queued")
}
