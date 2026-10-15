package leaktest

import (
	"context"
	"net/http"
	"strings"
)

// Placeholders in a case's path for what the fixture seeded.
const (
	seededObject = "{seeded-object}"
	seededJob    = "{seeded-job}"
	seededEntity = "{seeded-entity}"
)

// none is the path value of a resource the fixture does not hold: the
// route answers its not-found path.
const none = "none"

// rsqlNotes is the RSQL query the search routes run: the seeded
// object's type.
const rsqlNotes = "type==note"

// builtinPipeline is a built-in pipeline every instance registers.
const builtinPipeline = "text.short"

// jsonRPCToolsList is the MCP request every case of the mount sends.
var jsonRPCToolsList = map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}

// expand fills path's placeholders from f.
func expand(path string, f *Fixture) string {
	r := strings.NewReplacer(seededObject, f.ObjectID, seededJob, f.JobID, seededEntity, f.EntitySlug)
	return r.Replace(path)
}

// httpCall builds a case's call: method and concrete path relative to
// /api/v1 (placeholders expanded), body JSON-encoded when non-nil.
func httpCall(method, path string, body any) CallFunc {
	return func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
		return f.HTTP(ctx, p, method, expand(path, f), body)
	}
}

// get is the case of a GET route whose path is its pattern.
func get(pattern string) Case {
	return getAt(pattern, pattern)
}

// getAt is the case of a GET route called at path (parameters filled,
// query string included).
func getAt(pattern, path string) Case {
	return Case{Route: HTTPKey(http.MethodGet, pattern), Call: httpCall(http.MethodGet, path, nil)}
}

// post is the case of a POST route sending body.
func post(pattern string, body any) Case {
	return Case{Route: HTTPKey(http.MethodPost, pattern), Call: httpCall(http.MethodPost, pattern, body)}
}

// stream is the case of a GET route that streams: it holds the
// response open for StreamWindow and reports the frames received.
func stream(pattern string) Case {
	call := httpCall(http.MethodGet, pattern, nil)
	return Case{Route: HTTPKey(http.MethodGet, pattern), Call: func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
		ctx, cancel := context.WithTimeout(ctx, StreamWindow)
		defer cancel()
		return call(ctx, f, p)
	}}
}

// mcp is the case of the MCP mount at pattern (mounted on every
// method; JSON-RPC rides on POST).
func mcp(pattern string) Case {
	return Case{Route: HTTPKey("", pattern), Call: httpCall(http.MethodPost, pattern, jsonRPCToolsList)}
}

// httpCases lists the HTTP read routes in route-table order.
func httpCases() []Case {
	obj := "/objects/" + seededObject
	return []Case{
		get("/whoami"),

		get("/objects"),
		get("/objects/facets"),
		getAt("/objects/{id}", obj),
		getAt("/objects/{id}/related", obj+"/related"),
		getAt("/objects/{id}/edges", obj+"/edges"),

		get("/jobs"),
		getAt("/jobs/{id}", "/jobs/"+seededJob),

		getAt("/search", "/search?q="+rsqlNotes),
		post("/find", map[string]any{"query": "note", "mode": "fts"}),
		getAt("/search/graph", "/search/graph?q=note"),

		get("/entities"),
		getAt("/entities/resolve", "/entities/resolve?mention="+none),
		getAt("/entities/{slug}", "/entities/"+seededEntity),
		getAt("/entities/{slug}/backlinks", "/entities/"+seededEntity+"/backlinks"),
		get("/entities/registry-pending"),

		get("/pages"),
		getAt("/pages/{slug}", "/pages/"+none),
		get("/topic-index"),
		getAt("/topic-index/{topic}", "/topic-index/"+none),
		get("/stats"),

		get("/pipelines"),
		getAt("/pipelines/{name}", "/pipelines/"+builtinPipeline),
		get("/steps"),
		getAt("/steps/{name}", "/steps/"+none),
		get("/steps/registries"),

		get("/feeds"),
		getAt("/import/{id}", "/import/"+none),
		getAt("/importers/runs/{id}", "/importers/runs/"+none),
		get("/system/reminders"),

		get("/watches"),
		getAt("/watches/{id}", "/watches/"+none),
		getAt("/watches/{id}/files", "/watches/"+none+"/files"),

		get("/inbox"),
		get("/inbox/queue"),
		stream("/events"),

		get("/aliases"),
		getAt("/aliases/{alias}", "/aliases/"+none),
		get("/capture/recent"),

		get("/saved-searches"),
		getAt("/saved-searches/{name}", "/saved-searches/"+none),
		get("/search-history"),
		get("/audit-log"),

		mcp("/mcp/"),
		mcp("/mcp"),
		post("/ui/login-codes", nil),
	}
}
