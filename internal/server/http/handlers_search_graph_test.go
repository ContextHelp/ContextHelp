package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/searchgraph"
	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// graphFixture is a service whose semantic leg reaches a fake embedding
// provider (withModel) or has no default model, seeded with the search
// graph gate corpus: profiles alpha/beta/global, an open and a secret
// entity.
func graphFixture(t *testing.T, withModel bool) *servicetest.HybridFixture {
	t.Helper()
	f := servicetest.NewHybridFixture(t, storageutil.NewTestDriver(t), withModel)
	f.SeedGraphGateCorpus(t)
	return f
}

// gatedGraphServer serves f behind bearer auth and the inbound gate; the
// reader "ops" (token tok-valid) and the admin "owner" (token tok-admin)
// are both entitled to the open namespace only; the admin bypasses the
// gate.
func gatedGraphServer(t *testing.T, f *servicetest.HybridFixture) *httptest.Server {
	t.Helper()
	for _, principal := range []string{"ops", "owner"} {
		require.NoError(t, f.Drv.Entitlements().Upsert(context.Background(), &storage.RegistryEntitlement{
			RegistryName: principal, Plan: "inbound", Namespaces: []string{servicetest.GraphOpenNamespace},
			FetchedAt: time.Now().Truncate(time.Second),
		}))
	}
	provider, err := authn.NewStatic([]authn.StaticToken{
		{Token: "tok-valid", Principal: "ops", Roles: []string{authn.RoleReader}},
		{Token: "tok-admin", Principal: "owner", Roles: []string{authn.RoleAdmin}},
	})
	require.NoError(t, err)
	ts := httptest.NewServer(NewRouterWithConfig(f.Svc, RouterConfig{
		Semantic:     f.Sem,
		Auth:         provider,
		Entitlements: registry.NewInboundGate(f.Drv.Entitlements(), f.Drv.Metering()),
	}))
	t.Cleanup(ts.Close)
	return ts
}

func openGraphServer(t *testing.T, f *servicetest.HybridFixture) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(NewRouterWithConfig(f.Svc, RouterConfig{Semantic: f.Sem}))
	t.Cleanup(ts.Close)
	return ts
}

// getGraph requests the search graph; token "" sends no credential.
func getGraph(t *testing.T, ts *httptest.Server, token string, params url.Values) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/search/graph?"+params.Encode(), nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, body
}

// graphOK asserts a 200 bare JGF response valid against the vendored JGF
// v2.1 schema and decodes it.
func graphOK(t *testing.T, resp *http.Response, body []byte) searchgraph.Document {
	t.Helper()
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &top))
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	require.Equal(t, []string{"graph"}, keys, "bare JGF document, no {data,total} envelope")
	validateAgainstJGF(t, body)
	var doc searchgraph.Document
	require.NoError(t, json.Unmarshal(body, &doc))
	assert.Equal(t, searchgraph.Vocabulary, doc.Graph.Metadata.Vocabulary)
	return doc
}

// fetchGraph requests the search graph and asserts a valid 200 document.
func fetchGraph(t *testing.T, ts *httptest.Server, token string, params url.Values) searchgraph.Document {
	t.Helper()
	resp, body := getGraph(t, ts, token, params)
	return graphOK(t, resp, body)
}

func validateAgainstJGF(t *testing.T, raw []byte) {
	t.Helper()
	const id = "http://jsongraphformat.info/v2.1/json-graph-schema.json"
	f, err := os.Open(filepath.Join("..", "..", "searchgraph", "testdata", "json-graph-schema_v2.json"))
	require.NoError(t, err)
	defer f.Close()
	schemaDoc, err := jsonschema.UnmarshalJSON(f)
	require.NoError(t, err)
	c := jsonschema.NewCompiler()
	require.NoError(t, c.AddResource(id, schemaDoc))
	schema, err := c.Compile(id)
	require.NoError(t, err)
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	require.NoError(t, err)
	require.NoError(t, schema.Validate(inst), "document violates JGF v2.1")
}

func nodesOfKind(doc searchgraph.Document, kind string) []string {
	var ids []string
	for id, n := range doc.Graph.Nodes {
		if n.Metadata.Kind == kind {
			ids = append(ids, id)
		}
	}
	return ids
}

func relationCount(doc searchgraph.Document, rel string) int {
	n := 0
	for _, e := range doc.Graph.Edges {
		if e.Relation == rel {
			n++
		}
	}
	return n
}

func alphaParams(extra ...string) url.Values {
	v := url.Values{"q": {servicetest.ProfileQuery}, "profile": {"alpha"}}
	for i := 0; i+1 < len(extra); i += 2 {
		v.Set(extra[i], extra[i+1])
	}
	return v
}

func TestSearchGraph_BareJGFScopedToProfile(t *testing.T) {
	ts := openGraphServer(t, graphFixture(t, true))
	doc := fetchGraph(t, ts, "", alphaParams())

	md := doc.Graph.Metadata
	assert.Equal(t, "hybrid", md.Mode)
	assert.Equal(t, "ok", md.SemanticStatus)
	assert.Equal(t, searchGraphDefaultLimit, md.Limit, "default limit is ctxt find's")
	assert.Equal(t, searchgraph.Caps{MaxNodes: searchgraph.DefaultMaxNodes, MaxEdges: searchgraph.DefaultMaxEdges}, md.Caps)
	assert.ElementsMatch(t, []string{"obj:alpha-text", "obj:alpha-vec"}, nodesOfKind(doc, searchgraph.KindObject),
		"only profile alpha's candidates")
	assert.Zero(t, relationCount(doc, searchgraph.RelSimilar), "similar is opt-in")

	// Unscoped: every profile's candidates.
	doc = fetchGraph(t, ts, "", url.Values{"q": {servicetest.ProfileQuery}})
	assert.Len(t, nodesOfKind(doc, searchgraph.KindObject), 2*len(servicetest.ProfileCorpusProfiles))
}

func TestSearchGraph_ParamsAndSimilar(t *testing.T) {
	ts := openGraphServer(t, graphFixture(t, true))
	doc := fetchGraph(t, ts, "", alphaParams(
		"limit", "1", "min_score", "0", "max_nodes", "3", "max_edges", "4",
		"similar", "true", "similar_threshold", "0.9",
	))
	md := doc.Graph.Metadata
	assert.Equal(t, 1, md.Limit)
	assert.Zero(t, md.Threshold, "min_score override")
	assert.Equal(t, searchgraph.Caps{MaxNodes: 3, MaxEdges: 4}, md.Caps)
	assert.LessOrEqual(t, len(doc.Graph.Nodes), 3)
	assert.LessOrEqual(t, len(doc.Graph.Edges), 4)
	assert.True(t, md.Truncated)
	assert.Equal(t, 1, md.Counts.Returned)
	assert.Equal(t, 1, md.Counts.CutLimit)

	// alpha-text and alpha-vec are indexed at cosine ~0.99.
	doc = fetchGraph(t, ts, "", alphaParams("similar", "true"))
	assert.Equal(t, 1, relationCount(doc, searchgraph.RelSimilar))
}

// Caps are enforced server-side: out-of-range values are refused, never
// clamped, and every malformed parameter is a 400 in the error envelope.
func TestSearchGraph_InvalidRequests(t *testing.T) {
	ts := openGraphServer(t, graphFixture(t, true))
	cases := map[string]url.Values{
		"missing q":                    {},
		"mode":                         alphaParams("mode", "hybrid"),
		"offset":                       alphaParams("offset", "0"),
		"limit not int":                alphaParams("limit", "ten"),
		"negative limit":               alphaParams("limit", "-1"),
		"min_score not number":         alphaParams("min_score", "high"),
		"negative min_score":           alphaParams("min_score", "-0.1"),
		"bad since":                    alphaParams("since", "yesterday"),
		"timestamp until":              alphaParams("until", "2026-09-01T00:00:00Z"),
		"min_score NaN":                alphaParams("min_score", "NaN"),
		"max_nodes zero":               alphaParams("max_nodes", "0"),
		"max_nodes over max":           alphaParams("max_nodes", "1001"),
		"max_nodes huge":               alphaParams("max_nodes", "1000000000"),
		"max_edges zero":               alphaParams("max_edges", "0"),
		"max_edges over max":           alphaParams("max_edges", "10001"),
		"max_edges not int":            alphaParams("max_edges", "1e9"),
		"similar not bool":             alphaParams("similar", "maybe"),
		"threshold without similar":    alphaParams("similar_threshold", "0.5"),
		"threshold with similar=false": alphaParams("similar", "false", "similar_threshold", "0.5"),
		"threshold zero":               alphaParams("similar", "true", "similar_threshold", "0"),
		"threshold over one":           alphaParams("similar", "true", "similar_threshold", "1.5"),
		"threshold NaN":                alphaParams("similar", "true", "similar_threshold", "NaN"),
		"threshold not number":         alphaParams("similar", "true", "similar_threshold", "high"),
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			resp, body := getGraph(t, ts, "", params)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode, string(body))
			assert.Equal(t, "INVALID_REQUEST", errorCode(t, body))
		})
	}

	// The maxima themselves are accepted.
	fetchGraph(t, ts, "", alphaParams("max_nodes", "1000", "max_edges", "10000", "similar", "true", "similar_threshold", "1"))
}

// No embedding model: a full-text-only graph with 200, the reason in
// metadata.
func TestSearchGraph_NoProvider(t *testing.T) {
	f := graphFixture(t, false)
	ts := openGraphServer(t, f)
	doc := fetchGraph(t, ts, "", alphaParams())
	assert.Equal(t, "fts_only", doc.Graph.Metadata.Mode)
	assert.Equal(t, "no_default_model", doc.Graph.Metadata.SemanticStatus)
	assert.Empty(t, doc.Graph.Metadata.VectorModel)
	assert.ElementsMatch(t, []string{"obj:alpha-text"}, nodesOfKind(doc, searchgraph.KindObject))
}

// A principal not entitled to an entity's namespace gets a document in
// which that entity does not exist: no node, no mentions edge, no
// co_mention weight, not counted, not named anywhere in the body.
func TestSearchGraph_EntitlementHidesEntities(t *testing.T) {
	f := graphFixture(t, true)

	// Guard: ungated, the secret entity is there and links the alpha pair.
	open := fetchGraph(t, openGraphServer(t, f), "", alphaParams())
	require.Contains(t, open.Graph.Nodes, "ent:"+servicetest.GraphSecretEntity)
	require.Equal(t, 1, relationCount(open, searchgraph.RelCoMention))

	ts := gatedGraphServer(t, f)
	resp, body := getGraph(t, ts, "tok-valid", alphaParams())
	doc := graphOK(t, resp, body)
	assert.ElementsMatch(t, []string{"ent:" + servicetest.GraphOpenEntity}, nodesOfKind(doc, searchgraph.KindEntity))
	assert.Zero(t, relationCount(doc, searchgraph.RelCoMention), "the only shared entity is hidden")
	assert.Equal(t, 1, relationCount(doc, searchgraph.RelMentions))
	assert.Equal(t, 1, doc.Graph.Metadata.Counts.Entities)
	assert.Equal(t, 1, doc.Graph.Nodes["ent:"+servicetest.GraphOpenEntity].Metadata.MentionCount)
	for _, secret := range []string{servicetest.GraphSecretEntity, servicetest.GraphSecretTitle, servicetest.GraphSecretNamespace} {
		assert.NotContains(t, string(body), secret)
	}
	assert.ElementsMatch(t, []string{"obj:alpha-text", "obj:alpha-vec"}, nodesOfKind(doc, searchgraph.KindObject),
		"objects are profile-scoped, not entity-gated")

	// The admin owner ignores its restrictive grant, as on every entity
	// route.
	admin := fetchGraph(t, ts, "tok-admin", alphaParams())
	assert.Contains(t, admin.Graph.Nodes, "ent:"+servicetest.GraphSecretEntity)

	// The route sits behind the /api/v1 auth middleware.
	resp, body = getGraph(t, ts, "", alphaParams())
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, string(body))
}

// A handler wired without an entity policy fails the request rather than
// serving every entity.
func TestSearchGraph_MissingPolicyFailsClosed(t *testing.T) {
	f := graphFixture(t, true)
	r := chi.NewRouter()
	r.Get("/api/v1/search/graph", SearchGraph(f.Svc, f.Sem, func(*http.Request) searchgraph.EntityVisibility { return nil }))
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	resp, body := getGraph(t, ts, "", alphaParams())
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode, string(body))
	assert.Equal(t, "INTERNAL_ERROR", errorCode(t, body))
	assert.False(t, strings.Contains(string(body), servicetest.GraphSecretEntity))
}
