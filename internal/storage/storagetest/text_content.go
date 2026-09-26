package storagetest

import (
	"context"
	"strconv"
	"testing"

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
