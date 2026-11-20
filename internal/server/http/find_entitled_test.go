package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// A non-owner principal authenticated by a registry API key searches and
// lists exactly what its audiences' share policies grant (ADR-080): its
// viewer is compiled from grants, never the owner's Unrestricted, and
// the focus profile it names narrows nothing and widens nothing. The
// owner's key sees everything, and a principal with no grant sees nothing.
func TestFindAndListAreScopedToTheKeyPrincipalsGrants(t *testing.T) {
	ctx := context.Background()
	drv := storageutil.NewTestDriver(t)
	f := servicetest.NewHybridFixture(t, drv, true)
	f.SeedProfileCorpus(t)

	const granted, ungranted = "prn-granted", "prn-ungranted"
	for _, id := range []string{granted, ungranted} {
		sharingtest.EnsurePrincipal(t, drv, id, storage.PrincipalKindPerson, authn.RoleReader)
	}
	// The grant is data-shaped: objects of the alpha profile, searchable
	// in every mode. The profile is the grant's data, not a viewer input.
	sharingtest.Grant(t, drv, sharing.Policy{
		Name:         "alpha-objects",
		Data:         "profile==alpha",
		Fields:       map[string][]string{"*": {sharing.FieldAll}},
		Capabilities: sharing.Capabilities{Search: sharing.SearchModes},
	}, granted)

	keys := authn.NewKeysOver(drv.Auth(), nil)
	token := map[string]string{}
	for role, principal := range map[string]string{"owner": storage.OwnerPrincipal, "granted": granted, "ungranted": ungranted} {
		secret, _, err := keys.Mint(ctx, authn.MintKey{PrincipalID: principal, Name: role})
		require.NoError(t, err)
		token[role] = secret
	}
	ts := httptest.NewServer(NewRouterWithConfig(f.Svc, RouterConfig{
		Semantic: f.Sem,
		Auth:     authn.NewChain(authn.NewAPIKeyMethod(keys, nil)),
	}))
	defer ts.Close()

	do := func(t *testing.T, role, method, path, body string) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, ts.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token[role])
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s %s as %s: %s", method, path, role, raw)
		return raw
	}
	ids := func(t *testing.T, raw []byte, key string) []string {
		t.Helper()
		var out map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &out), string(raw))
		var objs []struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(out[key], &objs), string(raw))
		got := []string{}
		for _, o := range objs {
			got = append(got, o.ID)
		}
		slices.Sort(got)
		return got
	}

	everything := []string{"alpha-text", "alpha-vec", "beta-text", "beta-vec", "global-text", "global-vec"}
	alpha := []string{"alpha-text", "alpha-vec"}
	q := `"query":"` + servicetest.ProfileQuery + `","limit":20`
	none := []string{}
	type want struct{ hybrid, vector, fts []string }
	for role, w := range map[string]want{
		"owner":     {everything, everything, []string{"alpha-text", "beta-text", "global-text"}},
		"granted":   {alpha, alpha, []string{"alpha-text"}},
		"ungranted": {none, none, none},
	} {
		for mode, expect := range map[string][]string{"hybrid": w.hybrid, "vector": w.vector, "fts": w.fts} {
			// A lens naming a profile the grant does not cover changes nothing.
			for _, profile := range []string{"", "alpha", "beta"} {
				t.Run("find/"+role+"/"+mode+"/profile="+profile, func(t *testing.T) {
					body := `{` + q + `,"mode":"` + mode + `"`
					if profile != "" {
						body += `,"profile":"` + profile + `"`
					}
					require.Equal(t, expect, ids(t, do(t, role, http.MethodPost, "/api/v1/find", body+`}`), "objects"))
				})
			}
		}
	}
	for role, expect := range map[string][]string{"owner": everything, "granted": alpha, "ungranted": none} {
		t.Run("list/"+role, func(t *testing.T) {
			require.Equal(t, expect, ids(t, do(t, role, http.MethodGet, "/api/v1/objects?limit=50", ""), "data"))
		})
	}
}
