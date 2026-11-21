package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
	"github.com/ideacrafterslabs/ctxt/internal/ambient/enqueue"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The ambient fields ride POST /analyze to the job and, through the
// worker, to the object: ambient_source and fingerprint as metadata keys,
// client metadata merged under them, session_id on the object.
func TestAnalyzeAmbientFieldsReachObject(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	jobID := postAnalyze(t, ts, map[string]any{
		"content":        "ambient pipeline body about okapi stripes and okapi diet",
		"source":         "test",
		"ambient_source": "clipboard",
		"fingerprint":    "sha256:abc",
		"session_id":     "sess_a1b2c3d4e5f6",
		"metadata":       map[string]any{"app_name": "Terminal", "ambient_source": "spoof"},
	})

	job, err := ts.svc.Queue.Get(ownerCtx(), jobID)
	require.NoError(t, err)
	require.NotNil(t, job.UserAmbient, "the job must carry the ambient fields")
	assert.Equal(t, "sess_a1b2c3d4e5f6", job.UserAmbient.SessionID)

	job = runWorkerUntilDone(t, ts, jobID)
	require.Equal(t, storage.JobCompleted, job.Status, "job error: %s", job.Error)
	obj, err := ts.svc.Store(ownerCtx()).Objects().Get(ownerCtx(), job.ResultID)
	require.NoError(t, err)
	require.NotNil(t, obj)

	assert.Equal(t, "sess_a1b2c3d4e5f6", obj.SessionID)
	assert.Equal(t, "clipboard", obj.Metadata["ambient_source"])
	assert.Equal(t, "sha256:abc", obj.Metadata["fingerprint"])
	assert.Equal(t, map[string]any{"app_name": "Terminal", "ambient_source": "spoof"}, obj.Metadata["client"])
	assert.NotContains(t, obj.Metadata, "app_name")
}

func TestAnalyzeRawAmbientFieldsReachObject(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	objID := postAnalyze(t, ts, map[string]any{
		"content": "ambient raw body about tapirs", "raw": true,
		"ambient_source": "clipboard", "fingerprint": "sha256:raw", "session_id": "sess_raw",
	})
	obj, err := ts.svc.Store(ownerCtx()).Objects().Get(ownerCtx(), objID)
	require.NoError(t, err)
	assert.Equal(t, "sess_raw", obj.SessionID)
	assert.Equal(t, "clipboard", obj.Metadata["ambient_source"])
	assert.Equal(t, "sha256:raw", obj.Metadata["fingerprint"])
}

// First wins over HTTP: the same content again, tagged with another
// session, resolves to the stored object and leaves its session alone.
func TestAnalyzeDedupAcrossSessionsFirstWins(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	const body = "ambient dedup body about kakapo booming and kakapo nests"
	first := runWorkerUntilDone(t, ts, postAnalyze(t, ts, map[string]any{
		"content": body, "ambient_source": "clipboard", "session_id": "sess_one",
	}))
	require.Equal(t, storage.JobCompleted, first.Status, "job error: %s", first.Error)

	second := runWorkerUntilDone(t, ts, postAnalyze(t, ts, map[string]any{
		"content": body, "force": true, "ambient_source": "browser", "session_id": "sess_two",
	}))
	require.Equal(t, storage.JobCompleted, second.Status, "job error: %s", second.Error)
	assert.Equal(t, first.ResultID, second.ResultID, "same content must dedup to the first object")

	obj, err := ts.svc.Store(ownerCtx()).Objects().Get(ownerCtx(), first.ResultID)
	require.NoError(t, err)
	assert.Equal(t, "sess_one", obj.SessionID)
	assert.Equal(t, "clipboard", obj.Metadata["ambient_source"])
}

func TestAnalyzeAmbientOversizedMetadataIs400(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	raw, err := json.Marshal(map[string]any{
		"content":  "x",
		"metadata": map[string]any{"blob": strings.Repeat("m", 300<<10)},
	})
	require.NoError(t, err)
	resp, err := http.Post(ts.URL+"/api/v1/analyze", "application/json", bytes.NewReader(raw))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// The real ambient enqueue client posts a RawEvent; the server stores
// the object with the event's source, fingerprint, session and metadata
// (ADR-066 smoke contract).
func TestAmbientEnqueueClientRoundTrip(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	client, err := enqueue.New(ts.URL)
	require.NoError(t, err)
	require.NoError(t, client.Enqueue(context.Background(), ambient.RawEvent{
		Source:      "clipboard",
		Kind:        ambient.Kind("text"),
		Payload:     []byte("clipboard body about echidna spines and echidna burrows"),
		Fingerprint: "sha256:echidna",
		SessionID:   "sess_roundtrip",
		Metadata:    map[string]any{"foreground_bundle_id": "com.apple.Terminal"},
	}))

	jobs, _, err := ts.svc.Queue.List(ownerCtx(), storage.JobFilter{})
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	job := runWorkerUntilDone(t, ts, jobs[0].ID)
	require.Equal(t, storage.JobCompleted, job.Status, "job error: %s", job.Error)

	obj, err := ts.svc.Store(ownerCtx()).Objects().Get(ownerCtx(), job.ResultID)
	require.NoError(t, err)
	assert.Equal(t, "sess_roundtrip", obj.SessionID)
	assert.Equal(t, "clipboard", obj.Metadata["ambient_source"])
	assert.Equal(t, "sha256:echidna", obj.Metadata["fingerprint"])
	assert.Equal(t, map[string]any{"foreground_bundle_id": "com.apple.Terminal"}, obj.Metadata["client"])
}

// The body cap depends on the access class: none on a private instance;
// server.max_analyze_bytes of content plus headroom on protected and
// public ones, where 0 lifts it.
func TestAnalyzeBodyLimitByAccess(t *testing.T) {
	const capBytes = 1 << 20
	big := `{"content":"` + strings.Repeat("a", capBytes+service.MaxAnalyzeNoteBytes+service.MaxAnalyzeDataBytes+service.MaxAnalyzeMetadataBytes+(128<<10)) + `"}`
	for _, tc := range []struct {
		name, access string
		maxBytes     int64
		want         int
	}{
		{"private has no cap", config.AccessPrivate, capBytes, http.StatusAccepted},
		{"default access is private", "", capBytes, http.StatusAccepted},
		{"protected over cap", config.AccessProtected, capBytes, http.StatusRequestEntityTooLarge},
		{"public over cap", config.AccessPublic, capBytes, http.StatusRequestEntityTooLarge},
		{"protected zero is unlimited", config.AccessProtected, 0, http.StatusAccepted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTestServerBundle(t)
			defer ts.Close()
			ts.svc.Cfg.Server.Access = tc.access
			ts.svc.Cfg.Server.MaxAnalyzeBytes = tc.maxBytes
			for _, path := range []string{"/api/v1/analyze", "/api/v1/pipelines/enqueue"} {
				resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(big))
				require.NoError(t, err)
				resp.Body.Close()
				assert.Equal(t, tc.want, resp.StatusCode, path)
			}
		})
	}
}

// The per-field limits hold in every access mode.
func TestAnalyzeFieldLimitsHoldOnPrivate(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	raw, err := json.Marshal(map[string]any{"content": "x", "metadata": map[string]any{"b": strings.Repeat("m", 300<<10)}})
	require.NoError(t, err)
	resp, err := http.Post(ts.URL+"/api/v1/analyze", "application/json", bytes.NewReader(raw))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
