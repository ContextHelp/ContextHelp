package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
	"github.com/ideacrafterslabs/ctxt/internal/bosobligations"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

type fakeAccessReceiver struct{ prof bosaccess.Profile }

func (f fakeAccessReceiver) Apply(bosaccess.Set) (bosaccess.Ack, error) { return bosaccess.Ack{}, nil }
func (f fakeAccessReceiver) State() bosaccess.Report                    { return bosaccess.Report{} }
func (f fakeAccessReceiver) Profile() bosaccess.Profile                 { return f.prof }
func (f fakeAccessReceiver) Scope() string                              { return "bos://biz_1" }

func TestBosObligationsHTTP_ProfileAndEndToEnd(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	svc := service.New(drv, nil, nil, nil, "", nil)
	rc := httpserver.RouterConfig{
		Access:          fakeAccessReceiver{prof: bosaccess.DeclaredProfile("1.0.0")},
		AccessPrincipal: "peer-bos",
		AccessScope:     "bos://biz_1",
	}
	router := httpserver.NewRouterWithConfig(svc, rc)

	// GET /.well-known/bos-peer returns declared profile with bos.obligations/v1.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/.well-known/bos-peer", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var prof bosaccess.Profile
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &prof))
	require.True(t, prof.Can.Erase && prof.Can.Hold && prof.Can.EraseDerived && !prof.Can.Anonymize)

	// Seed an object.
	const subj = "bos://core.documents/http_ob_1"
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, drv.Objects().Create(ownerCtx(), &storage.KnowledgeObject{
		ID: "o-ob-http-1", Type: "document", RawContent: "to erase", TextContent: "to erase",
		Source: "bos", SourceKey: subj, Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}))

	postOb := func(idem string, body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/bos/v1/obligations", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if idem != "" {
			req.Header.Set("Idempotency-Key", idem)
		}
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		return rw
	}

	// Mismatched Idempotency-Key -> 400 obligation_invalid.
	rw := postOb("wrong-key", map[string]any{
		"id": "ob-http-1", "kind": "erase", "subjects": []string{subj}, "effective_at": now.Format(time.RFC3339),
	})
	require.Equal(t, http.StatusBadRequest, rw.Code, rw.Body.String())

	// Valid erase with Idempotency-Key == id -> 200 applied + receipt.
	rw = postOb("ob-http-1", map[string]any{
		"id": "ob-http-1", "kind": "erase", "subjects": []string{subj}, "effective_at": now.Format(time.RFC3339), "tombstone": true, "derived": true,
	})
	require.Equal(t, http.StatusOK, rw.Code, rw.Body.String())
	var st bosobligations.Status
	require.NoError(t, json.Unmarshal(rw.Body.Bytes(), &st))
	require.Equal(t, bosobligations.StateApplied, st.State)
	require.Equal(t, bosobligations.SubjectDigest([]string{subj}), st.Receipt.Digest)

	// GET /bos/v1/obligations/{id} replays the status document.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bos/v1/obligations/ob-http-1", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), st.Receipt.Digest)

	// Unknown ID -> 404 obligation_unknown.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bos/v1/obligations/does-not-exist", nil))
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
