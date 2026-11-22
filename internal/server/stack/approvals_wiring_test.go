package stack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// A built stack serves the approval queue (ADR-083 §9): an empty one
// until a request is filed; approvals.pending_ttl validates through the
// config.
func TestBuildServesTheApprovalQueue(t *testing.T) {
	cfg := &config.Config{Approvals: config.ApprovalsConfig{PendingTTL: 24 * time.Hour}}
	st := build(t, cfg, storageutil.NewTestDriver(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approvals", nil)
	rec := httptest.NewRecorder()
	st.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /approvals: %d %s, want 200", rec.Code, rec.Body.String())
	}
	var body struct {
		Data  []json.RawMessage `json:"data"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Total != 0 || len(body.Data) != 0 {
		t.Errorf("queue = %s (%v), want empty", rec.Body.String(), err)
	}
}

// The share.change kind is registered, and approvals.share.required is
// the approver count a new request is filed with (1 when unset).
func TestApprovalKinds_ShareChangeTakesItsApproverCountFromTheConfig(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	for _, tc := range []struct {
		name     string
		required int
		want     int
	}{{"default", 0, 1}, {"two", 2, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Approvals: config.ApprovalsConfig{Share: config.ApprovalKindConfig{Required: tc.required}}}
			reg, err := ApprovalKinds(drv, cfg)
			if err != nil {
				t.Fatal(err)
			}
			kind, ok := reg.Lookup(sharechange.KindName)
			if !ok {
				t.Fatalf("kinds = %v, want %s registered", reg.Names(), sharechange.KindName)
			}
			if got := kind.Required(); got != tc.want {
				t.Errorf("required = %d, want %d", got, tc.want)
			}
		})
	}
}

// The Notify hook of the approval service runs every kind's reporter,
// in order, on each notice.
func TestNotifiers_RunEveryHookInOrder(t *testing.T) {
	var got []string
	hook := func(name string) func(context.Context, approval.Notice) {
		return func(_ context.Context, n approval.Notice) { got = append(got, name+":"+n.Event) }
	}
	notifiers(hook("a"), hook("b"))(context.Background(), approval.Notice{Event: approval.EventFiled})
	if want := []string{"a:" + approval.EventFiled, "b:" + approval.EventFiled}; !slices.Equal(got, want) {
		t.Fatalf("hooks ran %v, want %v", got, want)
	}
}
