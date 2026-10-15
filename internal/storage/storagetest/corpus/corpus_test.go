package corpus_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest/corpus"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func build(t *testing.T, spec corpus.Spec) (storage.StorageDriver, *corpus.Corpus) {
	t.Helper()
	drv := storageutil.NewTestDriver(t)
	c, err := corpus.Build(context.Background(), drv, spec)
	require.NoError(t, err)
	return drv, c
}

// everything lists every object, whatever its status, ordered by ID.
func everything(t *testing.T, drv storage.StorageDriver) []*storage.KnowledgeObject {
	t.Helper()
	objs, total, err := drv.Objects().List(context.Background(), storage.ObjectFilter{Status: "all"})
	require.NoError(t, err)
	require.Len(t, objs, total)
	slices.SortFunc(objs, func(a, b *storage.KnowledgeObject) int { return strings.Compare(a.ID, b.ID) })
	return objs
}

func edgeIDs(edges []*storage.Edge) []string {
	ids := make([]string, len(edges))
	for i, e := range edges {
		ids[i] = e.ID
	}
	slices.Sort(ids)
	return ids
}

// One seed is one corpus: two fresh stores get the same IDs, text,
// timestamps, metadata, tags, mentions and edges. Another seed gets
// another corpus.
func TestBuild_Deterministic(t *testing.T) {
	ctx := context.Background()
	spec := corpus.Spec{Objects: 300, Seed: 7}
	d1, c1 := build(t, spec)
	d2, c2 := build(t, spec)

	assert.Equal(t, c1.Contacts, c2.Contacts)
	assert.Equal(t, c1.NoteIDs, c2.NoteIDs)
	assert.Equal(t, c1.Edges, c2.Edges)

	o1, o2 := everything(t, d1), everything(t, d2)
	require.Len(t, o2, len(o1))
	for i := range o1 {
		a, b := o1[i], o2[i]
		require.Equal(t, a.ID, b.ID)
		assert.Equal(t, a.Type, b.Type, a.ID)
		assert.Equal(t, a.RawContent, b.RawContent, a.ID)
		assert.Equal(t, a.Summaries, b.Summaries, a.ID)
		assert.Equal(t, a.Metadata, b.Metadata, a.ID)
		assert.Equal(t, a.Tags, b.Tags, a.ID)
		assert.Equal(t, a.Mentions, b.Mentions, a.ID)
		assert.Equal(t, a.ProfileID, b.ProfileID, a.ID)
		assert.Equal(t, a.ContentHash, b.ContentHash, a.ID)
		assert.True(t, a.CreatedAt.Equal(b.CreatedAt), "%s created at %s and %s", a.ID, a.CreatedAt, b.CreatedAt)
	}
	for _, id := range c1.NoteIDs[:10] {
		e1, err := d1.Edges().ListFrom(ctx, "object", id)
		require.NoError(t, err)
		e2, err := d2.Edges().ListFrom(ctx, "object", id)
		require.NoError(t, err)
		require.NotEmpty(t, e1, id)
		assert.Equal(t, edgeIDs(e1), edgeIDs(e2), id)
	}

	_, c3 := build(t, corpus.Spec{Objects: 300, Seed: 8})
	assert.NotEqual(t, c1.Contacts[0].ID, c3.Contacts[0].ID)
	assert.NotEqual(t, c1.NoteIDs, c3.NoteIDs)
}

// A tenth of the corpus is contacts and the rest notes, spread over the
// profiles and tags. A contact's card carries its email and phone in the
// metadata only, its text is the rendered card, a third carry a personal
// note as a classed section node, as contact ingest stores them; each
// names its entity, and that entity exists.
func TestBuild_Shape(t *testing.T) {
	ctx := context.Background()
	d, c := build(t, corpus.Spec{Objects: 500, Seed: 1})
	require.Len(t, c.Contacts, 50)
	require.Len(t, c.NoteIDs, 450)
	assert.Equal(t, 500, c.Spec.Objects)
	assert.Positive(t, c.BuildTime)

	objs := everything(t, d)
	require.Len(t, objs, 500)
	byID := make(map[string]*storage.KnowledgeObject, len(objs))
	profiles, tags, types := map[string]int{}, map[string]int{}, map[string]int{}
	for _, o := range objs {
		byID[o.ID] = o
		profiles[o.ProfileID]++
		types[o.Type]++
		for _, tg := range o.Tags {
			tags[tg.Label]++
		}
		assert.Equal(t, storage.ObjectStatusActive, o.Status, o.ID)
		assert.Equal(t, corpus.Source, o.Source, o.ID)
		assert.NotEmpty(t, o.Tags, o.ID)
		assert.NotEmpty(t, o.Summaries, o.ID)
		assert.NotEmpty(t, o.ContentHash, o.ID)
		assert.NotEmpty(t, o.Mentions, o.ID)
	}
	assert.Equal(t, map[string]int{corpus.TypeContact: 50, corpus.TypeNote: 450}, types)
	for _, p := range corpus.Profiles {
		assert.Positive(t, profiles[p], p)
	}
	assert.GreaterOrEqual(t, len(tags), 4)

	notes := 0
	for _, ct := range c.Contacts {
		o := byID[ct.ID]
		require.NotNil(t, o, ct.ID)
		assert.Equal(t, corpus.TypeContact, o.Type)
		for k, want := range map[string]string{
			"name": ct.Name, "email": ct.Email, "phone": ct.Phone, "org": ct.Org, "role": ct.Role, "city": ct.City,
		} {
			assert.Equal(t, want, o.Metadata[k], "%s %s", ct.ID, k)
		}
		for _, s := range []string{ct.Name, ct.Role, ct.Org, ct.City} {
			assert.Contains(t, o.RawContent, s, ct.ID)
		}
		for _, s := range []string{ct.Email, ct.Phone, ct.Note} {
			if s != "" {
				assert.NotContains(t, o.RawContent, s, ct.ID)
			}
		}
		assert.Equal(t, o.RawContent, o.TextContent, ct.ID)
		require.NotNil(t, o.Graph, ct.ID)
		require.Equal(t, storage.NodeTypeSection, o.Graph.Nodes[0].NodeType, ct.ID)
		assert.Equal(t, o.RawContent, o.Graph.Nodes[0].Content, ct.ID)
		assert.Nil(t, storage.NodeClasses(o.Graph.Nodes[0]), ct.ID)
		if ct.Note == "" {
			assert.Len(t, o.Graph.Nodes, 1, ct.ID)
		} else {
			notes++
			require.Len(t, o.Graph.Nodes, 2, ct.ID)
			assert.Equal(t, ct.Note, o.Graph.Nodes[1].Content, ct.ID)
			assert.Equal(t, []string{storage.ClassPersonal}, storage.NodeClasses(o.Graph.Nodes[1]), ct.ID)
			assert.True(t, strings.HasPrefix(o.Graph.Nodes[1].ID, ct.ID+"/"), ct.ID)
		}
		assert.Equal(t, []string{ct.Name}, o.Summaries)
		assert.Equal(t, ct.Profile, o.ProfileID)

		u, ok := mentions.Parse(ct.Mention)
		require.True(t, ok, ct.Mention)
		assert.Equal(t, ct.URI, u.String())
		require.Len(t, o.Mentions, 1)
		assert.Equal(t, ct.URI, o.Mentions[0].String())

		ent, err := d.Entities().Get(ctx, ct.Slug)
		require.NoError(t, err, ct.Slug)
		assert.Equal(t, ct.Name, ent.Title)
		assert.Equal(t, corpus.Namespace, ent.Namespace)
	}
	assert.Equal(t, 17, notes, "every third contact carries a note")
}

func assertEdge(t *testing.T, d storage.StorageDriver, fromType, fromID, toType, toID, edgeType string) {
	t.Helper()
	edges, err := d.Edges().ListFrom(context.Background(), fromType, fromID)
	require.NoError(t, err)
	for _, e := range edges {
		if e.ToType == toType && e.ToID == toID && e.EdgeType == edgeType {
			return
		}
	}
	t.Errorf("no %s edge %s/%s -> %s/%s", edgeType, fromType, fromID, toType, toID)
}

// Every note mentions one to three distinct contacts: each is named in
// the text, listed under metadata.people, carried as a mention URI, and
// joined by the mentions and mentioned_in edges the ingest worker writes.
// Contacts mention their own entity the same way. The mention filter and
// the backlink count read those rows.
func TestBuild_MentionEdges(t *testing.T) {
	ctx := context.Background()
	d, c := build(t, corpus.Spec{Objects: 200, Seed: 3})
	byURI := make(map[string]corpus.Contact, len(c.Contacts))
	for _, ct := range c.Contacts {
		byURI[ct.URI] = ct
	}

	total := 0
	for _, id := range c.NoteIDs {
		o, err := d.Objects().Get(ctx, id)
		require.NoError(t, err)
		require.NotEmpty(t, o.Mentions, id)
		require.LessOrEqual(t, len(o.Mentions), 3, id)
		people, _ := o.Metadata["people"].([]any)
		require.Len(t, people, len(o.Mentions), id)
		seen := map[string]bool{}
		for i, m := range o.Mentions {
			u := m.String()
			assert.False(t, seen[u], "%s mentions %s twice", id, u)
			seen[u] = true
			ct, ok := byURI[u]
			require.True(t, ok, "%s mentions unknown %s", id, u)
			assert.Contains(t, o.RawContent, ct.Name, id)
			assert.Equal(t, ct.Mention, people[i], id)
			assertEdge(t, d, "object", id, "entity", u, "mentions")
			assertEdge(t, d, "entity", u, "object", id, "mentioned_in")
		}
		total += len(o.Mentions)
	}
	for _, ct := range c.Contacts {
		assertEdge(t, d, "object", ct.ID, "entity", ct.URI, "mentions")
		assertEdge(t, d, "entity", ct.URI, "object", ct.ID, "mentioned_in")
		total++
	}
	assert.Equal(t, 2*total, c.Edges)

	ct := c.Contacts[0]
	objs, _, err := d.Objects().List(ctx, storage.ObjectFilter{Mention: ct.Mention})
	require.NoError(t, err)
	assert.NotEmpty(t, objs, "nothing mentions %s", ct.Mention)
	n, err := d.Edges().CountMentionsTo(ctx, "entity", ct.URI)
	require.NoError(t, err)
	assert.Equal(t, len(objs), n, "one mentions edge per object mentioning %s", ct.Mention)
}

// Every query has full-text hits, and a spec always yields the same
// queries.
func TestCorpus_Queries(t *testing.T) {
	ctx := context.Background()
	d, c := build(t, corpus.Spec{Objects: 400, Seed: 5})
	qs := c.Queries()
	require.NotEmpty(t, qs)
	for _, q := range qs {
		hits, err := d.Objects().FTSSearch(ctx, q, storage.ObjectFilter{Limit: 5})
		require.NoError(t, err, q)
		assert.NotEmpty(t, hits, q)
	}
	_, c2 := build(t, corpus.Spec{Objects: 400, Seed: 5})
	assert.Equal(t, qs, c2.Queries())
}

// Zero fields take the defaults; a tiny corpus still has a contact for
// its notes to mention.
func TestBuild_Defaults(t *testing.T) {
	d, c := build(t, corpus.Spec{Objects: 10, ContactRatio: 7, EdgeBatch: -1})
	assert.Equal(t, corpus.DefaultContactRatio, c.Spec.ContactRatio)
	assert.Equal(t, corpus.DefaultEdgeBatch, c.Spec.EdgeBatch)
	require.Len(t, c.Contacts, 1)
	require.Len(t, c.NoteIDs, 9)
	for _, o := range everything(t, d) {
		require.Len(t, o.Mentions, 1, o.ID)
		assert.Equal(t, c.Contacts[0].URI, o.Mentions[0].String(), o.ID)
	}

	_, c = build(t, corpus.Spec{Objects: 1})
	assert.Len(t, c.Contacts, 1)
	assert.Empty(t, c.NoteIDs)
	assert.Equal(t, 2, c.Edges)
}

// Progress reports every ten thousand objects and at the end.
func TestBuild_Progress(t *testing.T) {
	var seen [][2]int
	_, c := build(t, corpus.Spec{Objects: 120, Seed: 2, Progress: func(done, total int) {
		seen = append(seen, [2]int{done, total})
	}})
	assert.Equal(t, [][2]int{{120, 120}}, seen)
	assert.Equal(t, 120, c.Spec.Objects)
}
