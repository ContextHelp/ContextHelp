package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kitpolicy "hop.top/kit/go/runtime/policy"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
)

// newAlertCapture builds an emitter with a threshold of 1 (every event
// alerts) and a channel-backed handler so tests can wait on the async
// dispatch without sleeping.
func newAlertCapture(t *testing.T) (*security.Emitter, <-chan security.Alert) {
	t.Helper()
	em := security.New(security.Config{
		AuthFailureThreshold: 1,
		ACLDenialThreshold:   1,
		WindowDuration:       time.Minute,
	}, nil)
	ch := make(chan security.Alert, 8)
	em.AddHandler(func(_ context.Context, a security.Alert) { ch <- a })
	return em, ch
}

func waitAlert(t *testing.T, ch <-chan security.Alert) security.Alert {
	t.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for security alert")
		return security.Alert{}
	}
}

func TestRequireAuthRecordsAuthFailure(t *testing.T) {
	em, ch := newAlertCapture(t)
	h := RequireAuth(testProvider(t), em)(authedEcho(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	req.RemoteAddr = "203.0.113.7:55555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	a := waitAlert(t, ch)
	assert.Equal(t, security.EventAuthFailure, a.Kind)
	assert.Equal(t, "203.0.113.7", a.Principal)
}

func TestRequireAuthSuccessEmitsNoEvent(t *testing.T) {
	em, ch := newAlertCapture(t)
	h := RequireAuth(testProvider(t), em)(authedEcho(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Header.Set("Authorization", "Bearer tok-valid")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	select {
	case a := <-ch:
		t.Fatalf("unexpected alert on successful auth: %+v", a)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWritePolicyErrorRecordsACLDenial(t *testing.T) {
	em, ch := newAlertCapture(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipelines/x", nil)
	ctx := withSecurityEmitterCtx(req.Context(), em)
	ctx = authn.WithPrincipal(ctx, &authn.Principal{ID: "ops"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handled := writePolicyError(rec, req, &kitpolicy.PolicyDeniedError{
		PolicyName: "delete-pipeline-requires-note",
		Message:    "denied",
	})

	require.True(t, handled)
	assert.Equal(t, http.StatusConflict, rec.Code)
	a := waitAlert(t, ch)
	assert.Equal(t, security.EventACLDenial, a.Kind)
	assert.Equal(t, "ops", a.Principal)
}

func TestWritePolicyErrorWithoutEmitterStillWrites(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipelines/x", nil)
	rec := httptest.NewRecorder()
	handled := writePolicyError(rec, req, &kitpolicy.PolicyDeniedError{
		PolicyName: "p",
		Message:    "denied",
	})
	require.True(t, handled)
	assert.Equal(t, http.StatusConflict, rec.Code)
}
