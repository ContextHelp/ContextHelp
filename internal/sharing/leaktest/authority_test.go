package leaktest

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// The record-level reads of an external authority open the capabilities
// entities, MCP and export beside search. Every path that releases an object
// stays narrowed by what narrows any grant: the ceiling, the class
// exclusions of a key's policy, and the entries themselves. Each path is
// called as readers of one authority record and must name nothing else.

const (
	authScope   = "bos://core.business_scopes/business_scopes_00000000000000000000000001"
	authPerson  = "bos://core.principals/principals_00000000000000000000000001"
	authEng1    = "bos://projects.engagements/engagements_00000000000000000000000001"
	authEng2    = "bos://projects.engagements/engagements_00000000000000000000000002"
	authIssuer  = "https://login.microsoftonline.com/0f3c1c52-0000-4000-8000-000000000001/v2.0"
	authSubject = "9c0a1b5e-0000-4000-8000-00000000000a"

	authVisible  = "bos://core.documents/documents_00000000000000000000000001"
	authHidden   = "bos://core.documents/documents_00000000000000000000000002"
	authPersonal = "bos://core.documents/documents_00000000000000000000000003"

	canaryVisible  = "alphavisiblecanaryzzz"
	canaryHidden   = "betahiddencanaryzzz"
	canaryPersonal = "gammapersonalcanaryzzz"
)

type authorityWorld struct {
	f    *Fixture
	ctx  context.Context
	keys *authn.Keys
}

func authoritySet() bosaccess.Set {
	docs := "core.documents"
	s := bosaccess.Set{
		Contract: bosaccess.Contract, Epoch: 1, Mode: bosaccess.ModeFull, Scope: authScope,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Policy:      bosaccess.Policy{MaxStalenessSeconds: 86400, ReconcileIntervalSeconds: 3600, RecordBasisMaxStalenessSeconds: 3600},
		Scopes:      []bosaccess.Scope{{ID: authScope}},
		Principals:  []bosaccess.Principal{{ID: authPerson, Kind: "person", Status: "active", Identities: []bosaccess.Identity{{Issuer: authIssuer, Subject: authSubject + "-prn_auth_plain"}, {Issuer: authIssuer, Subject: authSubject + "-prn_auth_ceiling"}, {Issuer: authIssuer, Subject: authSubject + "-prn_auth_key_policy"}}}},
		RecordAccess: []bosaccess.RecordEntry{{
			ID: "rec_1", Subject: authPerson, Anchor: authEng1, Scope: authScope, TypeKey: &docs,
			Via: bosaccess.ViaAssignment, Grant: "g1",
		}},
	}
	s.Normalize()
	s.Digest = bosaccess.ContentDigest(s)
	return s
}

func startAuthority(t *testing.T) *authorityWorld {
	t.Helper()
	peer, err := bosaccess.Open(bosaccess.DeclaredProfile("test"), &bosaccess.MemStore{}, nil)
	require.NoError(t, err)
	_, err = peer.Apply(authoritySet())
	require.NoError(t, err)
	for subject, under := range map[string][]string{authVisible: {authEng1}, authHidden: {authEng2}, authPersonal: {authEng1}} {
		require.NoError(t, peer.Consume(bosaccess.Event{Subject: subject, Data: bosaccess.EventData{
			Change: "upserted", Scope: authScope, Sensitivity: "internal", Under: under}}))
	}
	require.NoError(t, peer.CatchUp())
	f := Start(t, storageutil.NewTestDriver(t), WithAuthority(peer))
	w := &authorityWorld{f: f, ctx: context.Background(), keys: authn.NewKeysOver(f.Server.Driver.Auth(), nil)}
	now := time.Now()
	for _, o := range []struct {
		key, content, entity string
		classes              []string
	}{
		{authVisible, canaryVisible + " body", "ent-visible", nil},
		{authHidden, canaryHidden + " body", "ent-hidden-only", nil},
		{authPersonal, canaryPersonal + " body", "ent-personal-only", []string{storage.ClassPersonal}},
	} {
		obj := &storage.KnowledgeObject{ID: "auth-" + o.entity, Type: "core.documents", Status: storage.ObjectStatusActive,
			RawContent: o.content, Source: sharing.RecordSourceName, SourceKey: o.key, ContentHash: "sha256:" + o.entity,
			Classes: o.classes, CreatedAt: now, UpdatedAt: now}
		f.create(t, obj)
		ent := "ctxt://entity/" + o.entity
		for _, e := range []*storage.Edge{
			{ID: "m-" + o.entity, FromType: "object", FromID: obj.ID, ToType: "entity", ToID: ent, EdgeType: storage.EdgeTypeMentions, Weight: 1, CreatedAt: now},
			{ID: "mi-" + o.entity, FromType: "entity", FromID: ent, ToType: "object", ToID: obj.ID, EdgeType: storage.EdgeTypeMentionedIn, Weight: 1, CreatedAt: now},
		} {
			require.NoError(t, f.Server.Driver.Edges().Create(w.ctx, e))
		}
	}
	return w
}

// reader creates a person bound to the authority's principal and a key,
// under a personal-class ceiling when ceiling is set and under a key policy
// that excludes the personal class when keyPolicy is set.
func (w *authorityWorld) reader(t *testing.T, id string, ceiling, keyPolicy bool) Principal {
	t.Helper()
	d := w.f.Server.Driver
	sharingtest.EnsurePrincipal(t, d, id, storage.PrincipalKindPerson, authn.RoleReader)
	require.NoError(t, d.Entitlements().Upsert(w.ctx, &storage.RegistryEntitlement{
		Kind: storage.EntitlementInbound, RegistryName: id, Plan: "inbound", Namespaces: []string{"*"}, FetchedAt: time.Now()}))
	require.NoError(t, d.Auth().Identities().Create(w.ctx, &storage.Identity{
		PrincipalID: id, Method: storage.IdentityMethodOIDC, Issuer: authIssuer, Subject: authSubject + "-" + id}))
	if ceiling {
		require.NoError(t, d.Sharing().SetDenyClasses(w.ctx, id, []string{storage.ClassPersonal}))
	}
	secret, issued, err := w.keys.Mint(w.ctx, authn.MintKey{PrincipalID: id, Name: "authority " + id})
	require.NoError(t, err)
	if keyPolicy {
		p := sharing.Policy{Name: "auth-key-" + id, Data: "source==" + sharing.RecordSourceName, ExcludeClasses: []string{storage.ClassPersonal},
			Fields: map[string][]string{"*": {sharing.FieldAll}}, Capabilities: sharing.Capabilities{Search: sharing.SearchModes, Entities: true, MCP: true}}
		spec, err := p.Spec()
		require.NoError(t, err)
		row := &storage.SharePolicy{Name: p.Name, Spec: spec}
		require.NoError(t, d.Sharing().Policies().Create(w.ctx, row))
		require.NoError(t, d.Sharing().BindIdentityPolicy(w.ctx, issued.IdentityID, row.ID))
	}
	return Principal{Name: id, ID: id, Token: secret}
}

// call sends one request as p and returns the body.
func (w *authorityWorld) call(t *testing.T, p Principal, method, path string, body any) string {
	t.Helper()
	r, err := w.f.HTTP(w.ctx, p, method, path, body)
	require.NoError(t, err)
	return string(r.Body)
}

func TestAuthorityRecordReadsStayNarrowedOnEveryPath(t *testing.T) {
	w := startAuthority(t)
	// each reader maps to the authority's person through its own identity,
	// which the set carries.
	cases := []struct {
		name      string
		ceiling   bool
		keyPolicy bool
		sees      []string // canaries it may be answered
		hides     []string // canaries and entities it must never be answered
	}{
		{"plain", false, false, []string{canaryVisible, canaryPersonal}, []string{canaryHidden, "ent-hidden-only"}},
		{"ceiling", true, false, []string{canaryVisible}, []string{canaryHidden, canaryPersonal, "ent-hidden-only", "ent-personal-only"}},
		{"key-policy", false, true, []string{canaryVisible}, []string{canaryHidden, canaryPersonal, "ent-hidden-only", "ent-personal-only"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := "prn_auth_" + strings.ReplaceAll(c.name, "-", "_")
			p := w.reader(t, id, c.ceiling, c.keyPolicy)
			paths := []struct {
				name, method, path string
				body               any
			}{
				{"list", http.MethodGet, "/objects?limit=100", nil},
				{"search-rsql", http.MethodGet, "/search?q=source==bos", nil},
				{"find-fts", http.MethodPost, "/find", map[string]any{"query": "canary", "mode": "fts"}},
				{"entities", http.MethodGet, "/entities", nil},
				{"entity-hidden", http.MethodGet, "/entities/ent-hidden-only", nil},
				{"entity-personal", http.MethodGet, "/entities/ent-personal-only", nil},
				{"backlinks-hidden", http.MethodGet, "/entities/ent-hidden-only/backlinks", nil},
				{"object-hidden", http.MethodGet, "/objects/auth-ent-hidden-only", nil},
				{"object-personal", http.MethodGet, "/objects/auth-ent-personal-only", nil},
				{"mcp-search", http.MethodPost, "/mcp/", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
					"params": map[string]any{"name": "search", "arguments": map[string]any{"query": "canary"}}}},
			}
			for _, pa := range paths {
				body := w.call(t, p, pa.method, pa.path, pa.body)
				for _, h := range c.hides {
					assert.NotContains(t, body, h, "%s answered %q: %s", pa.name, h, excerpt([]byte(body)))
				}
			}
			// the visible record does reach the reader on the list path
			assert.Contains(t, w.call(t, p, http.MethodGet, "/objects?limit=100", nil), canaryVisible)
			var out struct {
				Data []json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal([]byte(w.call(t, p, http.MethodGet, "/objects?limit=100", nil)), &out))
			assert.Len(t, out.Data, len(c.sees), "records listed")
		})
	}
}
