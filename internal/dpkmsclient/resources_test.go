package dpkmsclient_test

import (
	"context"
	"encoding/json"
	"go/build"
	"io"
	"log"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func discardLog() *log.Logger { return log.New(io.Discard, "", 0) }

func TestAnalyze(t *testing.T) {
	var got map[string]any
	var auth string
	srv, hits := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/analyze" {
			http.NotFound(w, r)
			return
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"job_id":"job-42"}`))
	})
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: "tok"})

	jobID, err := c.Analyze(context.Background(), dpkmsclient.AnalyzeRequest{
		Content: "hello", Type: "text", Hints: []string{"research"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if jobID != "job-42" || auth != "Bearer tok" || got["content"] != "hello" {
		t.Fatalf("job=%q auth=%q body=%v", jobID, auth, got)
	}
	minted, _ := got["idempotency_key"].(string)
	if minted == "" {
		t.Fatal("no idempotency key minted for a submission without one")
	}

	// A second submission gets its own key; a caller-supplied key is sent
	// unchanged.
	if _, err := c.Analyze(context.Background(), dpkmsclient.AnalyzeRequest{Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	if got["idempotency_key"] == minted {
		t.Error("two submissions shared one idempotency key")
	}
	if _, err := c.Analyze(context.Background(), dpkmsclient.AnalyzeRequest{Content: "x", IdempotencyKey: "k-1"}); err != nil {
		t.Fatal(err)
	}
	if got["idempotency_key"] != "k-1" {
		t.Errorf("caller key replaced: %v", got["idempotency_key"])
	}
	if n := hits.Load(); n != 3 {
		t.Errorf("dpkms saw %d requests, want 3", n)
	}
}

func TestSearch(t *testing.T) {
	var query map[string]string
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		query = map[string]string{}
		for k := range r.URL.Query() {
			query[k] = r.URL.Query().Get(k)
		}
		query["auth"] = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"o1","title":"One"}],"total":7}`))
	})
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: "tok"})

	objs, total, err := c.Search(context.Background(), dpkmsclient.SearchRequest{
		Query: "type==article", Limit: 20, Offset: 40, Profile: "founder",
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 7 || len(objs) != 1 || objs[0].ID != "o1" {
		t.Fatalf("total=%d objs=%+v", total, objs)
	}
	want := map[string]string{"q": "type==article", "limit": "20", "offset": "40", "profile": "founder", "auth": "Bearer tok"}
	if !reflect.DeepEqual(query, want) {
		t.Fatalf("query %v, want %v", query, want)
	}

	if _, _, err := c.Search(context.Background(), dpkmsclient.SearchRequest{Query: "x", Limit: 5}); err != nil {
		t.Fatal(err)
	}
	if _, ok := query["profile"]; ok {
		t.Error("empty profile sent")
	}
}

func TestSearchBadQueryIsUsage(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		dpkmsError(w, http.StatusBadRequest, "INVALID_REQUEST", "unexpected token")
	})
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL})
	_, _, err := c.Search(context.Background(), dpkmsclient.SearchRequest{Query: "(("})
	if e := envelope(t, err); e.ExitCode != 2 || !strings.Contains(e.Message, "unexpected token") {
		t.Fatalf("got %d %q", e.ExitCode, e.Message)
	}
}

// TestAnalyzeRequestWireParity keeps the client's wire type in step with
// the request the dpkms handler decodes. The client keeps its own type so
// ctxt does not link the service layer and its storage drivers.
func TestAnalyzeRequestWireParity(t *testing.T) {
	client := jsonFields(reflect.TypeFor[dpkmsclient.AnalyzeRequest]())
	server := jsonFields(reflect.TypeFor[service.AnalyzeRequest]())
	if !reflect.DeepEqual(client, server) {
		t.Fatalf("wire fields differ\nclient: %v\nserver: %v", client, server)
	}
}

// TestSearchResultType: search decodes into the shared storage type, so
// the wire shape stays the server's.
func TestSearchResultType(t *testing.T) {
	var objs []*storage.KnowledgeObject
	fn := reflect.TypeOf((*dpkmsclient.Client).Search)
	if fn.Out(0) != reflect.TypeOf(objs) {
		t.Fatalf("Search returns %v", fn.Out(0))
	}
}

func jsonFields(typ reflect.Type) []string {
	var out []string
	for f := range typ.Fields() {
		tag := f.Tag.Get("json")
		out = append(out, f.Type.String()+" "+tag)
	}
	sort.Strings(out)
	return out
}

// TestNoStoreDependency: the client never links the service layer, the
// storage drivers or the database lock (ADR-077 §1).
func TestNoStoreDependency(t *testing.T) {
	const module = "github.com/ideacrafterslabs/ctxt/"
	forbidden := []string{
		module + "internal/service",
		module + "internal/storage/sqlite",
		module + "internal/storage/postgres",
		module + "internal/dblock",
		module + "internal/idxbridge",
	}
	ctx := build.Default
	ctx.BuildTags = append(ctx.BuildTags, "fts5")
	seen := map[string]bool{}
	var walk func(path, dir string)
	walk = func(path, dir string) {
		if seen[path] {
			return
		}
		seen[path] = true
		for _, f := range forbidden {
			if path == f || strings.HasPrefix(path, f+"/") {
				t.Errorf("dpkmsclient depends on %s", path)
			}
		}
		pkg, err := ctx.Import(path, dir, 0)
		if err != nil {
			t.Fatalf("import %s: %v", path, err)
		}
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, module) {
				walk(imp, pkg.Dir)
			}
		}
	}
	walk(module+"internal/dpkmsclient", ".")
	if len(seen) < 2 {
		t.Fatalf("walked only %v; the import graph was not followed", seen)
	}
}
