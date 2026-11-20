package storageutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentHash(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		h1 := ContentHash("Hello World", "https://example.com")
		h2 := ContentHash("Hello World", "https://example.com")
		assert.Equal(t, h1, h2)
	})

	t.Run("different_content", func(t *testing.T) {
		h1 := ContentHash("Hello World", "https://example.com")
		h2 := ContentHash("Goodbye World", "https://example.com")
		assert.NotEqual(t, h1, h2)
	})

	t.Run("different_source", func(t *testing.T) {
		h1 := ContentHash("Hello World", "https://example.com")
		h2 := ContentHash("Hello World", "https://other.com")
		assert.NotEqual(t, h1, h2)
	})

	t.Run("normalizes_whitespace", func(t *testing.T) {
		h1 := ContentHash("Hello   World\n\nTest", "")
		h2 := ContentHash("hello world test", "")
		assert.Equal(t, h1, h2)
	})

	t.Run("case_insensitive", func(t *testing.T) {
		h1 := ContentHash("HELLO WORLD", "")
		h2 := ContentHash("hello world", "")
		assert.Equal(t, h1, h2)
	})

	t.Run("empty_source", func(t *testing.T) {
		h1 := ContentHash("Content", "")
		h2 := ContentHash("Content", "")
		assert.Equal(t, h1, h2)
	})
}

func TestDraftContentHash(t *testing.T) {
	forge := func(id string) map[string]any { return map[string]any{"forge": "github", "forge_id": id} }

	t.Run("forge identity ignores raw content", func(t *testing.T) {
		h1 := DraftContentHash("stars: 1", "https://github.com/o/r", forge("o/r"))
		h2 := DraftContentHash("stars: 2", "https://github.com/o/r", forge("o/r"))
		assert.Equal(t, h1, h2)
	})
	t.Run("forge identity is case-insensitive", func(t *testing.T) {
		assert.Equal(t,
			DraftContentHash("a", "s", forge("Octo/Repo")),
			DraftContentHash("b", "s", forge("octo/repo")))
	})
	t.Run("different repositories differ", func(t *testing.T) {
		assert.NotEqual(t,
			DraftContentHash("a", "s", forge("o/r1")),
			DraftContentHash("a", "s", forge("o/r2")))
	})
	t.Run("non-forge objects hash exactly as before", func(t *testing.T) {
		for _, md := range []map[string]any{nil, {}, {"final_url": "https://x", "stars": 3}, {"forge_id": "o/r"}, {"forge": ""}} {
			assert.Equal(t, ContentHash("Some Body\n text", "https://x"), DraftContentHash("Some Body\n text", "https://x", md))
		}
	})
	t.Run("no forge falls back to raw content", func(t *testing.T) {
		assert.Equal(t, ContentHash("body", "src"), DraftContentHash("body", "src", nil))
		assert.Equal(t, ContentHash("body", "src"), DraftContentHash("body", "src", map[string]any{"forge": "github"}))
		assert.NotEqual(t, DraftContentHash("a", "src", nil), DraftContentHash("b", "src", nil))
	})
}
