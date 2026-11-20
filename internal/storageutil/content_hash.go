package storageutil

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var whitespaceRe = regexp.MustCompile(`\s+`)

func ContentHash(rawContent, source string) string {
	normalized := normalizeContent(rawContent)
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
