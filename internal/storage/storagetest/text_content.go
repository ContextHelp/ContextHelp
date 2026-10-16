package storagetest

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TextContentDefaultConformance pins how ObjectStore.Create fills an empty
// TextContent: the stored value is projection.BodyText of the input, the
// rule the embedding step applies to a draft before it is persisted. An
// explicit TextContent is never overwritten.
//
// Create applies the default to the object in place, which is what the
// FTS projection reads, and Get reads the stored value back.
func TextContentDefaultConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	cases := []struct {
		name, raw, text, want string
	}{
		{name: "raw only", raw: "raw body", want: "raw body"},
		{name: "explicit text kept", raw: "raw body", text: "extracted text", want: "extracted text"},
		{name: "text only", text: "extracted text", want: "extracted text"},
		{name: "both empty", want: ""},
		{name: "blob ref is not text", raw: "blob://0123abcd", want: ""},
		{name: "blob ref with text", raw: "blob://0123abcd", text: "extracted text", want: "extracted text"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			obj := &storage.KnowledgeObject{
				ID:          "text-default-" + strconv.Itoa(i),
				Type:        "note",
				RawContent:  tc.raw,
				TextContent: tc.text,
				CreatedAt:   fixtureTime(),
				UpdatedAt:   fixtureTime(),
			}
			input := *obj
			if got := projection.BodyText(&input); got != tc.want {
				t.Fatalf("fixture drift: BodyText = %q, want %q", got, tc.want)
			}
			if err := drv.Objects().Create(ctx, obj); err != nil {
				t.Fatalf("Create: %v", err)
			}
			if obj.TextContent != tc.want {
				t.Errorf("Create left TextContent %q on the object, want %q", obj.TextContent, tc.want)
			}
			got, err := drv.Objects().Get(ctx, obj.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.TextContent != tc.want {
				t.Errorf("stored TextContent = %q, want %q", got.TextContent, tc.want)
			}
			if got.RawContent != tc.raw {
				t.Errorf("stored RawContent = %q, want %q", got.RawContent, tc.raw)
			}
		})
	}
}

// BlobRefNotIndexedConformance pins that a blob:// ref (content
// externalized to the blob store) is never indexed: an object stored
// the way externalize_content leaves it is found by its text, on Create
// and Update, and never by the ref's tokens.
func BlobRefNotIndexedConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	const key = "f00dfacecafe"
	search := func(t *testing.T, q string) []string {
		t.Helper()
		res, err := drv.Objects().FTSSearch(ctx, q, storage.ObjectFilter{Limit: 10})
		if err != nil {
			t.Fatalf("FTSSearch %q: %v", q, err)
		}
		ids := make([]string, 0, len(res))
		for _, o := range res {
			ids = append(ids, o.ID)
		}
		return ids
	}
	refOnly := &storage.KnowledgeObject{
		ID: "blobref-bare", Type: "note", RawContent: "blob://" + key,
		Summaries: []string{"wombat gist"}, CreatedAt: fixtureTime(), UpdatedAt: fixtureTime(),
	}
	withText := &storage.KnowledgeObject{
		ID: "blobref-text", Type: "note", RawContent: "blob://" + key,
		TextContent: "quokka marmalade", CreatedAt: fixtureTime(), UpdatedAt: fixtureTime(),
	}
	for _, obj := range []*storage.KnowledgeObject{refOnly, withText} {
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("Create %s: %v", obj.ID, err)
		}
	}
	check := func(t *testing.T) {
		t.Helper()
		if got := search(t, "quokka"); len(got) != 1 || got[0] != withText.ID {
			t.Errorf("text search = %v, want [%s]", got, withText.ID)
		}
		if got := search(t, "wombat"); len(got) != 1 || got[0] != refOnly.ID {
			t.Errorf("summary search = %v, want [%s]", got, refOnly.ID)
		}
		for _, q := range []string{"blob", key} {
			if got := search(t, q); len(got) != 0 {
				t.Errorf("search %q = %v: the blob ref was indexed", q, got)
			}
		}
		for _, id := range []string{refOnly.ID, withText.ID} {
			got, err := drv.Objects().Get(ctx, id)
			if err != nil {
				t.Fatalf("Get %s: %v", id, err)
			}
			if strings.HasPrefix(got.TextContent, "blob://") {
				t.Errorf("%s: stored text_content is the blob ref %q", id, got.TextContent)
			}
		}
	}
	t.Run("create", check)
	t.Run("update", func(t *testing.T) {
		for _, obj := range []*storage.KnowledgeObject{refOnly, withText} {
			obj.UpdatedAt = fixtureTime().Add(time.Hour)
			if err := drv.Objects().Update(ctx, obj); err != nil {
				t.Fatalf("Update %s: %v", obj.ID, err)
			}
		}
		check(t)
	})
}
