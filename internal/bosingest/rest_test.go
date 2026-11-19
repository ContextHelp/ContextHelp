package bosingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
	"github.com/ideacrafterslabs/ctxt/internal/bosingest"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

const (
	partyAddr   = "bos://core.parties/parties_00000000000000000000000001"
	docAddr     = "bos://core.documents/documents_00000000000000000000000001"
	deniedAddr  = "bos://core.documents/documents_00000000000000000000000002"
	goneAddr    = "bos://core.documents/documents_00000000000000000000000003"
	unknownAddr = "bos://scheduling.resources/resources_00000000000000000000000001"
)

// fakeBos serves the shape bos documents for its served command tree:
// POST /v1/commands/<path> {"args":[...],"flags":{...}} with a bearer token,
// answered {"exit_code":0,"data":[row]}; a refusal is 403 (exit 5) and a
// record the caller cannot find 404 (exit 3).
func fakeBos(t *testing.T, calls *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ctxt-token" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req struct {
			Args []string `json:"args"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		*calls = append(*calls, strings.TrimPrefix(r.URL.Path, "/v1/commands/")+" "+strings.Join(req.Args, ","))
		reply := func(status int, v any) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(v)
		}
		switch {
		case r.URL.Path == "/v1/commands/party/get" && req.Args[0] == "parties_00000000000000000000000001":
			reply(200, map[string]any{"exit_code": 0, "data": []map[string]any{{"id": req.Args[0], "name": "Acme Holdings", "kind": "organization", "identifiers": []map[string]any{{"scheme": "siren", "value": "123456789"}}}}})
		case r.URL.Path == "/v1/commands/billing/get" || r.URL.Path == "/v1/commands/contract/show":
			reply(200, map[string]any{"exit_code": 0, "data": []map[string]any{{"id": req.Args[0], "title": "row of " + r.URL.Path}}})
		case r.URL.Path == "/v1/commands/party/get":
			reply(200, map[string]any{"exit_code": 0, "data": []map[string]any{{"id": req.Args[0], "name": "Person or org " + req.Args[0]}}})
		case r.URL.Path == "/v1/commands/document/get" && req.Args[0] == "documents_00000000000000000000000001":
			reply(200, map[string]any{"exit_code": 0, "data": []map[string]any{{"id": req.Args[0], "title": "Engagement letter", "kind": "contract", "versions": 2, "sha256": "abc"}}})
		case r.URL.Path == "/v1/commands/document/get" && req.Args[0] == "documents_00000000000000000000000002":
			reply(403, map[string]any{"exit_code": 5, "code": "forbidden", "message": "not allowed"})
		case r.URL.Path == "/v1/commands/document/get":
			reply(404, map[string]any{"exit_code": 3, "code": "not_found"})
		default:
			reply(500, map[string]any{"exit_code": 1})
		}
	}))
}

func TestRESTReaderReadsTheServedCommands(t *testing.T) {
	var calls []string
	srv := fakeBos(t, &calls)
	defer srv.Close()
	rd := &bosingest.RESTReader{BaseURL: srv.URL, Token: "ctxt-token"}
	ctx := context.Background()

	rec, err := rd.Read(ctx, partyAddr, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: Acme Holdings", "identifiers[0].scheme: siren", "kind: organization"} {
		if !strings.Contains(rec.Content, want) {
			t.Errorf("content lacks %q: %s", want, rec.Content)
		}
	}
	if rec.Metadata["title"] != "Acme Holdings" || rec.Metadata["bos_revision"] != int64(4) {
		t.Errorf("metadata %v", rec.Metadata)
	}
	if rec, err = rd.Read(ctx, docAddr, 0); err != nil || rec.Metadata["title"] != "Engagement letter" {
		t.Fatalf("document: %v %v", rec, err)
	}
	if want := []string{"party/get parties_00000000000000000000000001", "document/get documents_00000000000000000000000001"}; strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls %v", calls)
	}
	// the authority's decision applies: refused and not found are ErrNoAccess
	for _, a := range []string{deniedAddr, goneAddr} {
		if _, err := rd.Read(ctx, a, 0); !errors.Is(err, bosingest.ErrNoAccess) {
			t.Errorf("%s: err %v, want ErrNoAccess", a, err)
		}
	}
	// a type with no read command is unmapped; a wrong credential is an error, not a refusal
	if _, err := rd.Read(ctx, unknownAddr, 0); !errors.Is(err, bosingest.ErrUnmapped) {
		t.Errorf("unmapped: %v", err)
	}
	bad := &bosingest.RESTReader{BaseURL: srv.URL, Token: "wrong"}
	if _, err := bad.Read(ctx, partyAddr, 0); err == nil || errors.Is(err, bosingest.ErrNoAccess) {
		t.Errorf("a 401 must be a retryable error, got %v", err)
	}
}

// End to end over the fake: a record is stored from the served command, a
// refusal on a later recheck retracts it, and a refused record is never stored.
func TestSinkOverTheServedCommands(t *testing.T) {
	var calls []string
	srv := fakeBos(t, &calls)
	defer srv.Close()
	ctx := context.Background()
	d := storageutil.NewTestDriver(t)
	s := &bosingest.Sink{Reader: &bosingest.RESTReader{BaseURL: srv.URL, Token: "ctxt-token"},
		Store: d.Objects(), Runner: ingest.NewRunner(d.Objects())}
	ev := func(subject, change string) bosaccess.Event {
		return bosaccess.Event{ID: "1", Subject: subject, Data: bosaccess.EventData{Change: change, Scope: "s", Sensitivity: "internal", Revision: 2}}
	}

	if err := s.Apply(ctx, ev(partyAddr, "upserted")); err != nil {
		t.Fatal(err)
	}
	o, _ := d.Objects().GetBySourceKey(ctx, partyAddr)
	if o == nil || o.Source != "bos" || !strings.Contains(o.RawContent, "Acme Holdings") {
		t.Fatalf("party not stored: %+v", o)
	}
	if err := s.Apply(ctx, ev(deniedAddr, "upserted")); err != nil {
		t.Fatal(err)
	}
	if o, _ := d.Objects().GetBySourceKey(ctx, deniedAddr); o != nil {
		t.Fatal("a refused record was stored")
	}
	if err := s.Apply(ctx, ev(unknownAddr, "upserted")); err != nil {
		t.Fatal(err)
	}
	if o, _ := d.Objects().GetBySourceKey(ctx, unknownAddr); o != nil {
		t.Fatal("an unmapped record was stored")
	}
	// a record held earlier that the credential can no longer read goes on
	// the next recheck
	if _, err := s.Runner.Run(ctx, held{key: deniedAddr}); err != nil {
		t.Fatal(err)
	}
	if o, _ := d.Objects().GetBySourceKey(ctx, deniedAddr); o == nil {
		t.Fatal("setup: the held copy is missing")
	}
	if err := s.Apply(ctx, ev(deniedAddr, "access_changed")); err != nil {
		t.Fatal(err)
	}
	if o, _ := d.Objects().GetBySourceKey(ctx, deniedAddr); o != nil {
		t.Fatal("a record the credential can no longer read is still held")
	}
}

type held struct{ key string }

func (held) Name() string { return "bos" }
func (h held) Fetch(context.Context) ([]ingest.Object, error) {
	return []ingest.Object{{ID: h.key, SourceKey: h.key, Type: "core.documents", Content: "old"}}, nil
}

// Every mapped type reaches its served command with the record's id.
func TestEveryMappedTypeUsesItsCommand(t *testing.T) {
	var calls []string
	srv := fakeBos(t, &calls)
	defer srv.Close()
	rd := &bosingest.RESTReader{BaseURL: srv.URL, Token: "ctxt-token"}
	want := map[string]string{
		"core.parties":           "party/get",
		"core.people":            "party/get",
		"core.organizations":     "party/get",
		"core.documents":         "document/get",
		"core.billing_documents": "billing/get",
		"core.contracts":         "contract/show",
	}
	if len(want) != len(bosingest.DefaultCommands) {
		t.Fatalf("the test covers %d types, the default mapping has %d", len(want), len(bosingest.DefaultCommands))
	}
	for typ, cmd := range want {
		name := typ[strings.Index(typ, ".")+1:]
		id := name + "_00000000000000000000000001"
		calls = nil
		if typ == "core.documents" {
			id = "documents_00000000000000000000000001"
		}
		if _, err := rd.Read(context.Background(), "bos://"+typ+"/"+id, 0); err != nil {
			t.Errorf("%s: %v", typ, err)
			continue
		}
		if len(calls) != 1 || calls[0] != cmd+" "+id {
			t.Errorf("%s: calls %v, want %s %s", typ, calls, cmd, id)
		}
	}
}
