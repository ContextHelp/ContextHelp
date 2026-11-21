package storageutil

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode/utf8"
)

var whitespaceRe = regexp.MustCompile(`\s+`)

// ContentHash is the dedup hash of content from source. Text is
// normalised (trimmed, lowercased, whitespace collapsed) so cosmetic
// differences dedup; content that is not valid UTF-8 is binary and hashes
// over its exact bytes, because lowercasing would turn every invalid byte
// into U+FFFD and merge distinct binaries, a corrupted copy included.
func ContentHash(rawContent, source string) string {
	normalized := rawContent
	if utf8.ValidString(rawContent) {
		normalized = normalizeContent(rawContent)
	}
	payload := normalized + "\x00" + source
	h := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(h[:])
}

func normalizeContent(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = whitespaceRe.ReplaceAllString(s, " ")
	return s
}

// DraftContentHash is the dedup hash of a finished draft. A draft that
// names a forge identity (metadata forge + forge_id, set by the forge
// mappers) hashes over that identity, so re-submitting the same repository
// reinforces its object however its text has changed since; any other draft
// hashes over its raw content.
func DraftContentHash(rawContent, source string, metadata map[string]any) string {
	forge, _ := metadata["forge"].(string)
	id, _ := metadata["forge_id"].(string)
	if forge != "" && id != "" {
		return ContentHash("forge:"+forge+":"+id, source)
	}
	return ContentHash(rawContent, source)
}
