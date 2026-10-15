package dpkmsclient_test

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// pageFixture seeds a source object with topics and a reminder, the
// entity page person.alice built from it, two jobs, an entity, a feed
// and one resurfacing candidate for profile "work".
func pageFixture(t *testing.T) storage.StorageDriver {
	t.Helper()
	ctx := context.Background()
	drv := storageutil.NewTestDriver(t)
	now := time.Now().UTC().Truncate(time.Second)
	for _, o := range []*storage.KnowledgeObject{
		{
			ID: "s-1", Type: "note", Status: "active", RawContent: "Alice ships Go", Summaries: []string{"Alice ships Go"},
			Metadata: map[string]any{"enrichment.structured_metadata": map[string]any{"topics": []any{"go"}}},
		},
		{
			ID: "p-alice", Type: "entity_page", Status: "active", RawContent: "stale",
			Metadata: map[string]any{"entity_page": map[string]any{
				"entity_slug": "person.alice", "source_ids": []any{"s-1"}, "revision_count": 1,
				"revision_log": []any{map[string]any{"date": now.Format(time.RFC3339), "source_id": "s-1", "delta": "initial page creation"}},
			}},
		},
	} {
		o.CreatedAt, o.UpdatedAt = now, now
		if err := drv.Objects().Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	if err := drv.Objects().SetReminder(ctx, "s-1", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for id, st := range map[string]storage.JobStatus{"j-1": storage.JobPending, "j-2": storage.JobFailed} {
		if err := drv.Jobs().Create(ctx, &storage.Job{ID: id, Type: "analyze", Status: st, MaxRetries: 3, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := drv.Entities().Upsert(ctx, &storage.Entity{Slug: "ui.cart", Title: "Cart", Namespace: "ui", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := drv.Feeds().Create(ctx, &storage.Feed{ID: "f-1", URL: "https://a.example/rss", Status: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := drv.Resurfacing().Upsert(ctx, &storage.ResurfacingEntry{ID: "rq-1", ObjectID: "s-1", ProfileID: "work", Score: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return drv
}

func TestPages_Reader(t *testing.T) {
	srv := dpkmstest.Start(t, pageFixture(t), dpkmstest.WithKeys())
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: srv.Token(dpkmstest.RoleReader)})
	ctx := context.Background()

	pages, err := c.ListPages(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].ID != "p-alice" {
		t.Fatalf("ListPages = %+v", pages)
	}
	meta, ok := dpkmsclient.PageMeta(pages[0])
	if !ok || meta.EntitySlug != "person.alice" || !slices.Equal(meta.SourceIDs, []string{"s-1"}) ||
		meta.RevisionCount != 1 || len(meta.RevisionLog) != 1 || meta.RevisionLog[0].SourceID != "s-1" {
		t.Fatalf("PageMeta = %+v, %v", meta, ok)
	}
	if _, ok := dpkmsclient.PageMeta(&storage.KnowledgeObject{}); ok {
		t.Error("PageMeta found meta on a plain object")
	}

	page, err := c.GetPage(ctx, "person.alice")
	if err != nil || page.ID != "p-alice" {
		t.Fatalf("GetPage = %+v, %v", page, err)
	}
	_, err = c.GetPage(ctx, "no.such")
	if e := envelope(t, err); e.ExitCode != 3 {
		t.Fatalf("GetPage(no.such) exit %d: %v", e.ExitCode, err)
	}

	// A reader may not refresh: UNAUTHORIZED (exit 5), nothing written.
	_, err = c.RefreshPage(ctx, "person.alice")
	if e := envelope(t, err); e.ExitCode != 5 || !strings.Contains(err.Error(), "write:objects") {
		t.Fatalf("RefreshPage as reader: %v", err)
	}
	if page, _ := c.GetPage(ctx, "person.alice"); page.RawContent != "stale" {
		t.Errorf("a refused refresh changed the page: %q", page.RawContent)
	}
}

func TestPages_WriterRefreshes(t *testing.T) {
	srv := dpkmstest.Start(t, pageFixture(t), dpkmstest.WithKeys())
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: srv.Token(dpkmstest.RoleWriter)})
	ctx := context.Background()

	page, err := c.RefreshPage(ctx, "person.alice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.RawContent, "Alice ships Go") {
		t.Fatalf("refreshed content: %q", page.RawContent)
	}
	_, err = c.RefreshPage(ctx, "no.such")
	if e := envelope(t, err); e.ExitCode != 3 {
		t.Fatalf("RefreshPage(no.such) exit %d: %v", e.ExitCode, err)
	}
}

func TestTopicIndex(t *testing.T) {
	srv := dpkmstest.Start(t, pageFixture(t), dpkmstest.WithKeys())
	reader := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: srv.Token(dpkmstest.RoleReader)})
	writer := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: srv.Token(dpkmstest.RoleWriter)})
	ctx := context.Background()

	// Not built yet: NOT_FOUND (exit 3).
	_, err := reader.TopicIndex(ctx)
	if e := envelope(t, err); e.ExitCode != 3 {
		t.Fatalf("TopicIndex before a rebuild: %v", err)
	}
	_, err = reader.RebuildTopicIndex(ctx)
	if e := envelope(t, err); e.ExitCode != 5 {
		t.Fatalf("RebuildTopicIndex as reader: %v", err)
	}

	id, err := writer.RebuildTopicIndex(ctx)
	if err != nil || id == "" {
		t.Fatalf("RebuildTopicIndex = %q, %v", id, err)
	}
	idx, err := reader.TopicIndex(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Categories) != 1 || idx.Categories[0].Topic != "go" || !slices.Equal(idx.ObjectIDs, []string{"s-1"}) ||
		idx.GeneratedAt.IsZero() {
		t.Fatalf("TopicIndex = %+v", idx)
	}
	entries, err := reader.TopicEntries(ctx, "G")
	if err != nil || len(entries) != 1 || entries[0].ObjectID != "s-1" || entries[0].Summary != "Alice ships Go" {
		t.Fatalf("TopicEntries(G) = %+v, %v", entries, err)
	}
	entries, err = reader.TopicEntries(ctx, "rust")
	if err != nil || entries == nil || len(entries) != 0 {
		t.Fatalf("TopicEntries(rust) = %#v, %v", entries, err)
	}
}

func TestStats(t *testing.T) {
	srv := dpkmstest.Start(t, pageFixture(t), dpkmstest.WithKeys())
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: srv.Token(dpkmstest.RoleReader)})
	ctx := context.Background()

	st, err := c.Stats(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	one := 1
	want := &dpkmsclient.Stats{
		KnowledgeObjects: 2,
		ObjectsByType:    map[string]int{"note": 1, "entity_page": 1},
		Jobs:             dpkmsclient.JobCounts{Total: 2, Pending: 1, Failed: 1},
		Entities:         1, Feeds: 1, ActiveFeeds: 1, Reminders: 1, Resurfacing: &one,
	}
	if !reflect.DeepEqual(st, want) {
		t.Fatalf("Stats = %+v\nwant %+v", st, want)
	}
	st, err = c.Stats(ctx, "home")
	if err != nil || st.Resurfacing == nil || *st.Resurfacing != 0 {
		t.Fatalf("Stats(home) = %+v, %v", st, err)
	}

	anon := newClient(t, dpkmsclient.Endpoint{URL: srv.URL})
	_, err = anon.Stats(ctx, "")
	if e := envelope(t, err); e.ExitCode != 5 {
		t.Fatalf("Stats without a token: %v", err)
	}
}

// The client's page, topic-index and stats types keep the wire shape of
// the service types the handlers encode.
func TestPagesIndexStatsWireParity(t *testing.T) {
	for _, pair := range []struct{ client, server reflect.Type }{
		{reflect.TypeFor[dpkmsclient.EntityPageMeta](), reflect.TypeFor[service.EntityPageMeta]()},
		{reflect.TypeFor[dpkmsclient.TopicIndex](), reflect.TypeFor[service.TopicIndex]()},
		{reflect.TypeFor[dpkmsclient.TopicEntry](), reflect.TypeFor[service.TopicEntry]()},
		{reflect.TypeFor[dpkmsclient.Stats](), reflect.TypeFor[service.Stats]()},
	} {
		client, server := wireShape(pair.client), wireShape(pair.server)
		if !reflect.DeepEqual(client, server) {
			t.Errorf("%s vs %s: wire shapes differ\nclient: %v\nserver: %v", pair.client, pair.server, client, server)
		}
	}
}
