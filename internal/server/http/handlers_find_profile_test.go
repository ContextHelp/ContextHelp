package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// POST /api/v1/find with a profile is a lens: it answers every profile's
// objects, and the empty profile's, in every mode, both legs included,
// exactly as without one. The body still decodes under DisallowUnknownFields.
func TestFind_ProfileIsALensInEveryMode(t *testing.T) {
	f := servicetest.NewHybridFixture(t, storageutil.NewTestDriver(t), true)
	f.SeedProfileCorpus(t)
	ts := httptest.NewServer(NewRouterWithConfig(f.Svc, RouterConfig{Semantic: f.Sem}))
	defer ts.Close()

	find := func(t *testing.T, body string) []string {
		t.Helper()
		resp, err := http.Post(ts.URL+"/api/v1/find", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Objects []struct {
				ID string `json:"id"`
			} `json:"objects"`
			Diagnostics struct {
				Semantic *struct {
					Status string `json:"status"`
				} `json:"semantic"`
			} `json:"diagnostics"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d, decode %v", resp.StatusCode, err)
		}
		if s := out.Diagnostics.Semantic; s != nil && s.Status != "ok" {
			t.Fatalf("semantic leg did not run: %s", s.Status)
		}
		ids := make([]string, len(out.Objects))
		for i, o := range out.Objects {
			ids[i] = o.ID
		}
		sort.Strings(ids)
		return ids
	}

	q := `"query":"` + servicetest.ProfileQuery + `","limit":20`
	for mode, want := range map[string][]string{
		"hybrid": {"alpha-text", "alpha-vec", "beta-text", "beta-vec", "global-text", "global-vec"},
		"vector": {"alpha-text", "alpha-vec", "beta-text", "beta-vec", "global-text", "global-vec"},
		"fts":    {"alpha-text", "beta-text", "global-text"},
	} {
		for _, profile := range []string{"alpha", "demo"} {
			t.Run(mode+"/"+profile, func(t *testing.T) {
				got := find(t, `{`+q+`,"mode":"`+mode+`","profile":"`+profile+`"}`)
				if strings.Join(got, ",") != strings.Join(want, ",") {
					t.Errorf("profile %s: got %v want %v", profile, got, want)
				}
			})
		}
	}
}
