package steward_test

import (
	"context"
	"slices"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// seedCorpus writes the objects the scopes select from:
//
//	c1 contact, tag work
//	c2 contact, tag family
//	c3 contact, tag client, class personal
//	c4 contact, tag work, class financial
//	n1 note, tag work
func (f *fixture) seedCorpus() {
	f.t.Helper()
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	for _, o := range []struct {
		name, typ, tag string
		classes        []string
	}{
		{"c1", "contact", "work", nil}, {"c2", "contact", "family", nil},
		{"c3", "contact", "client", []string{"personal"}}, {"c4", "contact", "work", []string{"financial"}},
		{"n1", "note", "work", nil},
	} {
		obj := &storage.KnowledgeObject{
			ID: "obj-" + o.name, Type: o.typ, RawContent: o.name + " body", Status: storage.ObjectStatusActive,
			Tags: []storage.Tag{{Label: o.tag}}, Classes: o.classes, ContentHash: "sha256:" + o.name,
			Metadata: map[string]any{}, CreatedAt: at, UpdatedAt: at,
		}
		require.NoError(f.t, f.drv.Objects().Create(context.Background(), obj))
	}
}

// visible runs v's SQLite predicate and returns the matched object
// IDs, sorted.
func (f *fixture) visible(v sharing.Viewer) []string {
	f.t.Helper()
	sql, args := v.Predicate(search.DialectFor(f.drv))
	objs, _, err := f.drv.Objects().ListBySQL(context.Background(), sql, args, storage.ObjectFilter{Status: storage.ObjectStatusAll})
	require.NoError(f.t, err)
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.ID
	}
	slices.Sort(out)
	return out
}

// want names objects by their corpus name.
func want(names ...string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "obj-" + n
	}
	slices.Sort(out)
	return out
}

// elevated resolves the steward's viewer under grant g.
func (f *fixture) elevated(g *storage.StewardGrant) (sharing.Viewer, error) {
	return f.viewers.Resolve(context.Background(), f.stewardP.ID, storage.PrincipalKindPerson, "", sharing.AsSteward(g.ID))
}
