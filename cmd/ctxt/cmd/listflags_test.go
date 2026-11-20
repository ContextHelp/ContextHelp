package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"hop.top/kit/go/console/output"
)

func TestParseHintList(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"csv", []string{"ux,research"}, []string{"ux", "research"}},
		{"hash syntax", []string{"#ui #inspiration"}, []string{"ui", "inspiration"}},
		{"repeated", []string{"a", "b"}, []string{"a", "b"}},
		{"mixed", []string{"#a, b", "#c  d,,e"}, []string{"a", "b", "c", "d", "e"}},
		{"empties dropped", []string{",", " ", "#", ""}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseHintList(tc.in); !slices.Equal(got, tc.want) {
				t.Errorf("parseHintList(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseMentionList(t *testing.T) {
	good := []struct {
		name string
		in   []string
		want []string
	}{
		{"csv", []string{"@client.acme,@project.foo"}, []string{"@client.acme", "@project.foo"}},
		{"whitespace", []string{"@client.acme @project.foo"}, []string{"@client.acme", "@project.foo"}},
		{"repeated", []string{"@client.acme", "@project.foo"}, []string{"@client.acme", "@project.foo"}},
		{"mixed", []string{"@a.b, @c.d", "@e.f"}, []string{"@a.b", "@c.d", "@e.f"}},
		{"deep slug", []string{"@stripe.api.checkout"}, []string{"@stripe.api.checkout"}},
		{"empty", nil, nil},
	}
	for _, tc := range good {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMentionList(tc.in)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Errorf("parseMentionList(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		})
	}
	for _, bad := range []string{"product/ctxt", "@", "@client", "client.acme", "@Client.acme", "@a.b,product/ctxt", "@a.b/c"} {
		t.Run("bad "+bad, func(t *testing.T) {
			got, err := parseMentionList([]string{bad})
			if err == nil || got != nil {
				t.Fatalf("parseMentionList(%q) = %q, %v; want usage error", bad, got, err)
			}
			if ExitCodeFor(err) != output.ExitUsage {
				t.Errorf("exit = %d, want usage", ExitCodeFor(err))
			}
		})
	}
}

// analyzeRecorder is a dpkms stand-in that records /api/v1/analyze bodies.
type analyzeRecorder struct {
	mu     sync.Mutex
	bodies []map[string]any
	srv    *httptest.Server
}

func newAnalyzeRecorder(t *testing.T) *analyzeRecorder {
	t.Helper()
	r := &analyzeRecorder{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case req.URL.Path == "/api/v1/analyze" && req.Method == http.MethodPost:
			var b map[string]any
			_ = json.NewDecoder(req.Body).Decode(&b)
			r.mu.Lock()
			r.bodies = append(r.bodies, b)
			r.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_12345678"})
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func strs(v any) []string {
	var out []string
	a, _ := v.([]any)
	for _, x := range a {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

// Every spelling of a list reaches the server as the same clean arrays,
// on analyze, bare root and capture alike.
func TestListFlagGrammarOverAPI(t *testing.T) {
	wantM := []string{"@client.acme", "@project.foo", "@person.bob"}
	wantH := []string{"ux", "research", "inspiration"}
	spellings := [][]string{
		{"--mention", "@client.acme,@project.foo,@person.bob", "--hint", "#ux,research,#inspiration"},
		{"--mention", "@client.acme @project.foo @person.bob", "--hint", "#ux #research #inspiration"},
		{"--mention", "@client.acme", "--mention", "@project.foo", "--mention", "@person.bob",
			"--hint", "ux", "--hint", "research", "--hint", "#inspiration"},
		{"--mention", "@client.acme, @project.foo", "--mention", "@person.bob", "--hint", "ux,#research", "--hint", "inspiration"},
	}
	for _, cmdArgs := range [][]string{{"analyze", "some note"}, {"some note"}} {
		for i, sp := range spellings {
			rec := newAnalyzeRecorder(t)
			args := append(append([]string{}, cmdArgs...), sp...)
			args = append(args, "--server", rec.srv.URL)
			if _, err := executeCommand(args...); err != nil {
				t.Fatalf("%v spelling %d: %v", cmdArgs, i, err)
			}
			if len(rec.bodies) != 1 {
				t.Fatalf("%v spelling %d: %d requests", cmdArgs, i, len(rec.bodies))
			}
			if got := strs(rec.bodies[0]["mentions"]); !slices.Equal(got, wantM) {
				t.Errorf("%v spelling %d: mentions = %q, want %q", cmdArgs, i, got, wantM)
			}
			if got := strs(rec.bodies[0]["hints"]); !slices.Equal(got, wantH) {
				t.Errorf("%v spelling %d: hints = %q, want %q", cmdArgs, i, got, wantH)
			}
		}
	}

	for i, sp := range spellings {
		var recs []captureRecord
		srv := startMockCaptureDPKMS(t, &recs)
		args := append([]string{"capture", "some note"}, sp...)
		args = append(args, "--server", srv.URL)
		if _, err := executeCommand(args...); err != nil {
			srv.Close()
			t.Fatalf("capture spelling %d: %v", i, err)
		}
		srv.Close()
		if got := strs(recs[0].Body["mentions"]); !slices.Equal(got, wantM) {
			t.Errorf("capture spelling %d: mentions = %q, want %q", i, got, wantM)
		}
		if got := strs(recs[0].Body["hints"]); !slices.Equal(got, wantH) {
			t.Errorf("capture spelling %d: hints = %q, want %q", i, got, wantH)
		}
	}
}

// A malformed mention is a usage error and nothing is sent.
func TestMalformedMentionRefusedNothingSent(t *testing.T) {
	for _, args := range [][]string{
		{"analyze", "note", "--mention", "product/ctxt,@concept.x"},
		{"note", "--mention", "product/ctxt"},
		{"capture", "note", "--mention", "@concept.x,bogus"},
	} {
		rec := newAnalyzeRecorder(t)
		var recs []captureRecord
		csrv := startMockCaptureDPKMS(t, &recs)
		server := rec.srv.URL
		if args[0] == "capture" {
			server = csrv.URL
		}
		_, err := executeCommand(append(args, "--server", server)...)
		csrv.Close()
		if err == nil || ExitCodeFor(err) != output.ExitUsage {
			t.Errorf("%v: err = %v (exit %d), want usage error", args, err, ExitCodeFor(err))
		}
		if len(rec.bodies) != 0 || len(recs) != 0 {
			t.Errorf("%v: request sent despite malformed mention", args)
		}
	}
}

func TestEditMentionGrammar(t *testing.T) {
	db := setupTestDB(t)
	seedEditObject(t, db)
	if _, err := db.exec("edit", "obj_123", "--mention", "product/ctxt"); exitOf(err) != output.ExitUsage {
		t.Errorf("malformed mention: exit %d (%v), want usage", exitOf(err), err)
	}
	if _, err := db.exec("edit", "obj_123", "--mention", "@ui.best-practice,@ux.onboarding", "--mention", "@a.b"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	var ms []string
	for _, m := range storedObj(t, db, "obj_123").Mentions {
		ms = append(ms, m.String())
	}
	want := []string{"ctxt://entity/ui/best-practice", "ctxt://entity/ux/onboarding", "ctxt://entity/a/b"}
	if !slices.Equal(ms, want) {
		t.Errorf("mentions = %v, want %v", ms, want)
	}
}
