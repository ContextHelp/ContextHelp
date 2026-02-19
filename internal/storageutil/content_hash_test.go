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
