package blob

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

func putBlob(t *testing.T, s storage.BlobStore, key, data string) {
	t.Helper()
	if err := s.Put(context.Background(), key, strings.NewReader(data), storage.BlobMeta{}); err != nil {
		t.Fatalf("put: %v", err)
	}
}

func TestContent(t *testing.T) {
	ctx := context.Background()
	s, _ := local.New(t.TempDir())
	putBlob(t, s, "abcd1234", "the whole blob")

	if got, err := Content(ctx, s, "blob://abcd1234"); err != nil || got != "the whole blob" {
		t.Errorf("ref: got %q, %v", got, err)
	}
	if got, err := Content(ctx, s, "inline text"); err != nil || got != "inline text" {
		t.Errorf("inline: got %q, %v", got, err)
	}
	if got, err := Content(ctx, nil, "inline text"); err != nil || got != "inline text" {
		t.Errorf("inline without store: got %q, %v", got, err)
	}
	if _, err := Content(ctx, s, "blob://missing"); err == nil {
		t.Error("missing blob: want error")
	}
	if _, err := Content(ctx, nil, "blob://abcd1234"); err == nil {
		t.Error("ref without store: want error")
	}
}

func TestPreview(t *testing.T) {
	ctx := context.Background()
	s, _ := local.New(t.TempDir())
	putBlob(t, s, "exact000", "12345")
	putBlob(t, s, "longer00", "1234567890")
	putBlob(t, s, "runes000", "abéé") // a b é(2) é(2) = 6 bytes

	cases := []struct {
		content string
		max     int64
		want    string
		cut     bool
	}{
		{"blob://exact000", 5, "12345", false},
		{"blob://longer00", 5, "12345", true},
		{"blob://longer00", 0, "1234567890", false},
		{"blob://runes000", 3, "ab", true}, // never split é
		{"blob://runes000", 4, "abé", true},
		{"inline content longer than max", 4, "inline content longer than max", false},
	}
	for _, c := range cases {
		got, cut, err := Preview(ctx, s, c.content, c.max)
		if err != nil || got != c.want || cut != c.cut {
			t.Errorf("Preview(%q, %d) = %q, %v, %v; want %q, %v", c.content, c.max, got, cut, err, c.want, c.cut)
		}
	}
}
