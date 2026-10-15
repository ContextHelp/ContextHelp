package leaktest

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Every read route and RPC, called as the colleague and as the partner,
// answers without a server error, refuses what their capabilities
// close, names nothing hidden and counts the visible rows exactly.
func TestRestrictedReadsEveryRoute(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	for _, p := range f.Restricted() {
		require.NotEmpty(t, f.Hidden(p), "%s has nothing hidden", p.Name)
		for _, c := range Registry() {
			t.Run(p.Name+"/"+c.Route, func(t *testing.T) {
				r := Run(t, f, p, c)
				t.Logf("HTTP %d, gRPC %s, %d bytes", r.Status, r.Code, len(r.Body))
			})
		}
	}
}

// Timestamps, request ids and the credentials each instance mints for
// its principals (login codes, key ids, identity and registry ULIDs)
// differ between two instances; nothing else may.
var volatile = []*regexp.Regexp{
	regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`),
	regexp.MustCompile(`"request_id":"[^"]*"`),
	regexp.MustCompile(`"code":"[^"]*"`),
	regexp.MustCompile(`127\.0\.0\.1:\d+`),
	regexp.MustCompile(`"(id|subject|identity_id)":"[0-9a-f]{16}"`),
	regexp.MustCompile(`(idn|aud|pol|grt|acc)_[0-9A-HJKMNP-TV-Z]{26}`),
}

func normalize(b []byte) string {
	s := string(b)
	for _, re := range volatile {
		s = re.ReplaceAllString(s, "<volatile>")
	}
	return sortMCPTools(s)
}

// sortMCPTools orders the tools of a JSON-RPC tools/list answer by
// name: the MCP registry lists them in map order, which varies between
// instances and says nothing about rows. Any other body is returned as
// is.
func sortMCPTools(s string) string {
	var rpc struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  struct {
			Tools []json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(s), &rpc) != nil || len(rpc.Result.Tools) == 0 {
		return s
	}
	sort.Slice(rpc.Result.Tools, func(i, j int) bool { return string(rpc.Result.Tools[i]) < string(rpc.Result.Tools[j]) })
	out, err := json.Marshal(rpc.Result.Tools)
	if err != nil {
		return s
	}
	return "tools:" + string(out)
}

// Differential: a restricted principal's answer to every case is the
// same byte for byte, timestamps and request ids aside, whether or not
// the hidden rows exist. A difference is a leak through a count, an
// order, a score or a word.
func TestDifferentialHiddenRowsChangeNothing(t *testing.T) {
	with := Start(t, storageutil.NewTestDriver(t))
	require.NotEqual(t, len(with.Hidden(with.Colleague())), 0)
	for i, p := range with.Restricted() {
		without := Start(t, storageutil.NewTestDriver(t), WithoutHiddenFor(p.Name))
		q := without.Restricted()[i]
		require.Equal(t, with.VisibleObjects(p), without.VisibleObjects(q), "%s: the two fixtures must show the same rows", p.Name)
		for _, c := range Registry() {
			t.Run(p.Name+"/"+c.Route, func(t *testing.T) {
				a := Run(t, with, p, c)
				b := Run(t, without, q, c)
				assert.Equal(t, a.Status, b.Status, "status")
				assert.Equal(t, a.Code, b.Code, "code")
				assert.Equal(t, normalize(b.Body), normalize(a.Body), "body with the hidden rows differs from without them")
			})
		}
	}
}

func fetch(t *testing.T, f *Fixture, p Principal, path string) Result {
	t.Helper()
	r, err := f.HTTP(context.Background(), p, http.MethodGet, path, nil)
	require.NoError(t, err)
	return r
}

// Hidden rows are 404 for a restricted principal and 200 for the
// owner; capability-closed routes are 403 NOT_SHARED, never an empty
// 200; a route one template's capability opens stays closed to the
// other's.
func TestHiddenIs404AndClosedIs403(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	colleague, partner, owner := f.Colleague(), f.Partner(), f.Owner()

	for _, hidden := range []string{NoteSarah, ContactClassed, NoteCoMention} {
		for _, p := range f.Restricted() {
			r := fetch(t, f, p, "/objects/"+hidden)
			assert.Equal(t, http.StatusNotFound, r.Status, "%s GET %s: %s", p.Name, hidden, r.Body)
			assert.Contains(t, string(r.Body), httpserver.CodeNotFound)
		}
		assert.Equal(t, http.StatusOK, fetch(t, f, owner, "/objects/"+hidden).Status, "owner GET %s", hidden)
	}
	// The personal-tagged contact: hidden from the colleague's filter,
	// inside the partner's.
	assert.Equal(t, http.StatusNotFound, fetch(t, f, colleague, "/objects/"+ContactPersonalTag).Status)
	assert.Equal(t, http.StatusOK, fetch(t, f, partner, "/objects/"+ContactPersonalTag).Status)
	// The entity only hidden notes mention is absent; the one the
	// visible contact mentions is there.
	for _, p := range f.Restricted() {
		assert.Equal(t, http.StatusNotFound, fetch(t, f, p, "/entities/"+EntitySecret).Status, p.Name)
		assert.Equal(t, http.StatusOK, fetch(t, f, p, "/entities/"+EntitySarah).Status, p.Name)
		backlinks := fetch(t, f, p, "/entities/"+EntitySarah+"/backlinks")
		assert.Equal(t, http.StatusOK, backlinks.Status, p.Name)
		assert.NotContains(t, string(backlinks.Body), NoteSarah, p.Name)
	}

	for _, path := range []string{"/jobs", "/jobs/" + f.JobID, "/inbox", "/inbox/queue", "/capture/recent", "/events",
		"/pipelines", "/watches", "/pages", "/topic-index", "/search?q=type==contact"} {
		for _, p := range f.Restricted() {
			ctx, cancel := context.WithTimeout(context.Background(), StreamWindow)
			r, err := f.HTTP(ctx, p, http.MethodGet, path, nil)
			cancel()
			require.NoError(t, err)
			assert.Equal(t, http.StatusForbidden, r.Status, "%s GET %s: %s", p.Name, path, r.Body)
			assert.Contains(t, string(r.Body), httpserver.CodeNotShared, "%s GET %s", p.Name, path)
		}
	}
	// The graph search mode: the partner's template opens it, the
	// colleague's does not.
	assert.Equal(t, http.StatusForbidden, fetch(t, f, colleague, "/search/graph?q=sarah").Status)
	assert.Equal(t, http.StatusOK, fetch(t, f, partner, "/search/graph?q=sarah").Status)
}

// The same content captured under another profile is its own object:
// the owner lists both copies, the partner (every contact) both, the
// colleague only the work-tagged one, and the copy a viewer sees
// carries only its own tags.
func TestDedupStaysInsideAProfile(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	objects := func(p Principal) map[string][]string {
		r := fetch(t, f, p, "/objects?limit=100")
		require.Equal(t, http.StatusOK, r.Status, "%s: %s", p.Name, r.Body)
		var out struct {
			Data []storage.KnowledgeObject `json:"data"`
		}
		require.NoError(t, json.Unmarshal(r.Body, &out))
		tags := map[string][]string{}
		for _, o := range out.Data {
			for _, tag := range o.Tags {
				tags[o.ID] = append(tags[o.ID], tag.Label)
			}
			if len(o.Tags) == 0 {
				tags[o.ID] = nil
			}
		}
		return tags
	}
	owner := objects(f.Owner())
	assert.Contains(t, owner, ContactSarah)
	assert.Contains(t, owner, ContactSarahDup)
	assert.Equal(t, []string{"work"}, owner[ContactSarah])
	assert.Equal(t, []string{"personal"}, owner[ContactSarahDup], "the other profile's copy kept its own tags")

	colleague := objects(f.Colleague())
	assert.Contains(t, colleague, ContactSarah)
	assert.NotContains(t, colleague, ContactSarahDup)
	assert.Equal(t, []string{"work"}, colleague[ContactSarah])

	partner := objects(f.Partner())
	assert.Contains(t, partner, ContactSarah)
	assert.Contains(t, partner, ContactSarahDup)
}

// Revoking the colleague's grant takes effect on the next request:
// nothing listed, the visible contact gone, no restart, no cache
// flush.
func TestRevokedGrantSeesNothingOnTheNextRequest(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	colleague := f.Colleague()
	ctx := context.Background()
	st := f.Server.Driver.Sharing()

	before := fetch(t, f, colleague, "/objects")
	require.Equal(t, http.StatusOK, before.Status)
	require.Contains(t, string(before.Body), ContactSarah)
	require.Equal(t, http.StatusOK, fetch(t, f, colleague, "/objects/"+ContactSarah).Status)

	audience, err := st.Audiences().GetByName(ctx, sharing.TemplateColleagues)
	require.NoError(t, err)
	grants, err := st.Grants().List(ctx, storage.GrantFilter{AudienceID: audience.ID})
	require.NoError(t, err)
	require.NotEmpty(t, grants)
	for _, g := range grants {
		require.NoError(t, st.Grants().Revoke(ctx, g.ID, time.Now()))
	}

	after := fetch(t, f, colleague, "/objects")
	require.Equal(t, http.StatusOK, after.Status, "%s", after.Body)
	var out struct {
		Data  []json.RawMessage `json:"data"`
		Total int               `json:"total"`
	}
	require.NoError(t, json.Unmarshal(after.Body, &out))
	assert.Empty(t, out.Data)
	assert.Zero(t, out.Total)
	assert.Equal(t, http.StatusNotFound, fetch(t, f, colleague, "/objects/"+ContactSarah).Status)
	// The partner's grant is untouched.
	assert.Contains(t, string(fetch(t, f, f.Partner(), "/objects").Body), ContactSarah)
}
