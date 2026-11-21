package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
)

// The approval commands against the in-process dpkms: no kind is
// registered, so the queue is empty and a request id is not found.
func TestApprovalCommands_EmptyQueue(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	out, err := db.exec("approval", "list")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No approval requests.") {
		t.Errorf("list printed %q", out)
	}
	out, err = db.exec("approval", "list", "--format", "json")
	if err != nil {
		t.Fatalf("list json: %v\n%s", err, out)
	}
	var listed struct {
		Approvals []approvalReply `json:"approvals"`
		Total     int             `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil || listed.Total != 0 || listed.Approvals == nil {
		t.Errorf("list json = %q (%v), want an empty list", out, err)
	}
	if _, err := db.exec("approval", "show", "apr_missing"); exitOf(err) != output.ExitNotFound {
		t.Errorf("show missing: exit %d (%v), want not found", exitOf(err), err)
	}
	if _, err := db.exec("approval", "list", "--state", "pending"); err == nil {
		t.Error("an unknown state was accepted")
	}
	if _, err := db.exec("approval", "list", "--limit", "-1"); exitOf(err) != output.ExitUsage {
		t.Errorf("negative limit: exit %d (%v), want usage", exitOf(err), err)
	}
}

// fakeApprovals is a stand-in /api/v1/approvals that records what the
// commands send. Its one request needs two approvals; the second one
// applies it and answers a once-only result.
type fakeApprovals struct {
	mu       sync.Mutex
	calls    []string
	bodies   []map[string]string
	approves int
}

const fakeDigest = "v1:sha256:feed"

func (f *fakeApprovals) record(state string, approvals int, result string) map[string]any {
	rec := map[string]any{
		"id": "apr_1", "kind": "test.change", "action": "test.change.set", "state": state,
		"requested_by": "prn_req", "reason": "because", "payload": map[string]any{"target": "prn_t"},
		"payload_digest": fakeDigest, "approvals_required": 2, "approvals": approvals,
		"requested_at": "2026-10-16T09:00:00Z", "expires_at": "2026-10-19T09:00:00Z",
		"decisions": []any{}, "can_decide": state == "requested",
	}
	if result != "" {
		rec["result"] = result
	}
	return rec
}

func (f *fakeApprovals) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
	var body map[string]string
	if b, _ := io.ReadAll(r.Body); len(b) > 0 {
		_ = json.Unmarshal(b, &body)
	}
	f.bodies = append(f.bodies, body)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/approvals":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{f.record("requested", 0, "")}, "total": 1})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/approvals/apr_1":
		_ = json.NewEncoder(w).Encode(f.record("requested", f.approves, ""))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/approvals/apr_1/approve":
		if body["payload_digest"] != fakeDigest {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "DIGEST_MISMATCH", "message": "digest", "details": map[string]any{}}})
			return
		}
		f.approves++
		if f.approves < 2 {
			_ = json.NewEncoder(w).Encode(f.record("requested", f.approves, ""))
			return
		}
		_ = json.NewEncoder(w).Encode(f.record("approved", f.approves, "the-secret"))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/approvals/apr_1/reject":
		_ = json.NewEncoder(w).Encode(f.record("rejected", f.approves, ""))
	case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/approvals/apr_1":
		_ = json.NewEncoder(w).Encode(f.record("withdrawn", f.approves, ""))
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "NOT_FOUND", "message": "no", "details": map[string]any{}}})
	}
}

func (f *fakeApprovals) sent() ([]string, []map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...), append([]map[string]string(nil), f.bodies...)
}

// approve reads the request for its digest unless --digest pins one,
// sends it with the note, and prints the once-only result alone on
// stdout; reject, withdraw and list call their routes.
func TestApprovalCommands_DecideOverTheAPI(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	fake := &fakeApprovals{}
	ts := httptest.NewServer(fake)
	t.Cleanup(ts.Close)

	out, err := db.exec("approval", "approve", "apr_1", "--note", "looks right", "--server", ts.URL)
	if err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	if !strings.Contains(out, "1 of 2 approvals") {
		t.Errorf("first approve printed %q", out)
	}
	calls, bodies := fake.sent()
	if len(calls) != 2 || calls[0] != "GET /api/v1/approvals/apr_1?" || calls[1] != "POST /api/v1/approvals/apr_1/approve?" {
		t.Fatalf("calls = %v, want the read then the decision", calls)
	}
	if bodies[1]["payload_digest"] != fakeDigest || bodies[1]["note"] != "looks right" {
		t.Errorf("decision body = %v", bodies[1])
	}

	out, err = db.exec("approval", "approve", "apr_1", "--digest", fakeDigest, "--server", ts.URL)
	if err != nil {
		t.Fatalf("final approve: %v\n%s", err, out)
	}
	if !strings.Contains(out, "the-secret") || !strings.Contains(out, "shown once") {
		t.Errorf("final approve printed %q, want the result once", out)
	}
	if calls, _ = fake.sent(); len(calls) != 3 {
		t.Errorf("--digest still read the request: %v", calls)
	}
	if _, err := db.exec("approval", "approve", "apr_1", "--digest", "v1:sha256:other", "--server", ts.URL); err == nil {
		t.Error("a mismatched digest was not reported")
	}

	out, err = db.exec("approval", "reject", "apr_1", "--digest", fakeDigest, "--note", "no", "--server", ts.URL)
	if err != nil || !strings.Contains(out, "apr_1 is rejected.") {
		t.Errorf("reject: %v\n%s", err, out)
	}
	out, err = db.exec("approval", "withdraw", "apr_1", "--server", ts.URL)
	if err != nil || !strings.Contains(out, "Withdrew approval request apr_1.") {
		t.Errorf("withdraw: %v\n%s", err, out)
	}
	out, err = db.exec("approval", "list", "--state", "requested", "--kind", "test.change", "--limit", "5", "--server", ts.URL)
	if err != nil || !strings.Contains(out, "apr_1") || !strings.Contains(out, "0/2") || !strings.Contains(out, "yes") {
		t.Errorf("list: %v\n%s", err, out)
	}
	calls, _ = fake.sent()
	last := calls[len(calls)-1]
	for _, want := range []string{"state=requested", "kind=test.change", "limit=5"} {
		if !strings.Contains(last, want) {
			t.Errorf("list sent %q, want %s", last, want)
		}
	}
	out, err = db.exec("approval", "show", "apr_1", "--server", ts.URL)
	if err != nil || !strings.Contains(out, "Digest: "+fakeDigest) || !strings.Contains(out, "You may decide this request.") {
		t.Errorf("show: %v\n%s", err, out)
	}
}
