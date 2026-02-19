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
