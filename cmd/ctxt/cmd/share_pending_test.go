package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
)

// A sharing change by someone other than the owner that could widen
// what anyone sees is not made: the instance files a request and
// answers 202. The commands print that request in place of a result,
// with how to follow it, and exit 0.

// fakeWidening answers the sharing routes of the commands below with a
// 202 and the request it filed, and every other call with 200 and a
// canned result, recording the calls.
type fakeWidening struct {
	mu    sync.Mutex
	calls []string
	// pending makes the change routes answer 202.
	pending bool
}

func (f *fakeWidening) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	pending := f.pending
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if pending {
		w.Header().Set("Location", "/api/v1/approvals/apr_9")
		w.WriteHeader(http.StatusAccepted)
		action := map[string]string{
			"PUT /api/v1/share/audiences/partners/members/prn_x": "share.member.put",
			"POST /api/v1/share/grants":                          "share.grant.create",
			"PUT /api/v1/principals/prn_x/ceiling":               "share.ceiling.set",
			"PUT /api/v1/share/policies/colleagues-network":      "share.policy.update",
		}[r.Method+" "+r.URL.Path]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "apr_9", "kind": "share.change", "action": action, "state": "requested", "requested_by": "prn_admin",
			"payload": map[string]any{}, "payload_digest": "v1:sha256:aa", "approvals_required": 1, "approvals": 0,
			"requested_at": "2026-10-16T09:00:00Z", "expires_at": "2026-10-19T09:00:00Z", "decisions": []any{}, "can_decide": false,
		})
		return
	}
	switch r.URL.Path {
	case "/api/v1/share/audiences/partners/members/prn_x":
		_ = json.NewEncoder(w).Encode(map[string]any{"audience_id": "aud_1", "principal_id": "prn_x", "added_by": "owner", "added_at": "2026-10-16T09:00:00Z"})
	case "/api/v1/share/grants":
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "grt_1", "audience": "partners", "policy": "partners-network", "policy_version": 1})
	case "/api/v1/principals/prn_x/ceiling":
		_ = json.NewEncoder(w).Encode(map[string]any{"principal_id": "prn_x", "deny_classes": []string{"personal"}})
	case "/api/v1/share/policies/colleagues-network":
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "pol_1", "name": "colleagues-network", "version": 2})
	}
}

func TestShareCommands_PendingChangePrintsTheRequest(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	fake := &fakeWidening{pending: true}
	ts := httptest.NewServer(fake)
	t.Cleanup(ts.Close)
	policyFile := writeTemp(t, "policy.yaml", testPolicyYAML)

	for _, c := range []struct {
		name   string
		args   []string
		action string
	}{
		{"audience add", []string{"share", "audience", "add", "partners", "prn_x"}, "share.member.put"},
		{"grant", []string{"share", "grant", "partners", "partners-network"}, "share.grant.create"},
		{"ceiling", []string{"share", "ceiling", "prn_x", "--deny-classes", "personal"}, "share.ceiling.set"},
		{"policy edit", []string{"share", "policy", "edit", "colleagues-network", policyFile}, "share.policy.update"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := db.exec(append(c.args, "--server", ts.URL)...)
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if first := firstLine(out); first != "apr_9" {
				t.Errorf("first line = %q, want the request id\n%s", first, out)
			}
			for _, want := range []string{
				"Waiting for approval: apr_9 (" + c.action + ")",
				"it waits for an approver",
				"someone other than you",
				"ctxt approval show apr_9",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			for _, wrong := range []string{"Added", "Granted", "never sees", "is now at version", "has no ceiling"} {
				if strings.Contains(out, wrong) {
					t.Errorf("a pending change was reported as made (%q):\n%s", wrong, out)
				}
			}

			out, err = db.exec(append(c.args, "--server", ts.URL, "--format", "json")...)
			if err != nil {
				t.Fatalf("json: %v\n%s", err, out)
			}
			var got struct {
				Pending bool          `json:"pending"`
				Request approvalReply `json:"request"`
			}
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("decode: %v\n%s", err, out)
			}
			if !got.Pending || got.Request.ID != "apr_9" || got.Request.Action != c.action || got.Request.State != "requested" {
				t.Errorf("json = %+v", got)
			}
		})
	}
}

// When the instance applies the change, the commands print what they
// always did.
func TestShareCommands_AppliedChangeIsUnchanged(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	ts := httptest.NewServer(&fakeWidening{})
	t.Cleanup(ts.Close)
	policyFile := writeTemp(t, "policy.yaml", testPolicyYAML)

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"share", "audience", "add", "partners", "prn_x"}, "Added prn_x to partners."},
		{[]string{"share", "grant", "partners", "partners-network"}, "Granted partners-network (version 1) to partners."},
		{[]string{"share", "ceiling", "prn_x", "--deny-classes", "personal"}, "Principal prn_x never sees: personal."},
		{[]string{"share", "policy", "edit", "colleagues-network", policyFile}, "Policy colleagues-network is now at version 2."},
	} {
		out, err := db.exec(append(c.args, "--server", ts.URL)...)
		if err != nil || !strings.Contains(out, c.want) {
			t.Errorf("%v: %v\n%s\nwant %q", c.args, err, out, c.want)
		}
		if strings.Contains(out, "Waiting for approval") {
			t.Errorf("%v: an applied change reported as pending:\n%s", c.args, out)
		}
	}
}
