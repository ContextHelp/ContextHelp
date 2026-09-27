package dpkmsclient_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// startObjects serves a protected in-process dpkms with three objects:
// o-1 and o-2 tagged "tmp", o-3 tagged "keep".
func startObjects(t *testing.T) *dpkmstest.Server {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	srv := dpkmstest.Start(t, driver, dpkmstest.WithStaticTokens())
	now := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	for i, tag := range []string{"tmp", "tmp", "keep"} {
		id := []string{"o-1", "o-2", "o-3"}[i]
		if err := driver.Objects().Create(context.Background(), &storage.KnowledgeObject{
			ID: id, Type: "note", Status: "active", RawContent: "body " + id,
			Summaries: []string{"summary " + id},
			Tags:      []storage.Tag{{Label: tag}},
			CreatedAt: now.Add(time.Duration(i) * time.Minute), UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return srv
}

func strp(s string) *string { return &s }

func TestObjects_GetAndList(t *testing.T) {
	srv := startObjects(t)
	c := roleClient(t, srv, dpkmstest.RoleReader)
	ctx := context.Background()

	obj, err := c.GetObject(ctx, "o-2")
	if err != nil || obj.ID != "o-2" {
		t.Fatalf("GetObject: %+v %v", obj, err)
	}
	_, err = c.GetObject(ctx, "o-missing")
	if e := envelope(t, err); e.ExitCode != 3 {
		t.Fatalf("missing object: exit %d, want 3 (%v)", e.ExitCode, err)
	}

	objs, total, err := c.ListObjects(ctx, dpkmsclient.ObjectFilter{Tag: "tmp"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, o := range objs {
		ids = append(ids, o.ID)
	}
	slices.Sort(ids)
	if total != 2 || !slices.Equal(ids, []string{"o-1", "o-2"}) {
		t.Fatalf("tag=tmp: total %d ids %v", total, ids)
	}

	// Limit 0 is every match; a positive limit pages.
	all, total, err := c.ListObjects(ctx, dpkmsclient.ObjectFilter{})
	if err != nil || total != 3 || len(all) != 3 {
		t.Fatalf("every match: %d of %d, %v", len(all), total, err)
	}
	page, total, err := c.ListObjects(ctx, dpkmsclient.ObjectFilter{Limit: 1, Offset: 1, Sort: "created_at", Dir: "asc"})
	if err != nil || total != 3 || len(page) != 1 || page[0].ID != "o-2" {
		t.Fatalf("page: %+v of %d, %v", page, total, err)
	}

	_, _, err = c.ListObjects(ctx, dpkmsclient.ObjectFilter{Status: "bogus"})
	if e := envelope(t, err); e.ExitCode != 2 {
		t.Fatalf("bad status: exit %d, want 2", e.ExitCode)
	}
}

func TestObjects_UpdateObject(t *testing.T) {
	srv := startObjects(t)
	ctx := context.Background()
	tags := []string{"ux", "design"}
	mentions := []string{"@ui.best-practice"}
	patch := dpkmsclient.ObjectPatch{
		Subtype: strp("article"), Title: strp("New title"), Tags: &tags, Mentions: &mentions,
	}

	obj, err := roleClient(t, srv, dpkmstest.RoleWriter).UpdateObject(ctx, "o-1", patch)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Subtype != "article" || !slices.Equal(obj.Summaries, []string{"New title"}) ||
		len(obj.Tags) != 2 || len(obj.Mentions) != 1 || obj.Mentions[0].String() != "ctxt://entity/ui/best-practice" {
		t.Fatalf("updated object = %+v", obj)
	}
	stored, err := srv.Driver.Objects().Get(ctx, "o-1")
	if err != nil || stored.Subtype != "article" {
		t.Fatalf("stored = %+v, %v", stored, err)
	}

	_, err = roleClient(t, srv, dpkmstest.RoleReader).UpdateObject(ctx, "o-1", patch)
	if e := envelope(t, err); e.ExitCode != 5 {
		t.Fatalf("reader: exit %d, want 5", e.ExitCode)
	}
	_, err = roleClient(t, srv, dpkmstest.RoleWriter).UpdateObject(ctx, "o-1", dpkmsclient.ObjectPatch{Title: strp("a"), Summary: strp("b")})
	if e := envelope(t, err); e.ExitCode != 2 {
		t.Fatalf("title with summary: exit %d, want 2", e.ExitCode)
	}
	_, err = roleClient(t, srv, dpkmstest.RoleWriter).UpdateObject(ctx, "o-missing", patch)
	if e := envelope(t, err); e.ExitCode != 3 {
		t.Fatalf("missing: exit %d, want 3", e.ExitCode)
	}
}

func TestObjects_DeleteObject(t *testing.T) {
	srv := startObjects(t)
	ctx := context.Background()

	err := roleClient(t, srv, dpkmstest.RoleWriter).DeleteObject(ctx, "o-1")
	var re *dpkmsclient.RemoteError
	if e := envelope(t, err); e.ExitCode != 5 || !errors.As(err, &re) || re.StatusCode != http.StatusForbidden {
		t.Fatalf("writer delete: exit %d, %v", e.ExitCode, err)
	}
	if _, err := srv.Driver.Objects().Get(ctx, "o-1"); err != nil {
		t.Fatalf("writer delete removed the object: %v", err)
	}

	admin := roleClient(t, srv, dpkmstest.RoleAdmin)
	if err := admin.DeleteObject(ctx, "o-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Driver.Objects().Get(ctx, "o-1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if e := envelope(t, admin.DeleteObject(ctx, "o-1")); e.ExitCode != 3 {
		t.Fatalf("second delete: exit %d, want 3", e.ExitCode)
	}
}

func TestObjects_ReprocessObject(t *testing.T) {
	srv := startObjects(t)
	ctx := context.Background()
	writer := roleClient(t, srv, dpkmstest.RoleWriter)

	jobID, err := writer.ReprocessObject(ctx, "o-1", "tagger")
	if err != nil || jobID == "" {
		t.Fatalf("reprocess: %q %v", jobID, err)
	}
	job, err := srv.Driver.Jobs().Get(ctx, jobID)
	if err != nil || job.Type != "object:reprocess" || job.Status != storage.JobPending {
		t.Fatalf("job = %+v, %v", job, err)
	}

	_, err = writer.ReprocessObject(ctx, "o-1", "summarizer")
	if e := envelope(t, err); e.ExitCode != 2 {
		t.Fatalf("unknown step: exit %d, want 2", e.ExitCode)
	}
	_, err = roleClient(t, srv, dpkmstest.RoleReader).ReprocessObject(ctx, "o-1", "tagger")
	if e := envelope(t, err); e.ExitCode != 5 {
		t.Fatalf("reader: exit %d, want 5", e.ExitCode)
	}
}

// TestObjectPatchWireParity keeps the client's patch in step with the
// patch the dpkms handler decodes, field for field.
func TestObjectPatchWireParity(t *testing.T) {
	client := jsonFields(reflect.TypeFor[dpkmsclient.ObjectPatch]())
	server := jsonFields(reflect.TypeFor[service.ObjectPatch]())
	if !reflect.DeepEqual(client, server) {
		t.Fatalf("wire fields differ\nclient: %v\nserver: %v", client, server)
	}
}
