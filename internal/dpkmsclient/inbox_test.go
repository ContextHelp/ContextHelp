package dpkmsclient_test

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func inboxFixture(t *testing.T) *dpkmstest.Server {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, o := range []*storage.KnowledgeObject{
		{ID: "i-old", Type: "text", Status: "inbox", RawContent: "old", CreatedAt: at("2026-04-10T09:00:00Z"), UpdatedAt: at("2026-04-10T09:00:00Z")},
		{ID: "i-new", Type: "text", Status: "inbox", RawContent: "new", CreatedAt: at("2026-04-20T09:00:00Z"), UpdatedAt: at("2026-04-20T09:00:00Z")},
		{ID: "r-1", Type: "note", Status: "raw", CreatedAt: at("2026-04-11T09:00:00Z"), UpdatedAt: at("2026-04-11T09:00:00Z")},
	} {
		if err := driver.Objects().Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	if err := driver.Jobs().Create(ctx, &storage.Job{
		ID: "j-failed", Type: "ingest:url", Status: storage.JobFailed, Pipeline: "url.generic", Error: "boom",
		MaxRetries: 3, CreatedAt: at("2026-04-12T09:00:00Z"), UpdatedAt: at("2026-04-12T09:00:00Z"),
	}); err != nil {
		t.Fatal(err)
	}
	return dpkmstest.Start(t, driver, dpkmstest.WithStaticTokens())
}

func roleClient(t *testing.T, srv *dpkmstest.Server, role string) *dpkmsclient.Client {
	t.Helper()
	return newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: srv.Token(role)})
}

func objectStatus(t *testing.T, srv *dpkmstest.Server, id string) string {
	t.Helper()
	obj, err := srv.Driver.Objects().Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return obj.Status
}

func TestListInbox(t *testing.T) {
	srv := inboxFixture(t)
	c := roleClient(t, srv, dpkmstest.RoleReader)
	ctx := context.Background()

	objs, total, err := c.ListInbox(ctx, dpkmsclient.InboxListRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(objs) != 2 {
		t.Fatalf("total=%d len=%d", total, len(objs))
	}

	after, _ := time.Parse(time.RFC3339, "2026-04-15T00:00:00Z")
	objs, total, err = c.ListInbox(ctx, dpkmsclient.InboxListRequest{Limit: 10, After: after})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(objs) != 1 || objs[0].ID != "i-new" {
		t.Fatalf("after: total=%d objs=%v", total, objs)
	}
	objs, _, err = c.ListInbox(ctx, dpkmsclient.InboxListRequest{Limit: 10, Before: after})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || objs[0].ID != "i-old" {
		t.Fatalf("before: objs=%v", objs)
	}
}

func TestListInboxQueue(t *testing.T) {
	srv := inboxFixture(t)
	c := roleClient(t, srv, dpkmstest.RoleReader)

	items, total, err := c.ListInboxQueue(context.Background(), dpkmsclient.InboxQueueRequest{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	slices.Sort(ids)
	if total != 2 || !slices.Equal(ids, []string{"j-failed", "r-1"}) {
		t.Fatalf("total=%d ids=%v", total, ids)
	}

	items, _, err = c.ListInboxQueue(context.Background(), dpkmsclient.InboxQueueRequest{Failed: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("failed: %+v", items)
	}
	got := items[0]
	if got.ID != "j-failed" || got.Kind != "job" || got.Status != "failed" || got.Type != "url" ||
		got.Pipeline != "url.generic" || got.Error != "boom" || got.CreatedAt.IsZero() {
		t.Fatalf("failed item: %+v", got)
	}
}

func TestTriageDiscardClearInbox(t *testing.T) {
	srv := inboxFixture(t)
	c := roleClient(t, srv, dpkmstest.RoleAdmin)
	ctx := context.Background()

	jobID, err := c.TriageInbox(ctx, "i-old", "text.short")
	if err != nil {
		t.Fatal(err)
	}
	job, err := srv.Driver.Jobs().Get(ctx, jobID)
	if err != nil {
		t.Fatalf("triaged job %q: %v", jobID, err)
	}
	if job.Pipeline != "text.short" || objectStatus(t, srv, "i-old") != "active" {
		t.Fatalf("triage: pipeline=%q status=%q", job.Pipeline, objectStatus(t, srv, "i-old"))
	}

	if err := c.DiscardInbox(ctx, "i-new"); err != nil {
		t.Fatal(err)
	}
	if s := objectStatus(t, srv, "i-new"); s != "discarded" {
		t.Fatalf("discard: status=%q", s)
	}

	n, err := c.ClearInbox(ctx)
	if err != nil || n != 0 {
		t.Fatalf("clear on an empty inbox: n=%d err=%v", n, err)
	}
}

func TestClearInboxCount(t *testing.T) {
	srv := inboxFixture(t)
	n, err := roleClient(t, srv, dpkmstest.RoleAdmin).ClearInbox(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if s := objectStatus(t, srv, "r-1"); s != "raw" {
		t.Fatalf("clear touched a raw object: %q", s)
	}
}

// Processing the inbox needs process:inbox: a writer or reader token is
// refused with exit 5 and nothing changes. A missing item is exit 3.
func TestInboxProcessingExitCodes(t *testing.T) {
	srv := inboxFixture(t)
	ctx := context.Background()
	for _, role := range []string{dpkmstest.RoleWriter, dpkmstest.RoleReader} {
		c := roleClient(t, srv, role)
		_, err := c.TriageInbox(ctx, "i-old", "")
		if e := envelope(t, err); e.ExitCode != output.ExitUnauthorized {
			t.Errorf("%s triage: exit %d", role, e.ExitCode)
		}
		if e := envelope(t, c.DiscardInbox(ctx, "i-old")); e.ExitCode != output.ExitUnauthorized {
			t.Errorf("%s discard: exit %d", role, e.ExitCode)
		}
		_, err = c.ClearInbox(ctx)
		if e := envelope(t, err); e.ExitCode != output.ExitUnauthorized {
			t.Errorf("%s clear: exit %d", role, e.ExitCode)
		}
	}
	if s := objectStatus(t, srv, "i-old"); s != "inbox" {
		t.Fatalf("a refused request changed the item: %q", s)
	}

	anon := newClient(t, dpkmsclient.Endpoint{URL: srv.URL})
	_, _, err := anon.ListInboxQueue(ctx, dpkmsclient.InboxQueueRequest{})
	if e := envelope(t, err); e.ExitCode != output.ExitUnauthorized {
		t.Errorf("no token: exit %d", e.ExitCode)
	}

	admin := roleClient(t, srv, dpkmstest.RoleAdmin)
	if e := envelope(t, admin.DiscardInbox(ctx, "missing")); e.ExitCode != output.ExitNotFound {
		t.Errorf("discard missing: exit %d", e.ExitCode)
	}
	if _, err := admin.TriageInbox(ctx, "missing", ""); envelope(t, err).ExitCode != output.ExitNotFound {
		t.Errorf("triage missing: exit %d", envelope(t, err).ExitCode)
	}
}

// TestInboxQueueItemWireParity keeps the client's queue item in step with
// the item the dpkms handler encodes.
func TestInboxQueueItemWireParity(t *testing.T) {
	client := jsonFields(reflect.TypeFor[dpkmsclient.InboxQueueItem]())
	server := jsonFields(reflect.TypeFor[service.InboxQueueItem]())
	if !reflect.DeepEqual(client, server) {
		t.Fatalf("wire fields differ\nclient: %v\nserver: %v", client, server)
	}
}
