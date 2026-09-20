package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/require"
	uri "hop.top/cite/scheme"
)

// makeObjectWithMentions builds a minimal KnowledgeObject carrying the given
// mention URIs. Mirrors makeObject() but lets the caller set Mentions directly.
func makeObjectWithMentions(id string, ms []uri.URI) *storage.KnowledgeObject {
	now := time.Now().Truncate(time.Second)
	content := "test content for " + id
	return &storage.KnowledgeObject{
		ID:         id,
		Type:       "article",
		Subtype:    "short",
		RawContent: content,
		Mentions:   ms,
		Summaries:  []string{content},
		Sections: []storage.Section{{
			Title:   "Body",
			Content: content,
			Order:   0,
		}},
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeSummary, 0),
					NodeType: pluginapi.NodeTypeSummary,
					Label:    "Summary",
					Content:  content,
					Order:    0,
				},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// mentionURI builds a ctxt://entity/<ns>/<slug> URI from a "@ns.slug" form.
func mentionURI(t *testing.T, s string) uri.URI {
	t.Helper()
	// Use the same parser the storage layer uses on read-back.
	// Avoid pulling in the mentions package directly to keep the test
	// focused on storage behaviour.
	switch s {
	case "@foo":
		return uri.URI{Scheme: "ctxt", Namespace: "entity", ID: "foo"}
	case "@bar":
		return uri.URI{Scheme: "ctxt", Namespace: "entity", ID: "bar"}
	case "@baz":
		return uri.URI{Scheme: "ctxt", Namespace: "entity", ID: "baz"}
	case "@client.acme":
		return uri.URI{Scheme: "ctxt", Namespace: "entity", ID: "client/acme"}
	}
	t.Fatalf("unknown test mention shorthand: %q", s)
	return uri.URI{}
}

func TestListObjects_FilterByMention_SingleMention(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	mFoo := mentionURI(t, "@foo")
	mBar := mentionURI(t, "@bar")

	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-foo", []uri.URI{mFoo})))
	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-bar", []uri.URI{mBar})))
	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-both", []uri.URI{mFoo, mBar})))

	got, total, err := d.Objects().List(ctx, storage.ObjectFilter{
		Mention: "@foo",
	})
	require.NoError(t, err)
	require.Equal(t, 2, total, "total should count obj-foo + obj-both")
	require.Len(t, got, 2)

	ids := map[string]bool{}
	for _, o := range got {
		ids[o.ID] = true
	}
	require.True(t, ids["obj-foo"], "obj-foo should match")
	require.True(t, ids["obj-both"], "obj-both should match")
	require.False(t, ids["obj-bar"], "obj-bar must not match")
}

func TestListObjects_FilterByMention_AcceptsURIForm(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	mFoo := mentionURI(t, "@foo")
	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-foo", []uri.URI{mFoo})))

	// CLI users may pass the canonical URI directly.
	got, _, err := d.Objects().List(ctx, storage.ObjectFilter{
		Mention: "ctxt://entity/foo",
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "obj-foo", got[0].ID)
}

func TestListObjects_FilterByMention_NamespacedSlug(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	mClient := mentionURI(t, "@client.acme")
	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-client", []uri.URI{mClient})))
	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-noclient", []uri.URI{mentionURI(t, "@foo")})))

	got, total, err := d.Objects().List(ctx, storage.ObjectFilter{
		Mention: "@client.acme",
	})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, got, 1)
	require.Equal(t, "obj-client", got[0].ID)
}

func TestListObjects_FilterByMention_ExcludesNonMatching(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-bar", []uri.URI{mentionURI(t, "@bar")})))
	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-baz", []uri.URI{mentionURI(t, "@baz")})))

	got, total, err := d.Objects().List(ctx, storage.ObjectFilter{
		Mention: "@foo",
	})
	require.NoError(t, err)
	require.Equal(t, 0, total)
	require.Len(t, got, 0)
}

func TestListObjects_FilterByMention_NoMentionsBehaviorUnchanged(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-1", []uri.URI{mentionURI(t, "@foo")})))
	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-2", []uri.URI{mentionURI(t, "@bar")})))
	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-3", nil)))

	// Empty Mention filter: list all (current default behaviour).
	got, total, err := d.Objects().List(ctx, storage.ObjectFilter{})
	require.NoError(t, err)
	require.Equal(t, 3, total, "empty filter still lists everything")
	require.Len(t, got, 3)
}

func TestListObjects_FilterByMention_EmptyMentionsRowExcluded(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-empty", nil)))
	require.NoError(t, d.Objects().Create(ctx,
		makeObjectWithMentions("obj-foo", []uri.URI{mentionURI(t, "@foo")})))

	got, _, err := d.Objects().List(ctx, storage.ObjectFilter{Mention: "@foo"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "obj-foo", got[0].ID)
}
